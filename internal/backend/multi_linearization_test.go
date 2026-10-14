package backend

import (
	"context"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/store"
)

func TestRoutingUpdateLinearizesForceRefreshToReplacement(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	old := newGenerationTestLane()
	old.forceReceipt = &RefreshReceipt{Refreshed: true}
	replacement := newGenerationTestLane()
	replacement.forceReceipt = &RefreshReceipt{Refreshed: true}
	m := newGenerationMulti(t, st, old)

	release := m.BeginRoutingUpdate()
	done := make(chan error, 1)
	go func() {
		_, err := m.ForceRefreshTarget(context.Background(), "hpc")
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("refresh crossed an unfinished routing update: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	m.SetTarget("hpc", replacement)
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if old.forceCalls != 0 || replacement.forceCalls != 1 {
		t.Fatalf("force calls old/replacement = %d/%d, want replacement-only snapshot", old.forceCalls, replacement.forceCalls)
	}
}
