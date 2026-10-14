package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"
)

// A user kill must terminate a task even when it lands while the remote
// submit is in flight and no external cancel handle has been persisted yet.

// fakeRemoteLauncher models the production remote-launcher contract.
type fakeRemoteLauncher struct {
	mu        sync.Mutex
	launched  []string
	killed    []string
	gate      chan struct{} // when non-nil, Launch blocks until closed
	launchErr error         // when non-nil, Launch fails with it
	killErr   error         // when non-nil, KillErr reports it (cancel failed)
}

func (f *fakeRemoteLauncher) Launch(_ context.Context, t *Task) error {
	if f.gate != nil {
		<-f.gate
	}
	if f.launchErr != nil {
		return f.launchErr
	}
	f.mu.Lock()
	f.launched = append(f.launched, t.ID)
	f.mu.Unlock()
	return nil
}

func (f *fakeRemoteLauncher) Kill(taskID string) error {
	f.mu.Lock()
	f.killed = append(f.killed, taskID)
	f.mu.Unlock()
	return f.killErr
}

func (f *fakeRemoteLauncher) killedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.killed...)
}

func (f *fakeRemoteLauncher) launchedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.launched...)
}

func newKillRaceHarness(t *testing.T, launcher Launcher) (*Scheduler, *Queue, *fakeRemoteLauncher) {
	t.Helper()
	q := NewQueue()
	st := testStore(t)
	fl, _ := launcher.(*fakeRemoteLauncher)
	s := New(DefaultConfig(), q, testPool(1), launcher, st, testLogger())
	return s, q, fl
}

func seedRunningTask(t *testing.T, s *Scheduler, q *Queue, id string) *Task {
	t.Helper()
	task := &Task{
		ID: id, JobID: "j-" + id, ProjectName: "test",
		Command: "true", GPUsNeeded: 1,
	}
	seedJob(t, s.store, task.JobID, "test", 1)
	seedTask(t, s.store, task)
	q.Push(task)
	// Simulate "handed to the remote lane": queue entry running, no
	// external id yet (exactly the in-flight window).
	task.Status = StatusRunning
	s.slots.Reserve(task.ID)
	return task
}

func taskStatus(t *testing.T, s *Scheduler, id string) string {
	t.Helper()
	row, err := s.store.GetTask(context.Background(), id)
	if err != nil || row == nil {
		t.Fatalf("get task %s: %v", id, err)
	}
	return row.Status
}

// A failure verdict arriving after RequestKill must settle killed. The flag
// persists until a lifecycle event can honor it.
func TestKillFlagSettlesFailureAsKilled(t *testing.T) {
	fl := &fakeRemoteLauncher{}
	s, q, _ := newKillRaceHarness(t, fl)
	task := seedRunningTask(t, s, q, "t-killflag")

	s.RequestKill(task.ID)
	s.FinishTask(task, StatusFailed, map[string]any{"status_source": "probe"})

	if got := taskStatus(t, s, task.ID); got != "killed" {
		t.Fatalf("status = %q, want killed (kill intent must win)", got)
	}
	row, err := s.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.StatusSource != "runq" {
		t.Fatalf("status_source = %q, want authoritative runq cancellation", row.StatusSource)
	}
	if qt := q.Get(task.ID); qt != nil && qt.Status == StatusPending {
		t.Fatal("task returned to pending despite kill intent")
	}
}

// Control: without the flag, the same verdict settles failed.
func TestFailureWithoutKillFlagSettlesFailed(t *testing.T) {
	fl := &fakeRemoteLauncher{}
	s, q, _ := newKillRaceHarness(t, fl)
	task := seedRunningTask(t, s, q, "t-failure")

	s.FinishTask(task, StatusFailed, map[string]any{"status_source": "probe"})

	if got := taskStatus(t, s, task.ID); got != "failed" {
		t.Fatalf("status = %q, want failed", got)
	}
	if qt := q.Get(task.ID); qt == nil || qt.Status != StatusFailed {
		t.Fatalf("failure reopened task: %+v", qt)
	}
}

// A kill racing an in-flight submit: the flag is set while Launch blocks;
// on submit completion the scheduler must cancel the freshly created
// remote job (late external id) and settle killed — no leaked cluster job.
func TestKillDuringInflightSubmitCancelsLateJob(t *testing.T) {
	fl := &fakeRemoteLauncher{gate: make(chan struct{})}
	s, q, _ := newKillRaceHarness(t, fl)
	task := seedRunningTask(t, s, q, "t-inflight")

	done := make(chan struct{})
	go func() {
		s.launchAsync(task)
		close(done)
	}()

	s.RequestKill(task.ID) // kill lands while the submit is in flight
	close(fl.gate)         // submit completes, external id now exists

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("launchAsync did not return")
	}

	killed := fl.killedIDs()
	if len(killed) != 1 || killed[0] != task.ID {
		t.Fatalf("launcher.Kill calls = %v, want exactly [%s] (late job must be cancelled)", killed, task.ID)
	}
	if got := taskStatus(t, s, task.ID); got != "killed" {
		t.Fatalf("status = %q, want killed", got)
	}
}

// Kill must win on a transient launch failure: the submit never reached
// the scheduler, so settling killed locally is honest.
func TestKillWinsOnTransientLaunch(t *testing.T) {
	fl := &fakeRemoteLauncher{launchErr: ErrLaunchTransient}
	s, q, _ := newKillRaceHarness(t, fl)
	task := seedRunningTask(t, s, q, "t-transient")

	s.RequestKill(task.ID)
	s.launchAsync(task)

	if got := taskStatus(t, s, task.ID); got != "killed" {
		t.Fatalf("status = %q, want killed (kill must beat transient failure)", got)
	}
	if qt := q.Get(task.ID); qt != nil && qt.Status == StatusPending {
		t.Fatal("task returned to pending despite kill intent")
	}
}

// Kill must win on a deterministic scheduler rejection: no remote job
// exists, and the user asked for killed — not a permanent failure.
func TestKillWinsOnRejectedLaunch(t *testing.T) {
	fl := &fakeRemoteLauncher{launchErr: context.DeadlineExceeded} // any non-sentinel error = rejected
	s, q, _ := newKillRaceHarness(t, fl)
	task := seedRunningTask(t, s, q, "t-rejected")

	s.RequestKill(task.ID)
	s.launchAsync(task)

	if got := taskStatus(t, s, task.ID); got != "killed" {
		t.Fatalf("status = %q, want killed (kill must beat rejected→failed)", got)
	}
}

// A late cancel that FAILS must not record killed (the kill never lies):
// the flag is retained and the next lifecycle event settles the task.
func TestLateCancelFailureRetainsKillIntent(t *testing.T) {
	fl := &fakeRemoteLauncher{killErr: context.DeadlineExceeded}
	s, q, _ := newKillRaceHarness(t, fl)
	task := seedRunningTask(t, s, q, "t-cancelfail")

	s.RequestKill(task.ID)
	s.launchAsync(task) // submit OK, late cancel attempted and fails

	if got := taskStatus(t, s, task.ID); got == "killed" {
		t.Fatal("recorded killed although the remote cancel failed — the kill lied")
	}
	if !s.killPending(task.ID) {
		t.Fatal("kill intent was consumed despite cancel failure")
	}

	// The retained intent is honored by the next verdict.
	s.FinishTask(task, StatusFailed, map[string]any{"status_source": "probe"})
	if got := taskStatus(t, s, task.ID); got != "killed" {
		t.Fatalf("status = %q, want killed (retained flag must settle the verdict)", got)
	}
}
