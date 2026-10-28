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

// Acceptance: a finished 6-cell job (one failed, one running) grows to 9
// cells in the same job; the failed cell gets a new version by default; a
// succeeded cell runs again only on request; once every cell succeeded or
// runs there is nothing to append and no submission is recorded.
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
	failed := rows[0]

	more := gridCfg([]any{0, 1, 2}, map[string]any{"h_rt": "4:00:00"})
	more.Name, more.Into = "more seeds", jobID

	text, plan, err := b.CompilePreview(ctx, more, proj)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Append == nil || plan.Append.New != 3 || plan.Append.NewVersion != 1 || plan.Append.Skipped != 5 {
		t.Fatalf("preview append = %+v\n%s", plan.Append, text)
	}
	if !strings.Contains(text, "new 3, new_version 1, skipped 5") || !strings.Contains(text, "(the job's directory)") {
		t.Errorf("preview text:\n%s", text)
	}

	var summary job.AppendSummary
	gotID, added, err := b.Prepare(ctx, more, proj, SubmitOpts{Append: &summary})
	if err != nil {
		t.Fatal(err)
	}
	if gotID != jobID || len(added) != 4 || summary.Batch != 2 || summary.New != 3 || summary.NewVersion != 1 {
		t.Fatalf("append: job %s, %d tasks, summary %+v", gotID, len(added), summary)
	}
	j, _ := b.Store.GetJob(ctx, jobID)
	if j.TotalTasks != 9 || j.Status != "running" || j.FinishedAt != nil {
		t.Fatalf("job after append: total %d cells, status %s, finished %v", j.TotalTasks, j.Status, j.FinishedAt)
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
	old, _ := b.Store.GetTask(ctx, failed.ID)
	var version store.TaskRow
	for _, r := range added {
		if r.RunName == failed.RunName {
			version = r
		}
	}
	if version.ID == "" || old.SupersededBy != version.ID {
		t.Fatalf("the failed cell's new version: %q, old superseded_by %q", version.ID, old.SupersededBy)
	}
	batches, _ := b.Store.ListJobBatches(ctx, jobID)
	if len(batches) != 2 || batches[1].Name != "more seeds" || batches[1].TaskCount != 4 {
		t.Fatalf("batches = %+v", batches)
	}

	// --rerun: a succeeded cell runs again; the others are skipped.
	ran := rows[2].RunName
	more.Rerun = []string{ran}
	summary = job.AppendSummary{}
	_, again, err := b.Prepare(ctx, more, proj, SubmitOpts{Append: &summary})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].RunName != ran || summary.Rerun != 1 || summary.Batch != 3 {
		t.Fatalf("rerun: %d tasks, %+v", len(again), summary)
	}
	if j, _ = b.Store.GetJob(ctx, jobID); j.TotalTasks != 9 {
		t.Errorf("total after rerun = %d cells, want 9", j.TotalTasks)
	}

	// Every cell succeeded or runs now: nothing to append, no submission.
	more.Rerun = nil
	_, _, err = b.Prepare(ctx, more, proj, SubmitOpts{})
	if !errors.Is(err, job.ErrNothingToAppend) || !strings.Contains(err.Error(), "all 9 cells") {
		t.Fatalf("err = %v", err)
	}
	if batches, _ := b.Store.ListJobBatches(ctx, jobID); len(batches) != 3 {
		t.Errorf("a failed append recorded a submission: %d", len(batches))
	}
}

// An append stays in its project, but a submission may run on another
// target: its versions get the experiment's directory there.
func TestAppendOtherProjectAndTarget(t *testing.T) {
	ctx := context.Background()
	b, proj := appendFixture(t)
	jobID, rows, err := b.Prepare(ctx, gridCfg([]any{0}, nil), proj, SubmitOpts{})
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
	for _, r := range rows {
		setStatus(t, b.Store, r.ID, "failed")
	}
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	b2 := &Backend{Cfg: &config.TargetConfig{Name: "elsewhere", Scheduler: "sge", DataPath: elsewhere, SubmitTemplate: "echo 1", SubmitIDRegex: "([0-9]+)", KillTemplate: "true"}, Store: b.Store, FS: b.FS}
	cfg = gridCfg([]any{0}, nil)
	cfg.Into = jobID
	_, added, err := b2.Prepare(ctx, cfg, proj, SubmitOpts{})
	if err != nil || len(added) != 3 {
		t.Fatalf("other target: %d versions, %v", len(added), err)
	}
	for _, r := range added {
		if r.Target != "elsewhere" || !strings.HasPrefix(r.TaskDir, elsewhere) || filepath.Base(filepath.Dir(r.TaskDir)) != jobID {
			t.Errorf("version on the other target: target %s, dir %s", r.Target, r.TaskDir)
		}
	}
	if jobs, _ := b.Store.ListJobs(ctx, "", "elsewhere"); len(jobs) != 1 || jobs[0].ID != jobID {
		t.Errorf("the experiment is listed on the other target too: %+v", jobs)
	}
}

// The command is a soft constraint: a new version may run another command
// (flagged in the summary), and so may a new cell.
func TestAppendAllowsAnotherCommand(t *testing.T) {
	ctx := context.Background()
	b, proj := appendFixture(t)
	jobID, rows, err := b.Prepare(ctx, gridCfg([]any{0}, nil), proj, SubmitOpts{})
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range rows {
		status := "success"
		if i == 0 {
			status = "failed"
		}
		setStatus(t, b.Store, r.ID, status)
	}
	cfg := gridCfg([]any{0, 1}, nil)
	cfg.Into, cfg.Command = jobID, "venv/bin/python t.py --lr {{lr}} --seed {{seed}}"
	gpus := 2
	cfg.Overrides = &job.Overrides{GPUsPerTask: &gpus}
	var summary job.AppendSummary
	if _, added, err := b.Prepare(ctx, cfg, proj, SubmitOpts{Append: &summary}); err != nil || len(added) != 4 {
		t.Fatalf("append with another command: %d tasks, %v", len(added), err)
	}
	for _, c := range summary.Cells {
		if c.CommandChanged != (c.Class == job.CellNewVersion) {
			t.Errorf("cell %s (%s): command_changed %v", c.Run, c.Class, c.CommandChanged)
		}
	}
}

// A row that names an existing cell must keep its params (scheduler-only
// ones aside): two drafts whose baselines differ name different cells alike.
func TestAppendRefusesSameNameOtherParams(t *testing.T) {
	ctx := context.Background()
	b, proj := appendFixture(t)
	cfg := job.JobConfig{Project: "kl", Tasks: []job.TaskRow{{Run: "base", Params: job.TaskParams{"lr": 1e-4, "seed": 0}}}}
	jobID, rows, err := b.Prepare(ctx, cfg, proj, SubmitOpts{})
	if err != nil {
		t.Fatal(err)
	}
	setStatus(t, b.Store, rows[0].ID, "failed")
	cfg.Into = jobID
	cfg.Tasks[0].Params = job.TaskParams{"lr": 1e-4, "seed": 1, "h_rt": "2:00:00"}
	if _, _, err := b.Prepare(ctx, cfg, proj, SubmitOpts{}); err == nil || !strings.Contains(err.Error(), "seed=1 (the cell has 0)") {
		t.Fatalf("same name, other seed: %v", err)
	}
	cfg.Tasks[0].Params = job.TaskParams{"lr": 1e-4, "seed": 0, "h_rt": "2:00:00"}
	if _, added, err := b.Prepare(ctx, cfg, proj, SubmitOpts{}); err != nil || len(added) != 1 {
		t.Fatalf("same cell, new walltime: %d, %v", len(added), err)
	}
}

// Two rows with the same params under different names are two cells.
func TestPlanAllowsSameParamsUnderTwoNames(t *testing.T) {
	ctx := context.Background()
	b, proj := appendFixture(t)
	cfg := job.JobConfig{Project: "kl", Tasks: []job.TaskRow{
		{Run: "A", Params: job.TaskParams{"lr": 1e-4, "seed": 0, "h_rt": "1:00:00"}},
		{Run: "B", Params: job.TaskParams{"lr": 1e-4, "seed": 0, "h_rt": "2:00:00"}},
	}}
	if _, rows, err := b.Prepare(ctx, cfg, proj, SubmitOpts{}); err != nil || len(rows) != 2 {
		t.Fatalf("two names: %d, %v", len(rows), err)
	}
}
