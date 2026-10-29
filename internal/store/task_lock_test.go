package store

import (
	"fmt"
	"sync"
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

// WithTaskLocks takes many ids (several per shard) without self-deadlock,
// excludes WithTaskLock on any of them while held, and releases them all.
func TestWithTaskLocksHoldsEveryTask(t *testing.T) {
	s := &Store{}
	ids := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		ids = append(ids, fmt.Sprintf("tk%04d", i))
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = s.WithTaskLocks(ids, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	got := make(chan struct{})
	go func() {
		_ = s.WithTaskLock("tk0123", func() error { close(got); return nil })
	}()
	select {
	case <-got:
		t.Fatal("WithTaskLock ran while WithTaskLocks held the same task")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("WithTaskLocks did not release its locks")
	}
}
