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

// A task whose run name the job has is a new version of that cell. Each
// cell shows one version (pinned, else latest success, else latest); the job
// counts cells, ends by the selected versions and stays live while any
// version runs.
func TestAppendTasksAddsVersions(t *testing.T) {
	s := batchTestStore(t)
	ctx := context.Background()
	if err := s.InsertJob(ctx, &JobRow{ID: "jbaaaa0001", ProjectName: "p", Name: "seeds", ConfigJSON: `{}`, Status: "partial", TotalTasks: 2, Target: "c", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	task := func(id, run, status string, seq int) TaskRow {
		at = at.Add(time.Second)
		return TaskRow{ID: id, RunName: run, BatchSeq: seq, JobID: "jbaaaa0001", ProjectName: "p", Command: "x", ParamsJSON: "{}", Status: status, EnqueuedAt: at}
	}
	for _, tr := range []TaskRow{task("a1", "a", "success", 1), task("b1", "b", "failed", 1)} {
		if err := s.InsertTask(ctx, &tr); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.DB().Exec(`UPDATE jobs SET finished_at = 1 WHERE id = 'jbaaaa0001'`); err != nil {
		t.Fatal(err)
	}
	live := func() []string {
		rows, _ := s.ListTasks(ctx, TaskFilter{JobID: "jbaaaa0001", LiveOnly: true})
		var ids []string
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		return ids
	}
	job := func() *JobRow { j, _ := s.GetJob(ctx, "jbaaaa0001"); return j }

	seq, err := s.AppendTasks(ctx, JobBatch{JobID: "jbaaaa0001", SubmittedAt: time.Now(), Name: "retry"}, []TaskRow{task("b2", "b", "pending", 0)})
	if err != nil || seq != 2 {
		t.Fatalf("append: seq %d, %v", seq, err)
	}
	if j := job(); j.TotalTasks != 2 || j.Status != "running" || j.FinishedAt != nil {
		t.Fatalf("job = total %d, %s, finished %v", j.TotalTasks, j.Status, j.FinishedAt)
	}
	if got := live(); len(got) != 2 || got[1] != "b2" {
		t.Fatalf("live = %v, want a1 and the new version b2", got)
	}

	if err := s.UpdateTaskStatus(ctx, "b2", "success", nil); err != nil {
		t.Fatal(err)
	}
	if st, _, err := s.RefreshJob(ctx, "jbaaaa0001", false); err != nil || st != "done" {
		t.Fatalf("after the new version succeeded: %s, %v", st, err)
	}
	if job().FinishedAt == nil {
		t.Fatal("a done job has finished_at")
	}

	// Pinning the failed version makes it the cell's again.
	if st, err := s.PinVersion(ctx, "jbaaaa0001", "b", "b1"); err != nil || st != "partial" {
		t.Fatalf("pin b1: %s, %v", st, err)
	}
	if got := live(); len(got) != 2 || got[1] != "b1" {
		t.Fatalf("live after pin = %v", got)
	}
	if _, err := s.PinVersion(ctx, "jbaaaa0001", "a", "b1"); err == nil {
		t.Fatal("pinned a version of another cell")
	}
	if st, err := s.PinVersion(ctx, "jbaaaa0001", "b", ""); err != nil || st != "done" {
		t.Fatalf("unpin: %s, %v", st, err)
	}

	// Running a succeeded cell again: the success stays selected, but the job
	// is live until the new version ends.
	if _, err := s.AppendTasks(ctx, JobBatch{JobID: "jbaaaa0001", SubmittedAt: time.Now()}, []TaskRow{task("a2", "a", "running", 0)}); err != nil {
		t.Fatal(err)
	}
	if j := job(); j.Status != "running" || j.TotalTasks != 2 {
		t.Fatalf("rerun of a success: %s, total %d", j.Status, j.TotalTasks)
	}
	if got := live(); got[0] != "a1" {
		t.Fatalf("live = %v, want the earlier success a1 while a2 runs", got)
	}
}

// Tasks without run names (pre-rc6) stay one cell with the task their
// superseded_by chain leads to, else each is a cell of its own.
func TestGroupCellsLegacy(t *testing.T) {
	at := time.Now()
	tasks := []TaskRow{
		{ID: "x", Status: "failed", SupersededBy: "y", EnqueuedAt: at},
		{ID: "y", RunName: "base", Status: "success", EnqueuedAt: at.Add(time.Second)},
		{ID: "z", Status: "success", EnqueuedAt: at.Add(2 * time.Second)},
	}
	cells := GroupCells(tasks, nil)
	if len(cells) != 2 || len(cells[0].Versions) != 2 || cells[0].Selected != "y" || cells[1].Selected != "z" {
		t.Fatalf("cells = %+v", cells)
	}
}

func TestAppendTasksRefusesArchivedAndPaused(t *testing.T) {
	s := batchTestStore(t)
	ctx := context.Background()
	insertBatchTestJob(t, s, "jbarch0001", "success")
	_ = s.ArchiveJob(ctx, "jbarch0001")
	task := TaskRow{ID: "x1", JobID: "jbarch0001", ProjectName: "p", Command: "x", ParamsJSON: "{}", Status: "pending", EnqueuedAt: time.Now()}
	if _, err := s.AppendTasks(ctx, JobBatch{JobID: "jbarch0001"}, []TaskRow{task}); err == nil {
		t.Fatal("append to an archived job")
	}
	insertBatchTestJob(t, s, "jbpaus0001", "pending")
	_ = s.UpdateJobStatus(ctx, "jbpaus0001", "paused")
	task.JobID = "jbpaus0001"
	if _, err := s.AppendTasks(ctx, JobBatch{JobID: "jbpaus0001"}, []TaskRow{task}); err == nil {
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
	tasks = append(tasks, TaskRow{ID: "d", Status: "running", SupersededBy: "a"})
	if got, _ := ProjectJobStatus(tasks); got != "running" {
		t.Fatalf("a version still running keeps the job live: %s", got)
	}
}

// <experiment>/<cell>[@vN] names a version: N counts from the cell's first,
// no @vN is the selected one.
func TestResolveVersionRef(t *testing.T) {
	s := batchTestStore(t)
	ctx := context.Background()
	if err := s.InsertJob(ctx, &JobRow{ID: "exaaaa0001", ProjectName: "p", Name: "x", ConfigJSON: `{}`, Status: "done", Target: "c", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, tr := range []TaskRow{
		{ID: "v1", RunName: "base", BatchSeq: 1, Status: "success"},
		{ID: "v2", RunName: "base", BatchSeq: 2, Status: "failed"},
	} {
		tr.JobID, tr.ProjectName, tr.Command, tr.ParamsJSON, tr.EnqueuedAt = "exaaaa0001", "p", "x", "{}", time.Now()
		if err := s.InsertTask(ctx, &tr); err != nil {
			t.Fatal(err)
		}
	}
	for ref, want := range map[string]string{"exaaaa0001/base": "v1", "exaaaa/base@v2": "v2", "exaaaa0001/base@v1": "v1"} {
		if got, err := s.ResolveTaskRef(ctx, ref); err != nil || got != want {
			t.Errorf("%s = %q, %v; want %s", ref, got, err, want)
		}
	}
	for _, ref := range []string{"exaaaa0001/base@v3", "exaaaa0001/nope"} {
		if _, err := s.ResolveTaskRef(ctx, ref); err == nil {
			t.Errorf("%s resolved", ref)
		}
	}
}
