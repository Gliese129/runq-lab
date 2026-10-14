package remote

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/scheduler"
	"github.com/gliese129/runq-lab/internal/store"
)

type resumeRecordingFinisher struct {
	store          *store.Store
	resumeCalls    int
	statusAtResume string
}

func (*resumeRecordingFinisher) FinishTask(*scheduler.Task, scheduler.TaskStatus, map[string]any) {}

func (f *resumeRecordingFinisher) ResumeUnknown(taskID string) {
	f.resumeCalls++
	row, _ := f.store.GetTask(context.Background(), taskID)
	if row != nil {
		f.statusAtResume = row.Status
	}
}

func seedRefreshTask(t *testing.T, status string) (*store.Store, store.TaskRow) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO projects (name, config_json) VALUES ('p', '{}')`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := st.InsertJob(ctx, &store.JobRow{
		ID: "j-refresh", ProjectName: "p", ConfigJSON: "{}", Status: "running",
		TotalTasks: 1, Target: "lab", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed job: %v", err)
	}
	row := store.TaskRow{
		ID: "t-refresh", JobID: "j-refresh", ProjectName: "p",
		Command: "true", ParamsJSON: "{}", Status: status, Target: "lab",
		ExternalID: "42", TaskDir: t.TempDir(), EnqueuedAt: time.Now(),
	}
	if err := st.InsertTask(ctx, &row); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	stored, err := st.GetTask(ctx, row.ID)
	if err != nil || stored == nil {
		t.Fatalf("get seeded task: %v", err)
	}
	return st, *stored
}

func TestUnknownResumePublishesOnlyAfterPersistence(t *testing.T) {
	st, tk := seedRefreshTask(t, "unknown")
	finisher := &resumeRecordingFinisher{store: st}
	b := &Backend{
		Cfg: &config.TargetConfig{Name: "lab"}, Store: st, FS: rfs.NewLocalFS(), Finisher: finisher,
	}
	if _, err := st.DB().Exec(`
		CREATE TRIGGER fail_unknown_resume BEFORE UPDATE OF status ON tasks
		WHEN NEW.status = 'running'
		BEGIN SELECT RAISE(ABORT, 'injected resume write failure'); END;
	`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	err := b.persistDecision(context.Background(), tk, Decision{Status: "running", Source: SourceScheduler}, map[string]any{
		"status_source": SourceScheduler,
	})
	if err == nil {
		t.Fatal("expected persistence failure")
	}
	if finisher.resumeCalls != 0 {
		t.Fatalf("queue resume published before persistence: calls=%d", finisher.resumeCalls)
	}

	if _, err := st.DB().Exec(`DROP TRIGGER fail_unknown_resume`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if err := b.persistDecision(context.Background(), tk, Decision{Status: "running", Source: SourceScheduler}, map[string]any{
		"status_source": SourceScheduler,
	}); err != nil {
		t.Fatalf("persist decision: %v", err)
	}
	if finisher.resumeCalls != 1 || finisher.statusAtResume != "running" {
		t.Fatalf("resume publication order wrong: calls=%d durable_status=%q", finisher.resumeCalls, finisher.statusAtResume)
	}
}

func TestFailedSchedulerProbeDoesNotAdvanceFreshness(t *testing.T) {
	st, tk := seedRefreshTask(t, "running")
	b := &Backend{
		Cfg:   &config.TargetConfig{Name: "lab", StatusTemplate: "check {{ext_id}}"},
		Store: st,
		FS: newTestFSFromRunner(func(context.Context, string) (string, error) {
			return "", errors.New("scheduler unavailable")
		}),
	}

	err := b.EnsureFresh(context.Background(), tk.JobID, time.Minute)
	if err == nil || !strings.Contains(err.Error(), "status_template exited") {
		t.Fatalf("expected visible probe failure, got %v", err)
	}
	job, err := st.GetJob(context.Background(), tk.JobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.RefreshedAt != nil {
		t.Fatalf("failed probe advanced durable freshness: %v", job.RefreshedAt)
	}
	if b.probeIsFresh(tk.JobID, time.Minute) {
		t.Fatal("failed probe advanced in-memory probe freshness")
	}
}

type statusReadErrorFS struct {
	*rfs.LocalFS
}

func (f *statusReadErrorFS) ReadFile(name string) ([]byte, error) {
	if strings.HasSuffix(name, "/"+statusFileName) {
		return nil, errors.New("injected remote read failure")
	}
	return f.LocalFS.ReadFile(name)
}

func (f *statusReadErrorFS) Exec(_ context.Context, _ string, _ ...string) ([]byte, []byte, int, error) {
	return []byte("RUNNING"), nil, 0, nil
}

func TestFailedStatusReadDoesNotAdvanceFreshness(t *testing.T) {
	st, tk := seedRefreshTask(t, "running")
	b := &Backend{
		Cfg:   &config.TargetConfig{Name: "lab", StatusTemplate: "check {{ext_id}}"},
		Store: st,
		FS:    &statusReadErrorFS{LocalFS: rfs.NewLocalFS()},
	}

	err := b.EnsureFresh(context.Background(), tk.JobID, time.Minute)
	if err == nil || !strings.Contains(err.Error(), "injected remote read failure") {
		t.Fatalf("expected visible status read failure, got %v", err)
	}
	job, err := st.GetJob(context.Background(), tk.JobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.RefreshedAt != nil {
		t.Fatalf("failed read advanced durable freshness: %v", job.RefreshedAt)
	}
	if b.probeIsFresh(tk.JobID, time.Minute) {
		t.Fatal("incomplete observation advanced in-memory probe freshness")
	}
}
