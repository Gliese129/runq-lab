package backend

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/store"
)

type killRecordingLane struct {
	*UnavailableBackend
	err   error
	calls int
}

func (l *killRecordingLane) KillJob(context.Context, string) error {
	l.calls++
	return l.err
}

func TestKillJobAttemptsEveryGenerationAndJoinsErrors(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.InsertJob(ctx, &store.JobRow{
		ID: "j1", ProjectName: "p", Status: "running", TotalTasks: 3,
		CreatedAt: time.Now(), Target: "hpc",
	}); err != nil {
		t.Fatal(err)
	}

	activeCause := errors.New("active cancel failed")
	retiringCause := errors.New("retiring cancel failed")
	active := &killRecordingLane{UnavailableBackend: NewUnavailableBackend(nil), err: activeCause}
	retiringFailed := &killRecordingLane{UnavailableBackend: NewUnavailableBackend(nil), err: retiringCause}
	retiringOK := &killRecordingLane{UnavailableBackend: NewUnavailableBackend(nil)}
	m, err := NewMultiBackend(map[string]Backend{"hpc": active}, st, "hpc")
	if err != nil {
		t.Fatal(err)
	}
	m.SetRetiringLane("hpc", "old-a", retiringFailed)
	m.SetRetiringLane("hpc", "old-b", retiringOK)

	err = m.KillJob(ctx, "j1")
	if !errors.Is(err, activeCause) || !errors.Is(err, retiringCause) {
		t.Fatalf("KillJob error = %v, want both active and retiring causes", err)
	}
	for name, lane := range map[string]*killRecordingLane{
		"active": active, "retiring failed": retiringFailed, "retiring successful": retiringOK,
	} {
		if lane.calls != 1 {
			t.Errorf("%s calls = %d, want 1", name, lane.calls)
		}
	}
}

// A target-scoped kill reaches only that target's lanes, and an experiment
// with no versions on the target is refused instead of killed elsewhere.
func TestKillJobOnTargetReachesOnlyThatTarget(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.DB().Exec(`INSERT INTO projects (name, config_json) VALUES ('p', '{}')`); err != nil {
		t.Fatal(err)
	}
	for _, j := range []struct{ id, target string }{{"exm", "a"}, {"exb", "b"}} {
		if err := st.InsertJob(ctx, &store.JobRow{ID: j.id, ProjectName: "p", ConfigJSON: "{}", Status: "running", Target: j.target, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	// exm: first submission on a, an append on b. exb: only on b.
	for _, tk := range []struct{ id, job, target string }{{"tka1", "exm", "a"}, {"tkb2", "exm", "b"}, {"tkb1", "exb", "b"}} {
		if err := st.InsertTask(ctx, &store.TaskRow{ID: tk.id, JobID: tk.job, ProjectName: "p", Command: "true", ParamsJSON: "{}",
			Status: "running", Target: tk.target, EnqueuedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	a := &killRecordingLane{UnavailableBackend: NewUnavailableBackend(nil)}
	b := &killRecordingLane{UnavailableBackend: NewUnavailableBackend(nil)}
	m, err := NewMultiBackend(map[string]Backend{"a": a, "b": b}, st, "a")
	if err != nil {
		t.Fatal(err)
	}

	if err := m.KillJobOnTarget(ctx, "exb", "a"); !errors.Is(err, ErrNotOnTarget) {
		t.Fatalf("KillJobOnTarget(exb, a) = %v, want ErrNotOnTarget", err)
	}
	if a.calls != 0 || b.calls != 0 {
		t.Fatalf("refused kill reached lanes: a=%d b=%d", a.calls, b.calls)
	}
	if err := m.KillJobOnTarget(ctx, "exm", "a"); err != nil {
		t.Fatal(err)
	}
	if a.calls != 1 || b.calls != 0 {
		t.Fatalf("kill exm on a reached a=%d b=%d, want a only", a.calls, b.calls)
	}
}
