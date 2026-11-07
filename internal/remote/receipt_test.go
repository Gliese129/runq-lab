package remote

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/scheduler"
	"github.com/gliese129/runq-lab/internal/store"
)

func writeSubmitCmd(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, submitCmdFileName)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func readReceiptFile(dir, suffix string) (string, bool) {
	raw, err := os.ReadFile(receiptBase(dir) + suffix)
	return string(raw), err == nil
}

// The receipt script is real shell: run it with sh and read what it leaves.
func TestReceiptScriptReplaysOutcome(t *testing.T) {
	for _, c := range []struct {
		name, body string
		code       int
	}{
		{"accepted", `printf 'Your job 42 ("x") has been submitted\n'; echo "warning: $RUNQ_SUBMIT_HANDLE" >&2`, 0},
		{"rejected", `echo 'Unable to run job: no such queue'; exit 3`, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			cmdFile := writeSubmitCmd(t, dir, c.body)
			out, err := exec.Command("sh", "-c", receiptScript(dir, cmdFile, "tk-handle")).CombinedOutput()
			var exit *exec.ExitError
			code := 0
			if errors.As(err, &exit) {
				code = exit.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if code != c.code {
				t.Fatalf("exit %d, want %d: %s", code, c.code, out)
			}
			r, err := readSubmitReceipt(rfs.NewLocalFS(), dir)
			if err != nil || !r.started || !r.finished || r.code != c.code || r.out != string(out) {
				t.Fatalf("receipt %+v err=%v, replayed output %q", r, err, out)
			}
			if c.code == 0 && !strings.Contains(r.out, "warning: tk-handle") {
				t.Fatalf("handle or stderr missing from the receipt: %q", r.out)
			}
		})
	}
}

// Losing the session must not lose the submission: the submit command keeps
// running and its receipt still says what the scheduler answered.
func TestReceiptSurvivesLostSession(t *testing.T) {
	dir := t.TempDir()
	cmdFile := writeSubmitCmd(t, dir, `sleep 1; echo 'Your job 43 ("x") has been submitted'`)
	cmd := exec.Command("sh", "-c", receiptScript(dir, cmdFile, "tk"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, ok := readReceiptFile(dir, ".started"); !ok {
		t.Fatal("no .started before the session was lost")
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
	_ = cmd.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rc, ok := readReceiptFile(dir, ".rc"); ok {
			out, _ := readReceiptFile(dir, ".out")
			if strings.TrimSpace(rc) != "0" || !strings.Contains(out, "Your job 43") {
				t.Fatalf("rc=%q out=%q", rc, out)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the submit command did not finish after the session was lost")
}

type receiptRecorder struct {
	finished, resumed, requeued []string
}

func (r *receiptRecorder) FinishTask(t *scheduler.Task, status scheduler.TaskStatus, _ map[string]any) {
	r.finished = append(r.finished, t.ID+":"+string(status))
}
func (r *receiptRecorder) ResumeUnknown(id string)  { r.resumed = append(r.resumed, id) }
func (r *receiptRecorder) RequeueUnknown(id string) { r.requeued = append(r.requeued, id) }

func TestUnknownLaunchSettledFromReceipt(t *testing.T) {
	marker := "submit command interrupted mid-flight\n" + launchReceiptMarker + "<dir>/.submit.*"
	for _, c := range []struct {
		name    string
		detail  string
		age     time.Duration
		receipt map[string]string // suffix → content
		status  string
		extID   string
		want    func(*receiptRecorder) string
	}{
		{name: "accepted", detail: marker, age: time.Minute,
			receipt: map[string]string{".started": "", ".out": "Your job 77 (\"x\") has been submitted\n", ".rc": "0\n"},
			status:  "pending", extID: "77",
			want: func(r *receiptRecorder) string { return strings.Join(r.resumed, ",") }},
		{name: "rejected", detail: marker, age: time.Minute,
			receipt: map[string]string{".started": "", ".out": "Unable to run job\n", ".rc": "2\n"},
			status:  "unknown",
			want:    func(r *receiptRecorder) string { return strings.Join(r.finished, ",") }},
		{name: "still submitting", detail: marker, age: time.Hour,
			receipt: map[string]string{".started": ""}, status: "unknown",
			want: func(r *receiptRecorder) string { return "" }},
		{name: "too early to judge", detail: marker, age: time.Second, status: "unknown",
			want: func(r *receiptRecorder) string { return "" }},
		{name: "never started", detail: marker, age: time.Hour, status: "unknown",
			want: func(r *receiptRecorder) string { return strings.Join(r.requeued, ",") }},
		{name: "older launch without a receipt", detail: "submit command interrupted mid-flight", age: time.Hour, status: "unknown",
			want: func(r *receiptRecorder) string { return "" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			st, err := store.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			dir := t.TempDir()
			jobID, taskID := seedTask(t, st, dir, "unknown", SourceSubmit)
			ctx := context.Background()
			row, _ := st.GetTask(ctx, taskID)
			fields := store.ControllerTime(*row, "dispatch_started_at", time.Now().Add(-c.age))
			fields["failure_detail"] = c.detail
			fields["external_id"] = nil
			if err := st.UpdateTaskStatus(ctx, taskID, "unknown", fields); err != nil {
				t.Fatal(err)
			}
			for suffix, body := range c.receipt {
				if err := os.WriteFile(receiptBase(dir)+suffix, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			rec := &receiptRecorder{}
			b := &Backend{Cfg: &config.TargetConfig{SubmitIDRegex: `Your job ([0-9]+)`}, Store: st,
				FS: newTestFSFromRunner(nopRunner), Finisher: rec}
			_ = b.EnsureFresh(ctx, jobID, 0)

			got, _ := st.GetTask(ctx, taskID)
			if got.Status != c.status || got.ExternalID != c.extID {
				t.Fatalf("task = %s ext %q, want %s ext %q", got.Status, got.ExternalID, c.status, c.extID)
			}
			want := map[string]string{"accepted": taskID, "rejected": taskID + ":failed", "never started": taskID}[c.name]
			if g := c.want(rec); g != want {
				t.Fatalf("scheduler calls = %q, want %q (recorder %+v)", g, want, rec)
			}
			if c.name == "accepted" && got.FailureDetail != "" {
				t.Fatalf("adopted task kept its unknown-era detail: %q", got.FailureDetail)
			}
			if len(rec.requeued) > 0 && c.name != "never started" {
				t.Fatalf("requeued without proof the submission never started: %+v", rec)
			}
		})
	}
}

// interruptingFS starts the submit command and loses its verdict.
type interruptingFS struct {
	*testFS
	deadline time.Time
}

func (f *interruptingFS) Exec(ctx context.Context, cmd string, args ...string) ([]byte, []byte, int, error) {
	f.deadline, _ = ctx.Deadline()
	return nil, nil, -1, rfs.ErrExecInterrupted
}

func TestLaunchGivesSubmitItsOwnBudgetAndNamesTheReceipt(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	writeSubmitCmd(t, dir, "qsub run.sh")
	fsys := &interruptingFS{testFS: newTestFSFromRunner(nopRunner)}
	l := NewLauncher(&Backend{Cfg: &config.TargetConfig{}, Store: st, FS: fsys})
	started := time.Now()
	err = l.Launch(context.Background(), &scheduler.Task{ID: "tk1", JobID: "j1", TaskDir: dir})
	if !errors.Is(err, scheduler.ErrLaunchUntracked) || !strings.Contains(err.Error(), launchReceiptMarker+receiptBase(dir)) {
		t.Fatalf("an interrupted launch must be unknown and name its receipt: %v", err)
	}
	if budget := fsys.deadline.Sub(started); budget < submitBudget-5*time.Second || budget > submitBudget+5*time.Second {
		t.Fatalf("submit budget %v, want about %v", budget, submitBudget)
	}
}
