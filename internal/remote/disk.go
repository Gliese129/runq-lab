package remote

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"sync"
	"time"

	"github.com/gliese129/runq-lab/internal/diskspace"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
)

// usageMeasurer measures finished jobs' directories (du) in the
// background, one at a time: a refresh pass only ever starts a
// measurement, never waits for one. A job whose measurement is in flight
// or failed recently is skipped and picked up by a later refresh.
type usageMeasurer struct {
	mu    sync.Mutex
	busy  bool
	done  map[string]bool      // measured (or found in the store): never again
	tried map[string]time.Time // last failed attempt
	// wg lets tests wait for the background measurement.
	wg sync.WaitGroup
}

const (
	usageRetryAfter = time.Hour
	usageTimeout    = 10 * time.Minute // du over a large Lustre tree is slow
)

func newUsageMeasurer() *usageMeasurer {
	return &usageMeasurer{done: map[string]bool{}, tried: map[string]time.Time{}}
}

// jobDirs are the distinct directories holding a job's task directories
// (normally exactly one: <data_path>/<pj…>/<jb…>).
func jobDirs(tasks []store.TaskRow) (dirs []string, taskCount int) {
	seen := map[string]bool{}
	for _, tk := range tasks {
		if tk.TaskDir == "" {
			continue
		}
		taskCount++
		if d := path.Dir(tk.TaskDir); !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	return dirs, taskCount
}

// measureJobLater starts measuring a terminal job's directory unless it is
// measured already, a measurement is running, or it failed recently.
// No-op on backends built without a measurer (tests' literal Backends).
func (b *Backend) measureJobLater(ctx context.Context, jobID string, tasks []store.TaskRow) {
	m := b.usage
	if m == nil || b.Store == nil {
		return
	}
	m.mu.Lock()
	if m.busy || m.done[jobID] || time.Since(m.tried[jobID]) < usageRetryAfter {
		m.mu.Unlock()
		return
	}
	m.busy = true
	m.mu.Unlock()
	release := func(done bool) {
		m.mu.Lock()
		m.busy = false
		if done {
			m.done[jobID] = true
		} else {
			m.tried[jobID] = time.Now()
		}
		m.mu.Unlock()
	}
	if u, err := b.Store.GetJobUsage(ctx, jobID); err == nil && u != nil {
		release(true)
		return
	}
	dirs, n := jobDirs(tasks)
	if len(dirs) == 0 {
		release(true) // nothing on disk to measure
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		mctx, cancel := context.WithTimeout(context.Background(), usageTimeout)
		defer cancel()
		u, found, err := measureDirs(mctx, rfs.WithContext(b.FS, mctx), dirs)
		if err == nil && found {
			err = b.Store.UpsertJobUsage(mctx, store.JobUsage{JobID: jobID, Bytes: u.Bytes, Files: u.Files, TaskCount: n, MeasuredAt: time.Now()})
		}
		if err != nil {
			slog.Debug("measure job usage", "job", jobID, "error", err)
		}
		release(err == nil)
	}()
}

// measureDirs sums the usage of dirs (one Exec); found=false when none of
// them exists any more (nothing worth recording).
func measureDirs(ctx context.Context, fsys rfs.FS, dirs []string) (sum diskspace.Usage, found bool, err error) {
	got, err := diskspace.Measure(ctx, fsys, dirs...)
	if err != nil {
		return sum, false, err
	}
	for _, u := range got {
		sum.Bytes += u.Bytes
		sum.Files += u.Files
	}
	return sum, len(got) > 0, nil
}

// diskFullDetailTimeout bounds the failed-task log check; it runs inside
// a verdict delivery, so a slow target must not hold the lane for long.
const diskFullDetailTimeout = 20 * time.Second

// diskFullDetail is the failure reason for a task whose log tail says the
// disk was full, "" otherwise (or when the check itself fails).
func (b *Backend) diskFullDetail(ctx context.Context, tk store.TaskRow) string {
	if tk.LogPath == "" || b.FS == nil {
		return ""
	}
	dir := tk.TaskDir
	if dir == "" {
		dir = path.Dir(tk.LogPath)
	}
	dctx, cancel := context.WithTimeout(ctx, diskFullDetailTimeout)
	defer cancel()
	d, ok, err := diskspace.Diagnose(dctx, rfs.WithContext(b.FS, dctx), tk.LogPath, dir)
	if err != nil || !ok {
		return ""
	}
	return d.Detail()
}

// lowFreeFrac is the "nearly full" line for the preview warning.
const lowFreeFrac = 0.05

// diskWarnings checks the filesystems a new job writes to — the project's
// data root and its working_dir, deduped by mount — and warns where free
// space or inodes are under 5%, or under what the project's measured jobs
// suggest `tasks` new tasks need. One Exec; a failed probe warns nothing.
func (b *Backend) diskWarnings(ctx context.Context, proj *project.Config, dataRoot string, tasks int) []string {
	if b.FS == nil || tasks <= 0 {
		return nil
	}
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	fss, err := diskspace.Probe(pctx, rfs.WithContext(b.FS, pctx), dataRoot, proj.WorkingDir)
	if err != nil {
		return nil
	}
	var fp store.TaskFootprint
	haveFP := false
	if b.Store != nil {
		fp, haveFP, _ = b.Store.ProjectTaskFootprint(ctx, proj.ProjectName)
	}
	dataFS, _ := diskspace.For(fss, dataRoot)
	var warns []string
	for _, f := range fss {
		est := haveFP && f.Mount == dataFS.Mount
		needBytes, needFiles := fp.BytesPerTask*int64(tasks), fp.FilesPerTask*int64(tasks)
		if f.TotalBytes > 0 && (f.FreeBytesFrac() < lowFreeFrac || est && needBytes > f.FreeBytes) {
			msg := fmt.Sprintf("disk: %s is low on space — %s free (%.0f%% of %s)",
				f.Mount, diskspace.Bytes(f.FreeBytes), 100*f.FreeBytesFrac(), diskspace.Bytes(f.TotalBytes))
			if est {
				msg += fmt.Sprintf("; this job may write ~%s (%d tasks × %s, the most per task so far, from %s)",
					diskspace.Bytes(needBytes), tasks, diskspace.Bytes(fp.BytesPerTask), fp.BytesJobID)
			}
			warns = append(warns, msg+freeSpaceHint)
		}
		if f.InodesKnown && (f.FreeInodesFrac() < lowFreeFrac || est && needFiles > f.FreeInodes) {
			msg := fmt.Sprintf("disk: %s is low on inodes (files) — %s free (%.0f%% of %s)",
				f.Mount, diskspace.Count(f.FreeInodes), 100*f.FreeInodesFrac(), diskspace.Count(f.TotalInodes))
			if est {
				msg += fmt.Sprintf("; this job may create ~%s files (%d tasks × %s, the most per task so far, from %s)",
					diskspace.Count(needFiles), tasks, diskspace.Count(fp.FilesPerTask), fp.FilesJobID)
			}
			warns = append(warns, msg+freeSpaceHint)
		}
	}
	return warns
}

const freeSpaceHint = ". To free space: runq clean --checkpoints --older-than 7d --show"
