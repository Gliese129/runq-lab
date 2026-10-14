package store

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskLockSerializesSameTask(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	entered := make(chan struct{})
	release := make(chan struct{})
	secondStarted := make(chan struct{})
	done := make(chan struct{})
	var secondEntered atomic.Bool
	go func() {
		_ = st.WithTaskLock("task", func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	go func() {
		close(secondStarted)
		_ = st.WithTaskLock("task", func() error {
			secondEntered.Store(true)
			return nil
		})
		close(done)
	}()
	<-secondStarted
	time.Sleep(20 * time.Millisecond)
	if secondEntered.Load() {
		t.Fatal("same-task effects overlapped")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second same-task effect did not resume")
	}
}
