package remote

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/utils"
)

// ErrMarkerDetectionDisabled means no marker source is configured, so no
// remote observation was attempted. ScanDoneMarkers returns nil only after a
// configured marker source has been observed successfully.
var ErrMarkerDetectionDisabled = errors.New("done marker detection is disabled")

// ScanDoneMarkers is the cheap completion detector: ONE ReadDir of the
// target's done dir tells us which tasks finished since the last look,
// then only those tasks' status.json files are read (K reads for K new
// completions, not N reads for N in-flight tasks). This is the primary
// completion path; the scheduler probe (qstat) is the low-frequency
// alignment net for tasks whose wrapper never got to write a marker
// (node fail, hard kill).
//
// Ownership rule: markers whose task id is unknown to this store, or whose
// task belongs to another target, are LEFT IN PLACE — another runq client
// sharing this workspace may own them. We only ever delete what we settled.
func (b *Backend) ScanDoneMarkers(ctx context.Context) error {
	dir := b.doneDir()
	if dir == "" {
		return ErrMarkerDetectionDisabled
	}
	entries, err := rfs.WithContext(b.FS, ctx).ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		b.recordContact(nil) // dir absent = successful contact, nothing finished yet
		return nil
	}
	b.recordContact(err) // the 2min marker scan is /health's main heartbeat
	if err != nil {
		return fmt.Errorf("scan done markers: %w", err)
	}

	// ReadDir is observation only; a slow listing must not block launch/cancel.
	b.lifecycleMu.Lock()
	defer b.lifecycleMu.Unlock()
	var cleanup []string
	jobs := make(map[string]struct{})
	var firstErr error

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		id := e.Name()
		tk, gerr := b.Store.GetTask(ctx, id)
		if gerr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("look up completion marker owner %s: %w", id, gerr)
			}
			continue
		}
		if tk == nil || tk.Target != b.Cfg.Name {
			continue // not ours — leave the marker alone
		}
		// Generation ownership (review round 3): with a shared workspace,
		// active and retiring lanes see the SAME done dir — only the
		// owning lane may read, settle and delete a task's marker.
		// (Adoption restamps rows at restore, so exact match suffices.)
		if b.Scope != nil && tk.TargetGeneration != b.Scope.Generation {
			continue
		}
		if awaitingLaunch(*tk) {
			continue // dispatch has not published its execution identity yet
		}

		sf, statusErr := readStatusObserved(rfs.WithContext(b.FS, ctx), tk.TaskDir)
		if statusErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("read completion marker status for %s: %w", tk.ID, statusErr)
			}
			continue
		}
		// Short jobs can finish before the periodic running-task ingest. Their
		// marker must remain until all final output and offsets are durable.
		if isTerminal(sf.Status) || (isTerminal(tk.Status) && (tk.StatusSource == SourceWrapper || tk.StatusSource == SourceRunq)) {
			current, verifyErr, ingestErr := b.ingestLoadedTask(ctx, *tk, true)
			if err := errors.Join(verifyErr, ingestErr); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("final ingest for %s: %w", tk.ID, err)
				}
				continue
			}
			if !current {
				continue
			}
		}
		if isTerminal(tk.Status) {
			if err := b.applyTimeOnly(ctx, *tk, observationTimeFields(*tk, sf, ProbeResult{})); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			cleanup = append(cleanup, path.Join(dir, id))
			continue
		}
		if sf.Status == "" {
			if firstErr == nil {
				firstErr = fmt.Errorf("completion marker for %s has no valid status yet", tk.ID)
			}
			continue
		}
		d := Reconcile(tk.Status, tk.StatusSource, Observed{
			WrapperStatus: sf.Status,
			ExitCode:      sf.ExitCode,
			Scheduler:     SchedUnknown, // marker path never probes
		})
		if !isTerminal(d.Status) {
			// Marker present but status.json unreadable/non-terminal — a
			// half-written state. Leave the marker; the probe-align pass
			// will settle it with scheduler facts.
			continue
		}

		fields := observationTimeFields(*tk, sf, ProbeResult{})
		fields["status_source"] = d.Source
		if sf.FinishedAt > 0 {
			fields["finished_at"] = sf.FinishedAt
		} else {
			fields["finished_at"] = nowUnix()
		}
		if perr := b.persistDecision(ctx, *tk, d, fields); perr != nil {
			if firstErr == nil {
				firstErr = perr
			}
			continue
		}
		cleanup = append(cleanup, path.Join(dir, id))
		jobs[tk.JobID] = struct{}{}
	}

	// Job aggregates: FinishTask already refreshes when a Finisher is wired,
	// but the direct-write path (Finisher == nil) needs it, and a duplicate
	// refresh is a cheap idempotent DB write either way.
	for jobID := range jobs {
		if rerr := b.refreshJobStatus(ctx, jobID); rerr != nil && firstErr == nil {
			firstErr = rerr
		}
	}

	// Batch-delete settled markers in one shell call (rfs.FS has no Remove;
	// one exec beats one round trip per marker).
	if len(cleanup) > 0 {
		quoted := make([]string, len(cleanup))
		for i, p := range cleanup {
			quoted[i] = utils.ShellQuote(p)
		}
		if _, derr := b.shellRun(ctx, "rm -f "+strings.Join(quoted, " ")); derr != nil && firstErr == nil {
			firstErr = derr
		}
	}
	return firstErr
}

// HasInFlight reports whether this lane owns work that may already exist
// outside runq-lab. Generation scope is load-bearing here: an empty active
// lane must not stay awake merely because a retiring lane owns the target's
// remaining work, and vice versa.
func (b *Backend) HasInFlight(ctx context.Context) (bool, error) {
	tasks, err := b.Store.ListTasks(ctx, store.TaskFilter{Target: b.Cfg.Name, Scope: b.Scope})
	if err != nil {
		return false, err
	}
	for _, task := range tasks {
		if !isTerminal(task.Status) {
			return true, nil
		}
	}
	return false, nil
}
