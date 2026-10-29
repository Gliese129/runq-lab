package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/store"
)

func taskRow(t *testing.T, st *store.Store, id string) *store.TaskRow {
	t.Helper()
	row, err := st.GetTask(context.Background(), id)
	if err != nil || row == nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return row
}

// An inferred failure that reconcile revives must reach its real verdict:
// the queue follows the DB back to running (ReopenTask), so the verdict
// passes the staleness gate instead of being dropped.
func TestReopenedTaskReachesItsVerdict(t *testing.T) {
	s, st := newStatusHarness(t)
	ctx := context.Background()
	seedJob(t, st, "j1", "test", 1)
	seedTaskWithStatus(t, st, "j1", "t1", "pending")
	task := &Task{ID: "t1", JobID: "j1"}
	s.queue.Push(task)
	if err := s.queue.MarkRunning("t1"); err != nil {
		t.Fatal(err)
	}
	if err := s.slots.Acquire("t1"); err != nil {
		t.Fatal(err)
	}
	s.FinishTask(task, StatusFailed, map[string]any{"status_source": "inferred"})
	if row := taskRow(t, st, "t1"); row.Status != "failed" || row.StatusSource != "inferred" {
		t.Fatalf("inferred failure = %s/%s", row.Status, row.StatusSource)
	}

	// reconcile: the wrapper says it still runs
	if err := st.UpdateTaskStatus(ctx, "t1", "running", map[string]any{"status_source": "wrapper"}); err != nil {
		t.Fatalf("a provisional terminal must revive: %v", err)
	}
	s.ReopenTask("t1")
	if s.slots.FreeCount() != 0 {
		t.Fatal("a reopened task must hold its slot again")
	}

	s.FinishTask(task, StatusSuccess, map[string]any{"status_source": "wrapper"})
	if row := taskRow(t, st, "t1"); row.Status != "success" || row.StatusSource != "wrapper" {
		t.Fatalf("verdict after reopen = %s/%s, want success/wrapper", row.Status, row.StatusSource)
	}
	if s.slots.FreeCount() != 1 {
		t.Fatal("the verdict must release the slot")
	}
}

// Settled is absorbing: no status write moves a settled task back to a
// live status — a reported terminal at once, a guessed one after the grace.
func TestSettledIsAbsorbing(t *testing.T) {
	_, st := newStatusHarness(t)
	ctx := context.Background()
	seedJob(t, st, "j1", "test", 6)
	old := time.Now().Add(-store.SettleGrace - time.Hour).Unix()
	cases := []struct {
		id, status, source string
		finishedAt         int64
		revives            bool
	}{
		{"wrapper", "success", "wrapper", time.Now().Unix(), false},
		{"runq", "killed", "runq", time.Now().Unix(), false},
		{"scheduler", "failed", "scheduler", time.Now().Unix(), false},
		{"inferred-fresh", "failed", "inferred", time.Now().Unix(), true},
		{"submit-fresh", "failed", "submit", time.Now().Unix(), true},
		{"inferred-old", "failed", "inferred", old, false},
	}
	for _, c := range cases {
		seedTaskWithStatus(t, st, "j1", c.id, "pending")
		if _, err := st.DB().Exec(`UPDATE tasks SET status = ?, status_source = ?, finished_at = ? WHERE id = ?`,
			c.status, c.source, c.finishedAt, c.id); err != nil {
			t.Fatal(err)
		}
		row := taskRow(t, st, c.id)
		if row.Settled(time.Now()) == c.revives {
			t.Errorf("%s: Settled = %v", c.id, !c.revives)
		}
		for _, live := range []string{"running", "unknown", "pending"} {
			err := st.UpdateTaskStatus(ctx, c.id, live, map[string]any{"status_source": "wrapper"})
			if c.revives && live == "running" {
				if err != nil {
					t.Errorf("%s → running: %v", c.id, err)
				}
				break
			}
			if !c.revives && !errors.Is(err, store.ErrTaskStatusConflict) {
				t.Errorf("%s → %s must be refused, got %v", c.id, live, err)
			}
		}
		// terminal → terminal stays allowed (a scheduler verdict corrected
		// by the wrapper's)
		if !c.revives && c.source == "scheduler" {
			if err := st.UpdateTaskStatus(ctx, c.id, "success", map[string]any{"status_source": "wrapper"}); err != nil {
				t.Errorf("scheduler → wrapper terminal: %v", err)
			}
		}
	}
}
