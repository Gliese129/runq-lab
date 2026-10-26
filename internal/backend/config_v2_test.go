package backend

import (
	"context"
	"encoding/json"
	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/submitplan"
	"testing"
	"time"
)

type v2PreviewLane struct {
	*UnavailableBackend
	received job.JobConfig
}

func (l *v2PreviewLane) PreviewSubmit(ctx context.Context, cfg job.JobConfig) (PreviewResult, error) {
	l.received = cfg
	plan, err := submitplan.Build(ctx, cfg, cfg.EffectiveConfig, submitplan.Deps{})
	return PreviewResult{Plan: &plan, Preflight: plan.Preflight}, err
}
func (l *v2PreviewLane) SubmitJob(_ context.Context, cfg job.JobConfig, _ SubmitOptions) (string, int, error) {
	l.received = cfg
	return "test-job", 1, nil
}

func TestV2TargetPrecedenceAndThreeExperimentPreviews(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, b, other := &v2PreviewLane{UnavailableBackend: NewUnavailableBackend(nil)}, &v2PreviewLane{UnavailableBackend: NewUnavailableBackend(nil)}, &v2PreviewLane{UnavailableBackend: NewUnavailableBackend(nil)}
	m, err := NewMultiBackend(map[string]Backend{"a": a, "b": b, "other": other}, st, "other")
	if err != nil {
		t.Fatal(err)
	}
	proj := project.Config{ProjectName: "p", Target: "a", Targets: []string{"a", "b"}, WorkingDir: t.TempDir()}
	if err := m.CreateProject(context.Background(), proj); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ command, jobTarget, cliTarget, want, source string }{
		{"echo train", "", "", "a", "project"}, {"echo smoke", "b", "", "b", "job"}, {"echo resume", "b", "a", "a", "cli"},
	} {
		cfg := job.JobConfig{Project: "p", Command: tt.command, Target: tt.jobTarget, FixedParams: map[string]any{"seed": 1}}
		out, err := m.PreviewSubmitForTarget(context.Background(), tt.cliTarget, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if out.Target != tt.want || out.TargetSource != tt.source || out.Plan.Tasks[0].Command != tt.command || out.SyncPending != (tt.want != "a") {
			t.Fatalf("preview: %+v", out)
		}
		if out.Plan.EffectiveConfig.Target != "a" {
			t.Fatal("execution target replaced sync source")
		}
	}
	for _, target := range []string{"other", "missing"} {
		cfg := job.JobConfig{Project: "p", Command: "echo train", Target: target}
		if _, err := m.PreviewSubmit(context.Background(), cfg); err == nil {
			t.Fatalf("preview accepted %s", target)
		}
		if _, _, err := m.SubmitJob(context.Background(), cfg, SubmitOptions{}); err == nil {
			t.Fatalf("submit accepted %s", target)
		}
	}
	if _, _, err := m.SubmitJob(context.Background(), job.JobConfig{Project: "p", Command: "echo train"}, SubmitOptions{}); err != nil {
		t.Fatal(err)
	}
	if a.received.Target != "a" || a.received.TargetSource != "project" || a.received.EffectiveConfig == nil {
		t.Fatalf("submit differs from preview: %+v", a.received)
	}
	if err := m.CreateProject(context.Background(), project.Config{ProjectName: "missing-target", WorkingDir: t.TempDir()}); err == nil {
		t.Fatal("registration silently selected global default")
	}
	data, _ := json.Marshal(a.received)
	if len(data) == 0 {
		t.Fatal("cannot persist plan inputs")
	}
}

func TestV2StateReadsSurviveRemovedTarget(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedGenerationJob(t, st, "job-offline", "old")
	m, err := NewMultiBackend(map[string]Backend{}, st, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MergeMetricSummaries(ctx, []store.MetricSummaryRow{{TaskID: "job-offline-ta", JobID: "job-offline", Key: "loss", Last: 0.4, Min: 0.4, Max: 0.4, Count: 1, LastTS: time.Now().Unix()}}); err != nil {
		t.Fatal(err)
	}
	detail, err := m.GetJob(ctx, "job-offline")
	if err != nil {
		t.Fatal(err)
	}
	view, err := m.GetTask(ctx, "job-offline-ta")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Tasks) != 1 || view.Status != "running" || view.Target != "hpc" || !view.Stale || view.Command != "true" || view.Metrics["loss"] != 0.4 {
		t.Fatalf("lost snapshot: %+v", view)
	}
	if _, err := m.ListJobs(ctx, ""); err != nil {
		t.Fatal(err)
	}
}

type v2CapsLane struct {
	*UnavailableBackend
	caps Capabilities
}

func (l *v2CapsLane) Capabilities() Capabilities { return l.caps }
func TestV2TaskViewsUseOwningGenerationCapabilities(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedGenerationJob(t, st, "job-old", "old")
	active := &v2CapsLane{UnavailableBackend: NewUnavailableBackend(nil), caps: Capabilities{StateModel: "push"}}
	old := &v2CapsLane{UnavailableBackend: NewUnavailableBackend(nil), caps: Capabilities{StateModel: "poll", KillAsync: true}}
	m, err := NewMultiBackend(map[string]Backend{"default": active, "hpc": active}, st, "default")
	if err != nil {
		t.Fatal(err)
	}
	m.SetRetiringLane("hpc", "old", old)
	view, err := m.GetTask(ctx, "job-old-ta")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := m.GetJob(ctx, "job-old")
	if err != nil {
		t.Fatal(err)
	}
	if view.Capabilities == nil || !view.Capabilities.KillAsync || view.Capabilities.StateModel != "poll" || detail.Capabilities == nil || !detail.Capabilities.KillAsync {
		t.Fatalf("views used default/active lane: %+v %+v", view.Capabilities, detail.Capabilities)
	}
}

func TestV2TaskListHidesArchivedHistoryButDetailsRemain(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedGenerationJob(t, st, "job-archived", "old")
	seedGenerationJob(t, st, "job-visible", "old")
	if _, err := st.DB().ExecContext(ctx, `UPDATE jobs SET status='done',archived_at=? WHERE id='job-archived'`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	m, err := NewMultiBackend(map[string]Backend{}, st, "")
	if err != nil {
		t.Fatal(err)
	}
	items, total, err := m.ListTasks(ctx, TaskListOptions{Limit: 25})
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != "job-visible-ta" {
		t.Fatalf("main task list: %+v %d %v", items, total, err)
	}
	items, total, err = m.ListTasks(ctx, TaskListOptions{JobID: "job-archived", Limit: 25})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("explicit archived detail hidden: %+v %d %v", items, total, err)
	}
}
