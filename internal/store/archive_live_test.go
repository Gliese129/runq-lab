package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Archiving checks the versions themselves in the same statement, and a
// version that revives later unarchives its job: a hidden job never holds
// live work.
func TestArchiveNeverHidesLiveWork(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.DB().Exec(`INSERT INTO projects(name, config_json) VALUES('p','{}')`); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertJob(ctx, &JobRow{ID: "j1", ProjectName: "p", ConfigJSON: "{}", Status: "failed", TotalTasks: 1, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	fin := time.Now()
	if err := st.InsertTask(ctx, &TaskRow{ID: "t1", JobID: "j1", ProjectName: "p", Command: "x", ParamsJSON: "{}", RunName: "a",
		Status: "failed", StatusSource: "inferred", FinishedAt: &fin, EnqueuedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	// the job row still says failed, but the version runs again
	if _, err := st.DB().Exec(`UPDATE tasks SET status = 'running' WHERE id = 't1'`); err != nil {
		t.Fatal(err)
	}
	if err := st.ArchiveJob(ctx, "j1"); err == nil || !strings.Contains(err.Error(), "running a version") {
		t.Fatalf("archive of a job with a live version: %v", err)
	}

	if _, err := st.DB().Exec(`UPDATE tasks SET status = 'failed' WHERE id = 't1'`); err != nil {
		t.Fatal(err)
	}
	if err := st.ArchiveJob(ctx, "j1"); err != nil {
		t.Fatal(err)
	}
	// reconcile revives the inferred failure
	if err := st.UpdateTaskStatus(ctx, "t1", "running", map[string]any{"status_source": "wrapper"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.RefreshJob(ctx, "j1", false); err != nil {
		t.Fatal(err)
	}
	j, _ := st.GetJob(ctx, "j1")
	if j.ArchivedAt != nil || j.Status != "running" {
		t.Fatalf("revived job: status %s archived %v", j.Status, j.ArchivedAt)
	}
}

// An append re-checks, in its transaction, the statuses it was planned
// against: a version that revived or finished meanwhile voids the plan.
func TestAppendRefusesAStalePlan(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.DB().Exec(`INSERT INTO projects(name, config_json) VALUES('p','{}')`); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertJob(ctx, &JobRow{ID: "j1", ProjectName: "p", ConfigJSON: "{}", Status: "failed", TotalTasks: 1, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertTask(ctx, &TaskRow{ID: "t1", JobID: "j1", ProjectName: "p", Command: "x", ParamsJSON: "{}", RunName: "a",
		Status: "failed", StatusSource: "inferred", EnqueuedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	retry := TaskRow{ID: "t2", JobID: "j1", ProjectName: "p", Command: "x", ParamsJSON: "{}", RunName: "a", Status: "pending", EnqueuedAt: time.Now()}
	planned := JobBatch{JobID: "j1", SubmittedAt: time.Now(), Expect: map[string]string{"t1": "failed"}}

	// the failure revives while the append is staged
	if err := st.UpdateTaskStatus(ctx, "t1", "running", map[string]any{"status_source": "wrapper"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTasks(ctx, planned, []TaskRow{retry}); !errors.Is(err, ErrAppendStale) {
		t.Fatalf("stale append: %v", err)
	}
	if got, _ := st.GetTask(ctx, "t2"); got != nil {
		t.Fatal("a refused append must write nothing")
	}
	planned.Expect["t1"] = "running"
	if _, err := st.AppendTasks(ctx, planned, []TaskRow{retry}); err != nil {
		t.Fatalf("current plan: %v", err)
	}
}

// A generation's versions all get its location, also one spelled another
// way before (an unresolved alias); other generations keep theirs.
func TestBackfillTaskLocations(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.DB().Exec(`INSERT INTO projects(name, config_json) VALUES('p','{}')`); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertJob(ctx, &JobRow{ID: "j1", ProjectName: "p", ConfigJSON: "{}", Status: "done", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for id, row := range map[string][2]string{"t1": {"g1", ""}, "t2": {"g1", "ssh:@hpc:22"}, "t3": {"g2", "ssh:x@y:22"}} {
		if err := st.InsertTask(ctx, &TaskRow{ID: id, JobID: "j1", ProjectName: "p", Command: "x", ParamsJSON: "{}", Status: "success",
			Target: "hpc", TargetGeneration: row[0], Location: row[1], EnqueuedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := st.BackfillTaskLocations(ctx, "hpc", "g1", "ssh:me@login1:22"); err != nil || n != 2 {
		t.Fatalf("backfilled %d, %v", n, err)
	}
	for id, want := range map[string]string{"t1": "ssh:me@login1:22", "t2": "ssh:me@login1:22", "t3": "ssh:x@y:22"} {
		if got, _ := st.GetTask(ctx, id); got.Location != want {
			t.Errorf("%s location = %q, want %q", id, got.Location, want)
		}
	}
}
