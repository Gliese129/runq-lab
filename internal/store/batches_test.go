package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func batchTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func insertBatchTestJob(t *testing.T, s *Store, id string, statuses ...string) {
	t.Helper()
	ctx := context.Background()
	if err := s.InsertJob(ctx, &JobRow{ID: id, ProjectName: "p", Name: "seeds", ConfigJSON: `{"command":"python t.py"}`, Status: "done", TotalTasks: len(statuses), Target: "c", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for i, st := range statuses {
		if err := s.InsertTask(ctx, &TaskRow{ID: id + "-t" + string(rune('0'+i)), JobID: id, ProjectName: "p", Command: "x", ParamsJSON: "{}", Status: st, EnqueuedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
}

// Upgrading an old database gives every existing job its seq-1 batch,
// command taken from the stored config; existing rows stay.
func TestMigrateBackfillsFirstBatch(t *testing.T) {
	s := batchTestStore(t)
	ctx := context.Background()
	insertBatchTestJob(t, s, "jbold00001", "success")
	if _, err := s.DB().Exec(`DELETE FROM job_batches`); err != nil { // as before batches existed
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil { // idempotent
		t.Fatal(err)
	}
	b, err := s.ListJobBatches(ctx, "jbold00001")
	if err != nil || len(b) != 1 || b[0].Seq != 1 || b[0].Command != "python t.py" || b[0].Name != "seeds" || b[0].TaskCount != 1 {
		t.Fatalf("batches = %+v, %v", b, err)
	}
	if task, _ := s.GetTask(ctx, "jbold00001-t0"); task == nil || task.BatchSeq != 1 {
		t.Fatalf("task batch = %+v", task)
	}
}

// An append reopens the job, counts only live tasks, and refuses when the
// failed task it reruns changed meanwhile (nothing is written then).
func TestAppendTasksReopensJob(t *testing.T) {
	s := batchTestStore(t)
	ctx := context.Background()
	insertBatchTestJob(t, s, "jbaaaa0001", "success", "failed")
	if _, err := s.DB().Exec(`UPDATE jobs SET finished_at = 1 WHERE id = 'jbaaaa0001'`); err != nil {
		t.Fatal(err)
	}
	newTask := TaskRow{ID: "jbaaaa0001-r", JobID: "jbaaaa0001", ProjectName: "p", Command: "x", ParamsJSON: "{}", Status: "pending", EnqueuedAt: time.Now()}
	seq, err := s.AppendTasks(ctx, JobBatch{JobID: "jbaaaa0001", SubmittedAt: time.Now(), Name: "rerun"}, []TaskRow{newTask}, map[string]string{"jbaaaa0001-t1": "jbaaaa0001-r"})
	if err != nil || seq != 2 {
		t.Fatalf("append: seq %d, %v", seq, err)
	}
	j, _ := s.GetJob(ctx, "jbaaaa0001")
	if j.TotalTasks != 2 || j.Status != "running" || j.FinishedAt != nil {
		t.Fatalf("job = total %d, %s, finished %v", j.TotalTasks, j.Status, j.FinishedAt)
	}
	tasks, _ := s.ListTasks(ctx, TaskFilter{JobID: "jbaaaa0001"})
	if st, _ := ProjectJobStatus(tasks); st != "running" {
		t.Fatalf("status with the superseded failure = %s", st)
	}
	live, _ := s.ListTasks(ctx, TaskFilter{JobID: "jbaaaa0001", LiveOnly: true})
	if len(live) != 2 {
		t.Fatalf("live tasks = %d", len(live))
	}

	// The same failed task cannot be superseded twice.
	again := newTask
	again.ID = "jbaaaa0001-r2"
	if _, err := s.AppendTasks(ctx, JobBatch{JobID: "jbaaaa0001", SubmittedAt: time.Now()}, []TaskRow{again}, map[string]string{"jbaaaa0001-t1": again.ID}); !errors.Is(err, ErrAppendRaced) {
		t.Fatalf("second supersede: %v", err)
	}
	if b, _ := s.ListJobBatches(ctx, "jbaaaa0001"); len(b) != 2 {
		t.Fatalf("a refused append left a batch: %d", len(b))
	}
	if got, _ := s.GetTask(ctx, again.ID); got != nil {
		t.Fatal("a refused append left a task")
	}
}

func TestAppendTasksRefusesArchivedAndPaused(t *testing.T) {
	s := batchTestStore(t)
	ctx := context.Background()
	insertBatchTestJob(t, s, "jbarch0001", "success")
	_ = s.ArchiveJob(ctx, "jbarch0001")
	task := TaskRow{ID: "x1", JobID: "jbarch0001", ProjectName: "p", Command: "x", ParamsJSON: "{}", Status: "pending", EnqueuedAt: time.Now()}
	if _, err := s.AppendTasks(ctx, JobBatch{JobID: "jbarch0001"}, []TaskRow{task}, nil); err == nil {
		t.Fatal("append to an archived job")
	}
	insertBatchTestJob(t, s, "jbpaus0001", "pending")
	_ = s.UpdateJobStatus(ctx, "jbpaus0001", "paused")
	task.JobID = "jbpaus0001"
	if _, err := s.AppendTasks(ctx, JobBatch{JobID: "jbpaus0001"}, []TaskRow{task}, nil); err == nil {
		t.Fatal("append to a paused job")
	}
}

func TestResolveRefsAmbiguous(t *testing.T) {
	s := batchTestStore(t)
	ctx := context.Background()
	insertBatchTestJob(t, s, "jbcc000001", "success")
	insertBatchTestJob(t, s, "jbcc000002", "success")
	var amb *AmbiguousRefError
	if _, err := s.ResolveJobRef(ctx, "jbcc"); !errors.As(err, &amb) || len(amb.IDs) != 2 {
		t.Fatalf("job prefix: %v", err)
	}
	if j, err := s.ResolveJobRef(ctx, "jbcc000002"); err != nil || j.ID != "jbcc000002" {
		t.Fatalf("exact job: %v", err)
	}
	if _, err := s.ResolveTaskRef(ctx, "jbcc00000"); !errors.As(err, &amb) {
		t.Fatalf("task prefix: %v", err)
	}
	if id, err := s.ResolveTaskRef(ctx, "jbcc000001-t"); err != nil || id != "jbcc000001-t0" {
		t.Fatalf("unique task prefix: %q %v", id, err)
	}
}

// A superseded failure does not keep a job partial once its rerun is done.
func TestProjectJobStatusIgnoresSuperseded(t *testing.T) {
	tasks := []TaskRow{{ID: "a", Status: "success"}, {ID: "b", Status: "failed", SupersededBy: "c"}, {ID: "c", Status: "success"}}
	if got, _ := ProjectJobStatus(tasks); got != "done" {
		t.Fatalf("status = %s, want done", got)
	}
}
