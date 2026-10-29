package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
)

// clean --checkpoints empties <task_dir>/checkpoints/ of inactive tasks
// and keeps everything else: logs, outputs, results, the dir itself and
// the DB rows. The dry run measures, lists free space, records usage and
// deletes nothing.
func TestCleanCheckpointsOnlyDeletesCheckpoints(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.DB().Exec(`INSERT INTO projects (name, config_json) VALUES ('p', '{}')`); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	jobs := map[string]string{"jbdone": "done", "jbrun": "running"}
	for id, status := range jobs {
		if err := st.InsertJob(ctx, &store.JobRow{ID: id, ProjectName: "p", ConfigJSON: "{}", Status: status, TotalTasks: 1, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	keep := []string{"task.log", "results.json", "outputs/pred.csv"}
	mk := func(job, task, status string) string {
		dir := filepath.Join(root, "pj1", job, task)
		for _, f := range append([]string{"checkpoints/step100.pt", "checkpoints/.latest", "checkpoints/sub/opt.pt"}, keep...) {
			p := filepath.Join(dir, f)
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, make([]byte, 40000), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.InsertTask(ctx, &store.TaskRow{ID: task, JobID: job, ProjectName: "p", Command: "true", ParamsJSON: "{}",
			Status: status, StatusSource: "wrapper", TaskDir: dir, LogPath: filepath.Join(dir, "task.log"), EnqueuedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	done1 := mk("jbdone", "tkdone1", "success")
	done2 := mk("jbdone", "tkdone2", "failed")
	running := mk("jbrun", "tkrun", "running")

	opts := CleanOptions{Checkpoints: true, JobID: "jbdone", DryRun: true}
	res, err := PerformClean(ctx, st, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.CheckpointJobs) != 1 || res.CheckpointJobs[0].JobID != "jbdone" || res.CheckpointJobs[0].Tasks != 2 {
		t.Fatalf("checkpoint jobs = %+v", res.CheckpointJobs)
	}
	if j := res.CheckpointJobs[0]; j.Bytes < 6*40000 || j.Files < 6 || j.Mount == "" {
		t.Fatalf("job usage = %+v", j)
	}
	if len(res.Filesystems) != 1 || res.Filesystems[0].TotalBytes <= 0 {
		t.Fatalf("filesystems = %+v", res.Filesystems)
	}
	if _, err := os.Stat(filepath.Join(done1, "checkpoints", "step100.pt")); err != nil {
		t.Fatal("dry run deleted a checkpoint")
	}
	if u, _ := st.GetJobUsage(ctx, "jbdone"); u == nil || u.TaskCount != 2 || u.Bytes < 12*40000 {
		t.Fatalf("dry run must record the job's usage: %+v", u)
	}

	// Selecting the running job too must not touch its checkpoints.
	opts = CleanOptions{Checkpoints: true, TaskIDs: []string{"tkdone1", "tkdone2", "tkrun"}}
	res, err = PerformClean(ctx, st, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tasks != 2 || res.Jobs != 1 || res.FreedBytes < 6*40000 {
		t.Fatalf("result = %+v", res)
	}
	for _, dir := range []string{done1, done2} {
		entries, err := os.ReadDir(filepath.Join(dir, "checkpoints"))
		if err != nil || len(entries) != 0 {
			t.Fatalf("%s/checkpoints = %v %v, want an empty dir", dir, entries, err)
		}
		for _, f := range keep {
			if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
				t.Fatalf("%s was deleted", f)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(running, "checkpoints", "step100.pt")); err != nil {
		t.Fatal("a running task's checkpoint was deleted")
	}
	for _, id := range []string{"tkdone1", "tkdone2", "tkrun"} {
		if tk, _ := st.GetTask(ctx, id); tk == nil {
			t.Fatalf("task row %s deleted", id)
		}
	}
	if u, _ := st.GetJobUsage(ctx, "jbdone"); u == nil || u.Bytes < 12*40000 {
		t.Fatalf("usage must survive the clean: %+v", u)
	}
}

// A checkpoint rm that fails comes back as a failure, and freed counts
// only what is gone (measured after the rm), not the size before it.
func TestCleanCheckpointsReportsFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.DB().Exec(`INSERT INTO projects (name, config_json) VALUES ('p', '{}')`); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertJob(ctx, &store.JobRow{ID: "jbdone", ProjectName: "p", ConfigJSON: "{}", Status: "done", TotalTasks: 1, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "pj1", "jbdone", "tk1")
	ck := filepath.Join(dir, "checkpoints")
	if err := os.MkdirAll(ck, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ck, "step100.pt"), make([]byte, 40000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ck, 0o555); err != nil { // its entries cannot be removed
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ck, 0o755) })
	if err := st.InsertTask(ctx, &store.TaskRow{ID: "tk1", JobID: "jbdone", ProjectName: "p", Command: "true", ParamsJSON: "{}",
		Status: "success", StatusSource: "wrapper", TaskDir: dir, EnqueuedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	res, err := PerformClean(ctx, st, nil, CleanOptions{Checkpoints: true, TaskIDs: []string{"tk1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failures) == 0 || res.FreedBytes != 0 {
		t.Fatalf("result = %+v, want a failure and nothing freed", res)
	}
}

// flipOnFirstExecFS is the local filesystem, but its first Exec (the du
// pass of a checkpoints clean) first runs hook: a status change landing
// between selection and the rm.
type flipOnFirstExecFS struct {
	*rfs.LocalFS
	hook  func()
	calls int
}

func (f *flipOnFirstExecFS) Exec(ctx context.Context, cmd string, args ...string) ([]byte, []byte, int, error) {
	f.calls++
	if f.calls == 1 {
		f.hook()
	}
	return f.LocalFS.Exec(ctx, cmd, args...)
}

// A guessed failure (inferred) is not settled: its checkpoints are kept
// and it comes back as skipped with why — even when reconciliation moves it
// back to running in the middle of the clean. The settled task is cleaned.
func TestCleanCheckpointsRechecksStatusBeforeDeleting(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.DB().Exec(`INSERT INTO projects (name, config_json) VALUES ('p', '{}')`); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertJob(ctx, &store.JobRow{ID: "jbdone", ProjectName: "p", ConfigJSON: "{}", Status: "failed", TotalTasks: 2, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	mk := func(task, status, source string) string {
		dir := filepath.Join(root, "pj1", "jbdone", task)
		ck := filepath.Join(dir, "checkpoints", "weights.pt")
		_ = os.MkdirAll(filepath.Dir(ck), 0o755)
		if err := os.WriteFile(ck, make([]byte, 4096), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := st.InsertTask(ctx, &store.TaskRow{ID: task, JobID: "jbdone", ProjectName: "p", Command: "true", ParamsJSON: "{}",
			Status: status, StatusSource: source, TaskDir: dir, EnqueuedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		return ck
	}
	flipped := mk("tkflip", "failed", "inferred")
	done := mk("tkdone", "success", "wrapper")

	fsys := &flipOnFirstExecFS{LocalFS: rfs.NewLocalFS(), hook: func() {
		if _, err := st.DB().Exec(`UPDATE tasks SET status = 'running', status_source = 'wrapper' WHERE id = 'tkflip'`); err != nil {
			t.Error(err)
		}
	}}
	res, err := PerformClean(ctx, st, func(store.TaskRow) (rfs.FS, error) { return fsys, nil },
		CleanOptions{Checkpoints: true, TaskIDs: []string{"tkflip", "tkdone"}})
	if err != nil {
		t.Fatal(err)
	}
	if fsys.calls == 0 {
		t.Fatal("the clean never measured: the status change did not land mid-clean")
	}
	if _, err := os.Stat(flipped); err != nil {
		t.Fatalf("checkpoint of a task that is running again was deleted: %v", err)
	}
	if _, err := os.Stat(done); !os.IsNotExist(err) {
		t.Fatalf("checkpoint of the still-terminal task kept: %v", err)
	}
	if res.Tasks != 1 || res.Jobs != 1 {
		t.Fatalf("result = %+v, want 1 version in 1 experiment cleaned", res)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].TaskID != "tkflip" || !strings.Contains(res.Skipped[0].Reason, "not confirmed") {
		t.Fatalf("skipped = %+v, want tkflip (outcome not confirmed)", res.Skipped)
	}
	if res.FreedBytes < 4096 || res.FreedBytes >= 2*4096 {
		t.Fatalf("freed = %d, want only the cleaned task's checkpoint", res.FreedBytes)
	}
}
