package remote

import "testing"

func TestSubmitHandleIsStablePerTask(t *testing.T) {
	first := submitHandle("task-1")
	if first != "task-1" {
		t.Fatalf("submit handle = %q, want task-1", first)
	}
	if got := submitHandle("task-1"); got != first {
		t.Fatalf("same task produced different submit handles: %q and %q", first, got)
	}
	if got := submitHandle("task-2"); got == first {
		t.Fatalf("distinct tasks produced the same submit handle %q", got)
	}
}
