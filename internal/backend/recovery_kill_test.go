package backend

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/scheduler"
	"github.com/gliese129/runq-lab/internal/store"
)

func TestSSHRestoreNeverResubmitsDurableSubmittingAttempt(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	be, err := NewSSHBackend(SSHBackendConfig{
		Target: config.TargetConfig{
			Name: "hpc", SubmitTemplate: "submit {{run_sh}}",
			SubmitIDRegex: `job ([0-9]+)`, MaxInflight: 1,
		},
		Store: st,
		FS:    rfs.NewLocalFS(),
	})
	if err != nil {
		t.Fatalf("new SSH backend: %v", err)
	}
	t.Cleanup(func() { _ = be.Close() })
	if err := st.InsertJob(ctx, &store.JobRow{
		ID: "job-submitting", ProjectName: "p", ConfigJSON: "{}",
		Status: "running", TotalTasks: 1, Target: "hpc", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	if err := st.InsertTask(ctx, &store.TaskRow{
		ID: "task-submitting", JobID: "job-submitting", ProjectName: "p",
		Command: "true", ParamsJSON: "{}", Status: "submitting",
		Target: "hpc", TargetGeneration: be.Generation(), EnqueuedAt: time.Now(),
	}); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	if err := be.restoreLane(ctx); err != nil {
		t.Fatalf("restore lane: %v", err)
	}

	row, err := st.GetTask(ctx, "task-submitting")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if row.Status != "unknown" || row.StatusSource != "submit" {
		t.Fatalf("recovered status/source = %q/%q, want unknown/submit", row.Status, row.StatusSource)
	}
	if !strings.Contains(row.FailureDetail, "submission was in flight") {
		t.Fatalf("recovery evidence missing: %q", row.FailureDetail)
	}
	queued := be.queue.Get(row.ID)
	if queued == nil || queued.Status != scheduler.StatusUnknown {
		t.Fatalf("recovered attempt became launchable: %+v", queued)
	}
	if be.pool.FreeCount() != 0 {
		t.Fatal("recovered ambiguous attempt did not retain its submission slot")
	}
}

func TestSSHRestoreFailsClosedWhenStoreIsUnreadable(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	be, err := NewSSHBackend(SSHBackendConfig{
		Target: config.TargetConfig{Name: "hpc", SubmitTemplate: "submit {{run_sh}}", SubmitIDRegex: `job ([0-9]+)`},
		Store:  st,
		FS:     rfs.NewLocalFS(),
	})
	if err != nil {
		t.Fatalf("new SSH backend: %v", err)
	}
	t.Cleanup(func() { _ = be.Close() })
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	if err := be.restoreLane(context.Background()); err == nil {
		t.Fatal("restore succeeded with an unreadable durable store")
	}
	if be.queue.Len() != 0 {
		t.Fatalf("failed restore published %d queue entries", be.queue.Len())
	}
}

func TestSSHRestoreReinstatesPausedDispatchGate(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	be, err := NewSSHBackend(SSHBackendConfig{
		Target: config.TargetConfig{Name: "hpc", SubmitTemplate: "submit {{run_sh}}", SubmitIDRegex: `job ([0-9]+)`},
		Store:  st,
		FS:     rfs.NewLocalFS(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = be.Close() })
	if err := st.InsertJob(ctx, &store.JobRow{ID: "job-paused", ProjectName: "p", ConfigJSON: "{}", Status: "paused", TotalTasks: 1, Target: "hpc", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertTask(ctx, &store.TaskRow{ID: "task-paused", JobID: "job-paused", ProjectName: "p", Command: "true", ParamsJSON: "{}", Status: "pending", Target: "hpc", TargetGeneration: be.Generation(), EnqueuedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := be.restoreLane(ctx); err != nil {
		t.Fatalf("restore lane: %v", err)
	}
	if !be.sched.IsJobPaused("job-paused") {
		t.Fatal("paused job dispatch gate was not restored")
	}
}

func TestSSHRestorePreservesPendingExternalKillForReplay(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	be, err := NewSSHBackend(SSHBackendConfig{
		Target: config.TargetConfig{Name: "hpc", SubmitTemplate: "submit {{run_sh}}", SubmitIDRegex: `job ([0-9]+)`, MaxInflight: 1},
		Store:  st,
		FS:     rfs.NewLocalFS(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = be.Close() })
	if err := st.InsertJob(ctx, &store.JobRow{
		ID: "job-external-kill", ProjectName: "p", ConfigJSON: "{}",
		Status: "running", TotalTasks: 1, Target: "hpc", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	row := &store.TaskRow{
		ID: "task-external-kill", JobID: "job-external-kill", ProjectName: "p",
		Command: "true", ParamsJSON: "{}", Status: "pending",
		ExternalID: "scheduler-42", KillRequested: true,
		Target: "hpc", TargetGeneration: be.Generation(), EnqueuedAt: time.Now(),
	}
	if err := st.InsertTask(ctx, row); err != nil {
		t.Fatal(err)
	}

	if err := be.restoreLane(ctx); err != nil {
		t.Fatalf("restore lane: %v", err)
	}
	restored, err := st.GetTask(ctx, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != "pending" || restored.ExternalID != row.ExternalID || !restored.KillRequested {
		t.Fatalf("durable task after recovery = %+v, want pending external work with kill intent", restored)
	}
	if queued := be.queue.Get(row.ID); queued == nil || queued.Status != scheduler.StatusRunning {
		t.Fatalf("queue did not restore external ownership: %+v", queued)
	}
	if be.pool.FreeCount() != 0 {
		t.Fatal("external work did not retain its submission slot")
	}

	// The restored in-memory kill flag must still win at the next verdict.
	be.sched.FinishTask(be.queue.Get(row.ID), scheduler.StatusFailed, map[string]any{"status_source": "probe"})
	settled, err := st.GetTask(ctx, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Status != "killed" {
		t.Fatalf("restored kill intent settled as %q, want killed", settled.Status)
	}
}

func TestSSHKillJobClearsPausedDispatchGate(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	be, err := NewSSHBackend(SSHBackendConfig{
		Target: config.TargetConfig{Name: "hpc", SubmitTemplate: "submit {{run_sh}}", SubmitIDRegex: `job ([0-9]+)`},
		Store:  st,
		FS:     rfs.NewLocalFS(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = be.Close() })
	if err := st.InsertJob(ctx, &store.JobRow{ID: "job-paused-kill", ProjectName: "p", ConfigJSON: "{}", Status: "paused", TotalTasks: 1, Target: "hpc", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	row := &store.TaskRow{ID: "task-paused-kill", JobID: "job-paused-kill", ProjectName: "p", Command: "true", ParamsJSON: "{}", Status: "pending", TaskDir: t.TempDir(), Target: "hpc", TargetGeneration: be.Generation(), EnqueuedAt: time.Now()}
	if err := st.InsertTask(ctx, row); err != nil {
		t.Fatal(err)
	}
	be.queue.Push(TaskRowToSchedulerTask(row))
	be.sched.PauseJob(row.JobID)

	if err := be.KillJob(ctx, row.JobID); err != nil {
		t.Fatalf("kill paused job: %v", err)
	}
	if be.sched.IsJobPaused(row.JobID) {
		t.Fatal("whole-job kill retained the paused dispatch gate")
	}
	job, err := st.GetJob(ctx, row.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.Status != "killed" {
		t.Fatalf("job after kill = %+v, want killed", job)
	}
}

func TestSSHKillJobSettlesUnknownAttemptWithoutExternalID(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	be, err := NewSSHBackend(SSHBackendConfig{
		Target: config.TargetConfig{
			Name: "hpc", SubmitTemplate: "submit {{run_sh}}",
			SubmitIDRegex: `job ([0-9]+)`, MaxInflight: 1,
		},
		Store: st,
		FS:    rfs.NewLocalFS(),
	})
	if err != nil {
		t.Fatalf("new SSH backend: %v", err)
	}
	t.Cleanup(func() { _ = be.Close() })
	if err := st.InsertJob(ctx, &store.JobRow{
		ID: "job-unknown", ProjectName: "p", ConfigJSON: "{}",
		Status: "running", TotalTasks: 1, Target: "hpc", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	row := &store.TaskRow{
		ID: "task-unknown", JobID: "job-unknown", ProjectName: "p",
		Command: "true", ParamsJSON: "{}", Status: "unknown",
		StatusSource: "submit", TaskDir: t.TempDir(), Target: "hpc",
		TargetGeneration: be.Generation(), EnqueuedAt: time.Now(),
	}
	if err := st.InsertTask(ctx, row); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	queued := TaskRowToSchedulerTask(row)
	queued.Status = scheduler.StatusUnknown
	be.queue.Restore(queued)
	be.pool.Reserve(row.ID)

	if err := be.KillJob(ctx, row.JobID); err != nil {
		t.Fatalf("kill job: %v", err)
	}
	got, err := st.GetTask(ctx, row.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != "killed" || got.StatusSource != "runq" {
		t.Fatalf("unknown task after whole-job kill = %q/%q, want killed/runq", got.Status, got.StatusSource)
	}
	if be.pool.FreeCount() != 1 {
		t.Fatal("whole-job kill did not release the unknown attempt's slot")
	}
}

func TestSSHKillTaskReportsPersistenceFailure(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	be, err := NewSSHBackend(SSHBackendConfig{
		Target: config.TargetConfig{Name: "hpc", SubmitTemplate: "submit {{run_sh}}", SubmitIDRegex: `job ([0-9]+)`},
		Store:  st,
		FS:     rfs.NewLocalFS(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = be.Close() })
	if err := st.InsertJob(ctx, &store.JobRow{ID: "job-kill-fail", ProjectName: "p", ConfigJSON: "{}", Status: "pending", TotalTasks: 1, Target: "hpc", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	row := &store.TaskRow{ID: "task-kill-fail", JobID: "job-kill-fail", ProjectName: "p", Command: "true", ParamsJSON: "{}", Status: "pending", TaskDir: t.TempDir(), Target: "hpc", TargetGeneration: be.Generation(), EnqueuedAt: time.Now()}
	if err := st.InsertTask(ctx, row); err != nil {
		t.Fatal(err)
	}
	be.queue.Push(TaskRowToSchedulerTask(row))
	if _, err := st.DB().Exec(`
		CREATE TRIGGER reject_killed_update
		BEFORE UPDATE OF status ON tasks
		WHEN NEW.status = 'killed'
		BEGIN SELECT RAISE(ABORT, 'injected killed persistence failure'); END`); err != nil {
		t.Fatal(err)
	}

	if err := be.KillTask(ctx, row.ID); err == nil {
		t.Fatal("kill acknowledged despite rejected durable transition")
	}
	got, err := st.GetTask(ctx, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "pending" {
		t.Fatalf("failed kill changed durable status to %q", got.Status)
	}
	if qt := be.queue.Get(row.ID); qt == nil || qt.Status != scheduler.StatusPending {
		t.Fatalf("failed kill changed queue state: %+v", qt)
	}
}

func TestSSHRestoreReplaysDurableKillIntentIntoLifecycle(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	be, err := NewSSHBackend(SSHBackendConfig{
		Target: config.TargetConfig{Name: "hpc", SubmitTemplate: "submit {{run_sh}}", SubmitIDRegex: `job ([0-9]+)`, MaxInflight: 1},
		Store:  st,
		FS:     rfs.NewLocalFS(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = be.Close() })
	if err := st.InsertJob(ctx, &store.JobRow{ID: "job-kill-recover", ProjectName: "p", ConfigJSON: "{}", Status: "running", TotalTasks: 1, Target: "hpc", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	row := &store.TaskRow{ID: "task-kill-recover", JobID: "job-kill-recover", ProjectName: "p", Command: "true", ParamsJSON: "{}", Status: "running", KillRequested: true, ExternalID: "external-1", TaskDir: t.TempDir(), Target: "hpc", TargetGeneration: be.Generation(), EnqueuedAt: time.Now()}
	if err := st.InsertTask(ctx, row); err != nil {
		t.Fatal(err)
	}
	if err := be.restoreLane(ctx); err != nil {
		t.Fatalf("restore lane: %v", err)
	}
	queued := be.queue.Get(row.ID)
	if queued == nil || queued.Status != scheduler.StatusRunning {
		t.Fatalf("restored queue entry = %+v", queued)
	}
	be.sched.FinishTask(queued, scheduler.StatusFailed, map[string]any{"status_source": "probe"})
	got, err := st.GetTask(ctx, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "killed" || got.KillRequested {
		t.Fatalf("restored kill intent settled as status=%q kill_requested=%v", got.Status, got.KillRequested)
	}
}
