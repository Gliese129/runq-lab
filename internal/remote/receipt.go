package remote

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/utils"
)

// A submit receipt settles a submission whose session was lost. The submit
// command runs under nohup on the target and records what happened next to
// run.sh, so the facts survive the connection:
//
//	.submit.started  the command began
//	.submit.out      its stdout and stderr
//	.submit.rc       its exit code, written last
//
// Reconcile reads them for a task left `unknown` by an interrupted launch:
// a finished command with a job id is adopted, a rejection fails the task, and
// a command that never started goes back to the queue. A started command with
// no exit code yet stays unknown: it may still be running.

const (
	// submitBudget bounds one submit command. Ordinary ones take about a second.
	submitBudget = 2 * time.Minute

	// receiptGrace is how long after the launch attempt a missing receipt is
	// read as "never started". The command writes .submit.started before
	// anything else, within moments of the session starting.
	receiptGrace = 2 * time.Minute

	submitReceiptName = ".submit"

	// launchReceiptMarker tags the failure_detail of an interrupted launch that
	// left a receipt. Launches from earlier runq versions have none; their
	// missing receipt proves nothing, so they are never requeued from it.
	launchReceiptMarker = "submit receipt: "

	// receiptScriptHeader starts every receipt script.
	receiptScriptHeader = "# runq submit receipt\n"
)

func receiptBase(taskDir string) string { return path.Join(taskDir, submitReceiptName) }

// receiptScript runs the task's submit.cmd under nohup and waits for it, then
// replays its output and exit code. If this shell is lost with the session,
// the submit command still finishes and leaves the receipt. A receipt that
// cannot be read back exits 255, which reads as a lost verdict.
func receiptScript(taskDir, cmdFile, handle string) string {
	q := utils.ShellQuote
	return receiptScriptHeader +
		"r=" + q(receiptBase(taskDir)) + "\n" +
		"c=" + q(cmdFile) + "\n" +
		"export RUNQ_SUBMIT_HANDLE=" + q(handle) + "\n" +
		`rm -f "$r.started" "$r.out" "$r.rc" "$r.rc.tmp"` + "\n" +
		`nohup sh -c ': > "$1.started"; sh "$2" > "$1.out" 2>&1; printf "%s\n" "$?" > "$1.rc.tmp" && mv "$1.rc.tmp" "$1.rc"' runq-submit "$r" "$c" </dev/null >/dev/null 2>&1 &` + "\n" +
		`wait "$!"` + "\n" +
		`cat "$r.out" 2>/dev/null` + "\n" +
		`rc=$(cat "$r.rc" 2>/dev/null)` + "\n" +
		`exit "${rc:-255}"` + "\n"
}

type submitReceipt struct {
	started, finished bool
	code              int
	out               string
}

func readSubmitReceipt(fsys rfs.FS, taskDir string) (submitReceipt, error) {
	base := receiptBase(taskDir)
	var r submitReceipt
	raw, err := fsys.ReadFile(base + ".rc")
	switch {
	case err == nil:
		code, perr := strconv.Atoi(strings.TrimSpace(string(raw)))
		if perr != nil {
			return r, fmt.Errorf("read %s.rc: %q is not an exit code", base, strings.TrimSpace(string(raw)))
		}
		r.started, r.finished, r.code = true, true, code
		out, oerr := fsys.ReadFile(base + ".out")
		if oerr != nil && !errors.Is(oerr, fs.ErrNotExist) {
			return r, fmt.Errorf("read %s.out: %w", base, oerr)
		}
		r.out = string(out)
		return r, nil
	case !errors.Is(err, fs.ErrNotExist):
		return r, fmt.Errorf("read %s.rc: %w", base, err)
	}
	if _, err := fsys.Stat(base + ".started"); err == nil {
		r.started = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return r, fmt.Errorf("stat %s.started: %w", base, err)
	}
	return r, nil
}

// launchAttemptAt is when the scheduler last handed the task to the launcher.
func launchAttemptAt(tk store.TaskRow) time.Time {
	if e, ok := store.CurrentTaskTimes(tk)["dispatch_started_at"]; ok && e.At > 0 {
		return time.Unix(e.At, 0)
	}
	return tk.EnqueuedAt
}

// settleFromReceipt settles an interrupted launch from its submit receipt.
// settled reports that the task is handled for this pass. Called under
// lifecycleMu; the row is re-read so a verdict that landed first wins.
func (b *Backend) settleFromReceipt(ctx context.Context, loaded store.TaskRow) (settled bool, err error) {
	if loaded.Status != "unknown" || loaded.ExternalID != "" {
		return false, nil
	}
	row, err := b.Store.GetTask(ctx, loaded.ID)
	if err != nil || row == nil {
		return false, err
	}
	tk := *row
	if tk.Status != "unknown" || tk.ExternalID != "" || !strings.Contains(tk.FailureDetail, launchReceiptMarker) {
		return false, nil
	}
	r, err := readSubmitReceipt(rfs.WithContext(b.FS, ctx), tk.TaskDir)
	if err != nil {
		return false, fmt.Errorf("task %s: %w", tk.ID, err)
	}
	switch {
	case r.finished && r.code == 0:
		extID, perr := utils.ExtractSubmitID(r.out, b.Cfg.SubmitIDRegex)
		if perr != nil {
			// The scheduler took it but runq cannot name the job: still unknown.
			return false, nil
		}
		fields := store.ControllerTime(tk, "submitted_at", time.Now())
		fields["external_id"] = extID
		fields["failure_detail"] = nil
		store.FenceTaskStatusUpdate(fields, tk)
		if err := b.Store.UpdateTaskStatus(ctx, tk.ID, "pending", fields); err != nil {
			if errors.Is(err, store.ErrTaskStatusConflict) {
				return true, nil // a newer durable status superseded this receipt
			}
			return false, fmt.Errorf("adopt job %s for %s: %w", extID, tk.ID, err)
		}
		opLog("SUBMIT RECOVERED task=%s job=%s ext=%s (from its submit receipt)", tk.ID, tk.JobID, extID)
		if ru, ok := b.Finisher.(interface{ ResumeUnknown(taskID string) }); ok {
			ru.ResumeUnknown(tk.ID)
		}
		return true, nil
	case r.finished:
		opLog("SUBMIT REJECTED task=%s job=%s exit=%d (from its submit receipt)", tk.ID, tk.JobID, r.code)
		detail := fmt.Sprintf("submit %s rejected (exit %d), from its submit receipt:\n%s", tk.ID, r.code, strings.TrimSpace(r.out))
		return true, b.persistDecision(ctx, tk, Decision{Status: "failed", Source: SourceSubmit},
			map[string]any{"failure_detail": detail, "finished_at": nowUnix()})
	case r.started:
		return false, nil // may still be running: wait for its exit code
	case time.Since(launchAttemptAt(tk)) < receiptGrace:
		return true, nil
	}
	rq, ok := b.Finisher.(interface{ RequeueUnknown(taskID string) })
	if !ok {
		return false, nil
	}
	opLog("SUBMIT NEVER STARTED task=%s job=%s: returned to the queue", tk.ID, tk.JobID)
	rq.RequeueUnknown(tk.ID)
	return true, nil
}
