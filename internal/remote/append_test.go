package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
)

func appendFixture(t *testing.T) (*Backend, *project.Config) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := t.TempDir()
	working := filepath.Join(root, "code")
	if err := os.MkdirAll(working, 0o755); err != nil {
		t.Fatal(err)
	}
	// h_rt is read only by the scheduler: it must not make cells differ.
	cfg := &config.TargetConfig{Name: "fixture", Scheduler: "sge", DataPath: filepath.Join(root, "bs"),
		SubmitTemplate: "echo 55 [[-l h_rt={{param.h_rt}}]] {{run_sh}}", SubmitIDRegex: "([0-9]+)", KillTemplate: "true"}
	b := &Backend{Cfg: cfg, Store: st, FS: rfs.NewLocalFS()}
	return b, &project.Config{ProjectName: "kl", WorkingDir: working, CmdTemplate: "python t.py --lr {{lr}} --seed {{seed}}"}
}

func gridCfg(seeds []any, fixed map[string]any) job.JobConfig {
	return job.JobConfig{Project: "kl", FixedParams: fixed, Sweep: []job.SweepBlock{{Method: "grid", Parameters: map[string]job.ParameterSpec{
		"lr": {Values: []any{1e-4, 2e-4, 3e-4}}, "seed": {Values: seeds}}}}}
}

func setStatus(t *testing.T, st *store.Store, id, status string) {
	t.Helper()
	if _, err := st.DB().Exec(`UPDATE tasks SET status = ? WHERE id = ?`, status, id); err != nil {
		t.Fatal(err)
	}
}

// Acceptance: a finished 6-cell job (one failed) grows to 9 cells in the
// same job; --rerun-failed reruns the failure as a new task; once every cell
// exists there is nothing to append and no batch is recorded.
func TestAppendIntoJob(t *testing.T) {
	ctx := context.Background()
	b, proj := appendFixture(t)
	first := gridCfg([]any{0, 1}, nil)
	first.Name = "A1 seeds"
	jobID, rows, err := b.Prepare(ctx, first, proj, SubmitOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 {
		t.Fatalf("first batch: %d tasks", len(rows))
	}
	for i, r := range rows {
		status := "success"
		if i == 0 {
			status = "failed"
		} else if i == 1 {
			status = "running"
		}
		setStatus(t, b.Store, r.ID, status)
	}
	failedID := rows[0].ID

	more := gridCfg([]any{0, 1, 2}, map[string]any{"h_rt": "4:00:00"})
	more.Name, more.Into = "more seeds", jobID

	text, plan, err := b.CompilePreview(ctx, more, proj)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Append == nil || plan.Append.New != 3 || plan.Append.SkippedExisting != 5 || plan.Append.SkippedFailed != 1 || plan.Append.RerunFailed != 0 {
		t.Fatalf("preview append = %+v\n%s", plan.Append, text)
	}
	if !strings.Contains(text, "new 3, skipped_existing 5, skipped_failed 1") || !strings.Contains(text, "(the job's directory)") {
		t.Errorf("preview text:\n%s", text)
	}

	var summary job.AppendSummary
	gotID, added, err := b.Prepare(ctx, more, proj, SubmitOpts{Append: &summary})
	if err != nil {
		t.Fatal(err)
	}
	if gotID != jobID || len(added) != 3 || summary.Batch != 2 || summary.New != 3 {
		t.Fatalf("append: job %s, %d tasks, summary %+v", gotID, len(added), summary)
	}
	j, _ := b.Store.GetJob(ctx, jobID)
	if j.TotalTasks != 9 || j.Status != "running" || j.FinishedAt != nil {
		t.Fatalf("job after append: total %d, status %s, finished %v", j.TotalTasks, j.Status, j.FinishedAt)
	}
	jobDir := filepath.Dir(rows[0].TaskDir)
	for _, r := range added {
		if filepath.Dir(r.TaskDir) != jobDir || r.BatchSeq != 2 {
			t.Errorf("appended task %s: dir %s, batch %d", r.ID, r.TaskDir, r.BatchSeq)
		}
		runsh, _ := os.ReadFile(filepath.Join(r.TaskDir, runScriptName))
		if !strings.Contains(string(runsh), "WANDB_RUN_GROUP='A1 seeds'") {
			t.Errorf("appended task %s does not keep the job's W&B group", r.ID)
		}
	}
	batches, _ := b.Store.ListJobBatches(ctx, jobID)
	if len(batches) != 2 || batches[1].Name != "more seeds" || batches[1].TaskCount != 3 {
		t.Fatalf("batches = %+v", batches)
	}

	// --rerun-failed: only the failed cell runs again, as a new task.
	more.RerunFailed = true
	summary = job.AppendSummary{}
	_, rerun, err := b.Prepare(ctx, more, proj, SubmitOpts{Append: &summary})
	if err != nil {
		t.Fatal(err)
	}
	if len(rerun) != 1 || summary.New != 0 || summary.RerunFailed != 1 || summary.Batch != 3 {
		t.Fatalf("rerun: %d tasks, %+v", len(rerun), summary)
	}
	old, _ := b.Store.GetTask(ctx, failedID)
	if old.SupersededBy != rerun[0].ID || rerun[0].RunName != old.RunName {
		t.Fatalf("old failed task: superseded_by %q (want %s), names %q / %q", old.SupersededBy, rerun[0].ID, old.RunName, rerun[0].RunName)
	}
	j, _ = b.Store.GetJob(ctx, jobID)
	if j.TotalTasks != 9 {
		t.Errorf("total after rerun = %d, want 9 (the superseded task is history)", j.TotalTasks)
	}

	// Everything exists now: nothing to append, no batch.
	_, _, err = b.Prepare(ctx, more, proj, SubmitOpts{})
	if !errors.Is(err, job.ErrNothingToAppend) || !strings.Contains(err.Error(), "all 9 cells already exist") {
		t.Fatalf("err = %v", err)
	}
	if batches, _ := b.Store.ListJobBatches(ctx, jobID); len(batches) != 3 {
		t.Errorf("a failed append recorded a batch: %d", len(batches))
	}
}

func TestAppendRefusesOtherProjectAndTarget(t *testing.T) {
	ctx := context.Background()
	b, proj := appendFixture(t)
	jobID, _, err := b.Prepare(ctx, gridCfg([]any{0}, nil), proj, SubmitOpts{})
	if err != nil {
		t.Fatal(err)
	}
	other := *proj
	other.ProjectName, other.ID = "other", ""
	cfg := gridCfg([]any{1}, nil)
	cfg.Project, cfg.Into = "other", jobID
	if _, _, err := b.Prepare(ctx, cfg, &other, SubmitOpts{}); err == nil || !strings.Contains(err.Error(), "belongs to project") {
		t.Errorf("other project: %v", err)
	}
	b2 := &Backend{Cfg: &config.TargetConfig{Name: "elsewhere", SubmitTemplate: "echo 1", SubmitIDRegex: "([0-9]+)", KillTemplate: "true"}, Store: b.Store, FS: b.FS}
	cfg = gridCfg([]any{1}, nil)
	cfg.Into = jobID
	if _, _, err := b2.Prepare(ctx, cfg, proj, SubmitOpts{}); err == nil || !strings.Contains(err.Error(), "runs on target fixture") {
		t.Errorf("other target: %v", err)
	}
}
