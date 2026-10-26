package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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
			Status: status, TaskDir: dir, LogPath: filepath.Join(dir, "task.log"), EnqueuedAt: time.Now()}); err != nil {
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
