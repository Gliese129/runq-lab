package backend

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/store"
)

// The fixture's cell R1 has v1 "old" (failed) and v2 "task" (success,
// selected).

// Cleaning the pinned, selected version drops the pick; what is left is
// selected and the experiment's status follows it.
func TestCleanSelectedVersionReselects(t *testing.T) {
	ctx := context.Background()
	f := newJobFilesFixture(t)
	if _, err := f.st.PinVersion(ctx, "job", "R1", "task"); err != nil {
		t.Fatal(err)
	}
	res, err := PerformClean(ctx, f.st, nil, CleanOptions{TaskID: "task"})
	if err != nil || res.Tasks != 1 {
		t.Fatalf("clean: %+v %v", res, err)
	}
	picks, err := f.st.CellPicks(ctx, "job")
	if err != nil || len(picks) != 0 {
		t.Fatalf("picks after clean = %v %v, want none", picks, err)
	}
	live, err := f.st.ListTasks(ctx, store.TaskFilter{JobID: "job", LiveOnly: true})
	if err != nil || len(live) != 1 || live[0].ID != "old" || live[0].SupersededBy != "" {
		t.Fatalf("selected after clean = %+v %v, want old", live, err)
	}
	if ref, err := f.st.ResolveTaskRef(ctx, "job/R1"); err != nil || ref != "old" {
		t.Fatalf("job/R1 = %q %v, want old", ref, err)
	}
	j, err := f.st.GetJob(ctx, "job")
	if err != nil || j.Status != "failed" || j.TotalTasks != 1 {
		t.Fatalf("job after clean = %+v %v, want failed with 1 cell", j, err)
	}
}

// Cleaning an older version keeps the others' numbers: @v1 is an error,
// not the next version.
func TestCleanKeepsVersionNumbers(t *testing.T) {
	ctx := context.Background()
	f := newJobFilesFixture(t)
	if _, err := PerformClean(ctx, f.st, nil, CleanOptions{TaskID: "old"}); err != nil {
		t.Fatal(err)
	}
	if ref, err := f.st.ResolveTaskRef(ctx, "job/R1@v2"); err != nil || ref != "task" {
		t.Fatalf("job/R1@v2 = %q %v, want task", ref, err)
	}
	if ref, err := f.st.ResolveTaskRef(ctx, "job/R1@v1"); err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("job/R1@v1 = %q %v, want a deleted-version error", ref, err)
	}
	if ref, err := f.st.ResolveTaskRef(ctx, "job/R1@v3"); err == nil || strings.Contains(err.Error(), "deleted") {
		t.Fatalf("job/R1@v3 = %q %v, want a no-such-version error", ref, err)
	}
}

// A number is never handed out twice: after the newest version is
// cleaned, the next one is v3, and @v2 stays an error.
func TestCleanNeverReusesVersionNumber(t *testing.T) {
	ctx := context.Background()
	f := newJobFilesFixture(t)
	if _, err := PerformClean(ctx, f.st, nil, CleanOptions{TaskID: "task"}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.InsertTask(ctx, &store.TaskRow{ID: "again", RunName: "R1", JobID: "job", ProjectName: "project",
		Command: "true", ParamsJSON: `{"lr":0.1}`, Status: "pending", BatchSeq: 2, EnqueuedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	again, err := f.st.GetTask(ctx, "again")
	if err != nil || again.Version != 3 {
		t.Fatalf("new version = %+v %v, want v3", again, err)
	}
	if ref, err := f.st.ResolveTaskRef(ctx, "job/R1@v3"); err != nil || ref != "again" {
		t.Fatalf("job/R1@v3 = %q %v, want again", ref, err)
	}
	if ref, err := f.st.ResolveTaskRef(ctx, "job/R1@v2"); err == nil {
		t.Fatalf("job/R1@v2 = %q, want a deleted-version error", ref)
	}
}

// Cleaning every version of an experiment removes it with its picks and
// version counters.
func TestCleanWholeJobDropsCellRecords(t *testing.T) {
	ctx := context.Background()
	f := newJobFilesFixture(t)
	if _, err := f.st.PinVersion(ctx, "job", "R1", "task"); err != nil {
		t.Fatal(err)
	}
	res, err := PerformClean(ctx, f.st, nil, CleanOptions{JobID: "job"})
	if err != nil || res.Jobs != 1 {
		t.Fatalf("clean: %+v %v", res, err)
	}
	for _, table := range []string{"cell_picks", "cell_versions"} {
		var n int
		if err := f.st.DB().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s rows after clean = %d %v, want 0", table, n, err)
		}
	}
}
