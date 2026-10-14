package store

import (
	"context"
	"errors"
	"testing"
)

func TestTaskStatusFencePreservesConcurrentTerminal(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedJobAndTask(t, st, "t-fence", "j-fence")
	if err := st.UpdateTaskStatus(context.Background(), "t-fence", "failed", map[string]any{
		"status_source":     "inferred",
		"external_id":       "attempt-0",
		"target_generation": "gen-old",
	}); err != nil {
		t.Fatalf("stamp running execution: %v", err)
	}
	old, err := st.GetTask(context.Background(), "t-fence")
	if err != nil || old == nil {
		t.Fatalf("load running execution: %v", err)
	}
	if err := st.UpdateTaskStatus(context.Background(), old.ID, "killed", map[string]any{"status_source": "runq"}); err != nil {
		t.Fatalf("concurrent cancellation: %v", err)
	}

	fields := map[string]any{"status_source": "wrapper"}
	FenceTaskStatusUpdate(fields, *old)
	if err := st.UpdateTaskStatus(context.Background(), old.ID, "success", fields); !errors.Is(err, ErrTaskStatusConflict) {
		t.Fatalf("stale fenced transition error = %v, want conflict", err)
	}
	// A terminal task must not return to the dispatch queue.
	if err := st.UpdateTaskStatus(context.Background(), old.ID, "pending", map[string]any{
		"external_id": nil,
	}); !errors.Is(err, ErrTaskStatusConflict) {
		t.Fatalf("terminal-to-pending transition error = %v, want conflict", err)
	}
	got, err := st.GetTask(context.Background(), old.ID)
	if err != nil || got == nil {
		t.Fatalf("get terminal task: %v", err)
	}
	if got.Status != "killed" || got.StatusSource != "runq" {
		t.Fatalf("stale transition overwrote cancellation: %#v", got)
	}
}
