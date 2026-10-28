package remote

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/preflight"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/submitplan"
	"github.com/gliese129/runq-lab/internal/utils"
)

// Preview compiles the job and renders what WOULD be submitted — the
// representative run.sh (first task) and its submit command — with zero
// side effects: nothing written, nothing persisted, nothing queued.
// The dry-run contract: preview is truth, disk stays untouched (C5/U1).
func (b *Backend) Preview(ctx context.Context, jobCfg job.JobConfig, proj *project.Config) (string, preflight.Report, error) {
	text, plan, err := b.CompilePreview(ctx, jobCfg, proj)
	return text, plan.Preflight, err
}

func (b *Backend) CompilePreview(ctx context.Context, jobCfg job.JobConfig, proj *project.Config) (string, submitplan.Plan, error) {
	warns := jobCfg.NormalizeLegacy()
	var into *appendTarget
	if jobCfg.Into != "" {
		var err error
		if into, err = b.loadAppendTarget(ctx, jobCfg.Into, proj.ProjectName); err != nil {
			return "", submitplan.Plan{}, err
		}
		jobCfg.Name = into.first.Name
	} else {
		warns = append(warns, duplicateNameWarnings(ctx, b.Store, jobCfg)...)
	}
	deps := b.planDeps()
	deps.JobID = utils.GenerateJobID()
	// Use the same root and layout as Prepare without materializing it.
	root, err := b.WorkspaceRoot(ctx, proj, false)
	if err != nil {
		return "", submitplan.Plan{}, err
	}
	jobRoot := path.Join(root, deps.JobID)
	if into != nil {
		deps.JobID, jobRoot = into.job.ID, path.Join(root, into.job.ID)
		if into.root != "" {
			jobRoot = into.root
		}
	}
	deps.Paths = submitplan.Paths{WorkspaceRoot: jobRoot, LogRoot: jobRoot}
	plan, err := submitplan.Build(ctx, jobCfg, proj, deps)
	if err != nil {
		return "", submitplan.Plan{}, err
	}
	b.addShellSyntax(ctx, &plan)

	var s strings.Builder
	if into != nil {
		summary, err := classifyAppend(&plan, into, jobCfg.Rerun, proj.CmdTemplate)
		if err != nil {
			return "", submitplan.Plan{}, err
		}
		plan.Append = &summary
		s.WriteString(summary.Report())
		if summary.Submits() == 0 {
			fmt.Fprintf(&s, "%v\n", job.NothingToAppendError(summary))
			return s.String(), plan, nil
		}
	}
	warns = append(warns, b.diskWarnings(ctx, proj, root, len(plan.Tasks))...)
	fmt.Fprintf(&s, "name: %s\n", plan.Name)
	for _, w := range warns {
		fmt.Fprintf(&s, "warning: %s\n", w)
	}
	if len(plan.UnconsumedParams) > 0 {
		fmt.Fprintf(&s, "params via ctx.params: %s\n", strings.Join(plan.UnconsumedParams, ", "))
	}
	fmt.Fprintf(&s, "dry-run: %d task(s) would be submitted\n", len(plan.Tasks))
	// Where <workspace> will actually live — shown read-only (nothing is
	// created here); ids are regenerated at real submit.
	if into != nil {
		fmt.Fprintf(&s, "data: %s/ (the job's directory)\n\n", jobRoot)
	} else {
		fmt.Fprintf(&s, "data: %s/ (preview job id; regenerated at submit)\n\n", jobRoot)
	}
	s.WriteString(preflight.Format(plan.Preflight.Results))

	if len(plan.Tasks) == 0 {
		return "", submitplan.Plan{}, fmt.Errorf("sweep expands to zero tasks — check sweep parameters (nothing would be submitted)")
	}
	fmt.Fprintf(&s, "baseline: %s\n", plan.Baseline)
	for _, task := range plan.Tasks {
		fmt.Fprintf(&s, "run %q task %s: outputs=%s checkpoints=%s WANDB_DIR=%s\n", task.RunName, task.TaskID, task.OutputDir, task.CheckpointDir, task.WandbDir)
	}
	for _, task := range plan.Tasks {
		if len(task.NeedsCode) > 0 {
			fmt.Fprintf(&s, "needs code: %q (%s) — submitting does not wait for it\n", task.RunName, strings.Join(task.NeedsCode, ", "))
		}
	}
	t := plan.Tasks[0]
	runsh := path.Join(t.TaskDir, runScriptName)
	// Render through THE submit renderer — a private copy of its vars map is
	// how preview and submit drift apart (deep-test P1: preview missed the
	// {{project}}/{{log_path}} vars the runq preset requires).
	cmd, err := renderSubmitCmd(b.Cfg.SubmitTemplate, t, plan, runsh)
	if err != nil {
		return "", submitplan.Plan{}, fmt.Errorf("render submit_template: %w\n%s", err, submitTemplateParamHint)
	}

	fmt.Fprintf(&s, "\n── submit command (task 1 of %d) ──\n%s%s\n", len(plan.Tasks), b.envPrelude(plan.Env).Render(), cmd)
	fmt.Fprintf(&s, "\n── run.sh (task 1 of %d) ──\n%s", len(plan.Tasks), b.buildRunScript(t, plan))
	return s.String(), plan, nil
}

// duplicateNameWarnings flags a name already used in the project, pointing
// a retry at the existing job instead of a same-named sibling.
func duplicateNameWarnings(ctx context.Context, st *store.Store, cfg job.JobConfig) []string {
	if cfg.Name == "" || st == nil {
		return nil
	}
	rows, err := st.ListJobs(ctx, cfg.Project, "")
	if err != nil {
		return nil
	}
	for _, r := range rows {
		if r.Name == cfg.Name {
			return []string{fmt.Sprintf("name %q is already used by %s (%s); to retry or add cells, extend that job instead of a same-named one (or pick a distinct name)", cfg.Name, r.ID, r.Status)}
		}
	}
	return nil
}
