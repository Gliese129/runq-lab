package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
)

// countingFS is the real local filesystem with Exec counted and optionally
// held until release is closed.
type countingFS struct {
	*rfs.LocalFS
	execs   atomic.Int32
	hold    chan struct{}
	started chan struct{}
	once    sync.Once
}

func (f *countingFS) Exec(ctx context.Context, cmd string, args ...string) ([]byte, []byte, int, error) {
	f.execs.Add(1)
	if f.hold != nil {
		f.once.Do(func() { close(f.started) })
		<-f.hold
	}
	return f.LocalFS.Exec(ctx, cmd, args...)
}

// seedLoggedTask is seedTask with a log path and a job directory layout
// (<root>/<job>/<task>), returning the task dir.
func seedLoggedTask(t *testing.T, st *store.Store, root, jobID, status string) string {
	t.Helper()
	ctx := context.Background()
	_, _ = st.DB().ExecContext(ctx, `INSERT OR IGNORE INTO projects(name, config_json) VALUES('p','{}')`)
	if err := st.InsertJob(ctx, &store.JobRow{ID: jobID, ProjectName: "p", ConfigJSON: "{}", Status: "running", TotalTasks: 1, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, jobID, "tk-"+jobID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	row := store.TaskRow{ID: "tk-" + jobID, JobID: jobID, ProjectName: "p", Command: "c", ParamsJSON: "{}",
		Status: status, StatusSource: SourceSubmit, ExternalID: "55", TaskDir: dir, LogPath: filepath.Join(dir, "task.log"), EnqueuedAt: time.Now()}
	if err := st.InsertTask(ctx, &row); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeWrapperStatus(t *testing.T, dir, status string, code int) {
	t.Helper()
	body := `{"status":"` + status + `","exit_code":` + string(rune('0'+code)) + `,"finished_at":1730000000}`
	if err := os.WriteFile(filepath.Join(dir, statusFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

var errFake = errors.New("fake failure")

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// A job that turns terminal gets its directory measured once, in the
// background, by backends built for real use (NewWithFS).
func TestTerminalJobUsageMeasuredInBackground(t *testing.T) {
	st := openStore(t)
	root := t.TempDir()
	dir := seedLoggedTask(t, st, root, "jb1", "running")
	if err := os.WriteFile(filepath.Join(dir, "ckpt.pt"), make([]byte, 50000), 0o644); err != nil {
		t.Fatal(err)
	}
	writeWrapperStatus(t, dir, "success", 0)
	b := NewWithFS(&config.TargetConfig{}, st, nil, rfs.NewLocalFS())
	if err := b.EnsureFresh(context.Background(), "jb1", 0); err != nil {
		t.Fatal(err)
	}
	b.usage.wg.Wait()
	u, err := st.GetJobUsage(context.Background(), "jb1")
	if err != nil || u == nil {
		t.Fatalf("job not measured: %+v %v", u, err)
	}
	if u.Bytes < 50000 || u.Files < 3 || u.TaskCount != 1 {
		t.Fatalf("usage = %+v", u)
	}
}

// One measurement at a time, and the caller never waits for du: a second
// finished job is skipped while the first is measured, then picked up.
func TestJobUsageMeasurementNeverBlocks(t *testing.T) {
	st := openStore(t)
	root := t.TempDir()
	seedLoggedTask(t, st, root, "jbA", "success")
	seedLoggedTask(t, st, root, "jbB", "success")
	fsys := &countingFS{LocalFS: rfs.NewLocalFS(), hold: make(chan struct{}), started: make(chan struct{})}
	b := &Backend{Cfg: &config.TargetConfig{}, Store: st, FS: fsys, usage: newUsageMeasurer()}
	ctx := context.Background()
	tasksOf := func(job string) []store.TaskRow {
		rows, _ := st.ListTasks(ctx, store.TaskFilter{JobID: job})
		return rows
	}

	start := time.Now()
	b.measureJobLater(ctx, "jbA", tasksOf("jbA"))
	<-fsys.started
	b.measureJobLater(ctx, "jbB", tasksOf("jbB"))
	if time.Since(start) > 2*time.Second {
		t.Fatal("measureJobLater waited for du")
	}
	close(fsys.hold)
	b.usage.wg.Wait()
	if n := fsys.execs.Load(); n != 1 {
		t.Fatalf("execs while busy = %d, want 1", n)
	}
	if u, _ := st.GetJobUsage(ctx, "jbB"); u != nil {
		t.Fatal("jbB measured while jbA was in flight")
	}
	b.measureJobLater(ctx, "jbB", tasksOf("jbB"))
	b.measureJobLater(ctx, "jbA", tasksOf("jbA")) // done: no second du
	b.usage.wg.Wait()
	if u, _ := st.GetJobUsage(ctx, "jbB"); u == nil {
		t.Fatal("jbB never measured")
	}
	if n := fsys.execs.Load(); n != 2 {
		t.Fatalf("execs = %d, want 2", n)
	}
}

// A run that dies of ENOSPC says so in failure_detail, at the cost of one
// Exec on the failed transition; an ordinary failure stays unexplained.
func TestFailedTaskLogDiskFullDetail(t *testing.T) {
	cases := []struct {
		log, want string
	}{
		{"epoch 3\nOSError: [Errno 28] No space left on device: 'ckpt.pt'\n", "disk full"},
		{"Traceback\nKeyError: 'lr'\n", ""},
	}
	for _, c := range cases {
		st := openStore(t)
		dir := seedLoggedTask(t, st, t.TempDir(), "jb1", "running")
		if err := os.WriteFile(filepath.Join(dir, "task.log"), []byte(c.log), 0o644); err != nil {
			t.Fatal(err)
		}
		writeWrapperStatus(t, dir, "failed", 1)
		fsys := &countingFS{LocalFS: rfs.NewLocalFS()}
		b := &Backend{Cfg: &config.TargetConfig{}, Store: st, FS: fsys}
		if err := b.EnsureFresh(context.Background(), "jb1", 0); err != nil {
			t.Fatal(err)
		}
		got, _ := st.GetTask(context.Background(), "tk-jb1")
		if got.Status != "failed" {
			t.Fatalf("status = %s", got.Status)
		}
		if c.want == "" && got.FailureDetail != "" || !strings.Contains(got.FailureDetail, c.want) {
			t.Fatalf("failure_detail = %q, want %q", got.FailureDetail, c.want)
		}
		if c.want != "" && !strings.Contains(got.FailureDetail, "runq clean --checkpoints") {
			t.Fatalf("no remedy in %q", got.FailureDetail)
		}
		if n := fsys.execs.Load(); n != 1 {
			t.Fatalf("execs = %d, want 1", n)
		}
		// Already failed: later refreshes don't check again.
		if err := b.EnsureFresh(context.Background(), "jb1", 0); err != nil {
			t.Fatal(err)
		}
		if n := fsys.execs.Load(); n != 1 {
			t.Fatalf("re-checked a failed task: execs = %d", n)
		}
	}
}

// dfRunner answers ProbeScript with df output for two filesystems: the
// data root on Lustre (10% free, inodes fine) and working_dir on NFS
// (half free, no inode counts).
func dfRunner(calls *int) func(ctx context.Context, command string) (string, error) {
	return func(ctx context.Context, command string) (string, error) {
		if !strings.Contains(command, "df -P -k") {
			return "", nil
		}
		*calls++
		lustre := "Filesystem 1024-blocks Used Available Capacity Mounted on\n10.0.2.11@o2ib:/gs 1000000000 900000000 100000000 90% /gs/bs\n"
		lustreI := "Filesystem Inodes IUsed IFree IUse% Mounted on\n10.0.2.11@o2ib:/gs 1000000 10000 990000 1% /gs/bs\n"
		nfs := "Filesystem 1024-blocks Used Available Capacity Mounted on\nnfs01:/export/home 104857600 52428800 52428800 50% /home\n"
		nfsI := "Filesystem Inodes IUsed IFree IUse% Mounted on\nnfs01:/export/home - - - - /home\n"
		return "@@k 0\n" + lustre + "@@i 0\n" + lustreI + "@@k 1\n" + nfs + "@@i 1\n" + nfsI, nil
	}
}

func TestPreviewWarnsWhenEstimateExceedsFreeSpace(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	seedLoggedTask(t, st, t.TempDir(), "jbold", "success")
	if err := st.UpsertJobUsage(ctx, store.JobUsage{JobID: "jbold", Bytes: 200 << 30, Files: 50, TaskCount: 1, MeasuredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	b := &Backend{Cfg: &config.TargetConfig{Name: "tsubame"}, Store: st, FS: newTestFSFromRunner(dfRunner(&calls))}
	proj := &project.Config{ProjectName: "p", WorkingDir: "/home/u/code"}

	warns := b.diskWarnings(ctx, proj, "/gs/bs/lab/runq/pj1", 1)
	if calls != 1 {
		t.Fatalf("probe execs = %d, want 1", calls)
	}
	if len(warns) != 1 {
		t.Fatalf("warns = %q", warns)
	}
	for _, want := range []string{"/gs/bs", "low on space", "95.4 GB free", "~200.0 GB", "from jbold", "runq clean --checkpoints"} {
		if !strings.Contains(warns[0], want) {
			t.Fatalf("warning %q lacks %q", warns[0], want)
		}
	}

	// Small enough for the free space, and over 5% free: nothing to say.
	if warns := b.diskWarnings(ctx, &project.Config{ProjectName: "other", WorkingDir: "/home/u/code"}, "/gs/bs/lab/runq/pj2", 1); len(warns) != 0 {
		t.Fatalf("unexpected warns %q", warns)
	}
	// Inodes: 1M-file tasks overflow the 990k free inodes.
	if err := st.UpsertJobUsage(ctx, store.JobUsage{JobID: "jbold", Files: 1_000_000, TaskCount: 1, MeasuredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	warns = b.diskWarnings(ctx, proj, "/gs/bs/lab/runq/pj1", 1)
	if len(warns) != 2 || !strings.Contains(warns[1], "low on inodes") || !strings.Contains(warns[1], "1.0M files") {
		t.Fatalf("inode warns = %q", warns)
	}
}

func TestPreviewDiskWarningInTextAndFailedProbeSilent(t *testing.T) {
	ctx := context.Background()
	for _, probeWorks := range []bool{true, false} {
		st := openStore(t)
		seedLoggedTask(t, st, t.TempDir(), "jbold", "success")
		_ = st.UpsertJobUsage(ctx, store.JobUsage{JobID: "jbold", Bytes: 200 << 30, Files: 1, TaskCount: 1, MeasuredAt: time.Now()})
		calls := 0
		run := dfRunner(&calls)
		if !probeWorks {
			run = func(ctx context.Context, command string) (string, error) {
				return "sh: df: not found", errFake
			}
		}
		cfg := &config.TargetConfig{Name: "tsubame", Scheduler: "sge", DataPath: "/gs/bs/lab/runq", SubmitTemplate: "qsub {{run_sh}}", SubmitIDRegex: "([0-9]+)", KillTemplate: "qdel {{ext_id}}"}
		b := &Backend{Cfg: cfg, Store: st, FS: newTestFSFromRunner(run)}
		proj := &project.Config{ProjectName: "p", WorkingDir: t.TempDir(), CmdTemplate: "python train.py"}
		text, _, err := b.CompilePreview(ctx, job.JobConfig{Project: "p"}, proj, true)
		if err != nil {
			t.Fatalf("probeWorks=%v: preview failed: %v", probeWorks, err)
		}
		has := strings.Contains(text, "warning: disk: /gs/bs is low on space")
		if has != probeWorks || (!probeWorks && strings.Contains(text, "disk")) {
			t.Fatalf("probeWorks=%v, text:\n%s", probeWorks, text)
		}
	}
}
