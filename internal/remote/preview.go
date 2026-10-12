package remote

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/preflight"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/submitplan"
	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/gliese129/runq-lab/internal/workspace"
)

// Preview compiles the job and renders what WOULD be submitted — the
// representative run.sh (first task) and its submit command — with zero
// side effects: nothing written, nothing persisted, nothing queued.
// The dry-run contract: preview is truth, disk stays untouched (C5/U1).
func (b *Backend) Preview(ctx context.Context, jobCfg job.JobConfig, proj *project.Config, skipPreflight bool) (string, preflight.Report, error) {
	text, plan, err := b.CompilePreview(ctx, jobCfg, proj, skipPreflight)
	return text, plan.Preflight, err
}

func (b *Backend) CompilePreview(ctx context.Context, jobCfg job.JobConfig, proj *project.Config, skipPreflight bool) (string, submitplan.Plan, error) {
	resolved, err := resolveNote(ctx, b.Store, jobCfg)
	if err != nil {
		return "", submitplan.Plan{}, err
	}
	jobCfg.Note = resolved
	deps := b.planDeps(skipPreflight)
	deps.JobID = utils.GenerateJobID()
	// Use the same root and layout as Prepare without materializing it.
	root, err := b.WorkspaceRoot(proj, false)
	if err != nil {
		return "", submitplan.Plan{}, err
	}
	jobRoot := path.Join(root, workspace.JobDirName(resolved, deps.JobID))
	deps.Paths = submitplan.Paths{WorkspaceRoot: jobRoot, LogRoot: jobRoot}
	plan, err := submitplan.Build(ctx, jobCfg, proj, deps)
	if err != nil {
		// A preflight failure is a RESULT for a dry-run, not an error
		// (Codex r1 #6): return the structured report + readable text so
		// the WebUI replaces last round's findings instead of keeping
		// them next to a bare error string. Other Build errors (render,
		// validation) still propagate.
		if len(plan.Preflight.Results) > 0 && !plan.Preflight.OK() {
			var s strings.Builder
			s.WriteString("preflight failed — nothing would be submitted:\n\n")
			for _, c := range plan.Preflight.Results {
				mark := map[string]string{"passed": "✓", "failed": "✗", "warning": "!"}[c.Status]
				if mark == "" {
					mark = "-"
				}
				fmt.Fprintf(&s, "%s %-10s %s\n", mark, c.Name, c.Detail)
				for _, rc := range c.Commands {
					fmt.Fprintf(&s, "             $ %s\n", rc)
				}
			}
			s.WriteString("\nFix the failed checks, or submit with --no-preflight.")
			return s.String(), plan, nil
		}
		return "", submitplan.Plan{}, err
	}

	var s strings.Builder
	fmt.Fprintf(&s, "note: %s\n", plan.Note)
	if len(plan.UnconsumedParams) > 0 {
		fmt.Fprintf(&s, "params via ctx.params: %s\n", strings.Join(plan.UnconsumedParams, ", "))
	}
	fmt.Fprintf(&s, "dry-run: %d task(s) would be submitted\n", len(plan.Tasks))
	// Where <workspace> will actually live — shown read-only (nothing is
	// created here); ids are regenerated at real submit.
	fmt.Fprintf(&s, "workspace root: %s (preview IDs; regenerated at submit)\n\n", jobRoot)
	for _, c := range plan.Preflight.Results {
		mark := map[string]string{"passed": "✓", "failed": "✗", "warning": "!"}[c.Status]
		if mark == "" {
			mark = "-"
		}
		fmt.Fprintf(&s, "%s %-10s %s\n", mark, c.Name, c.Detail)
		// Remediation commands (e.g. HF pre-download): copy-paste ready.
		for _, rc := range c.Commands {
			fmt.Fprintf(&s, "             $ %s\n", rc)
		}
	}

	if len(plan.Tasks) == 0 {
		return "", submitplan.Plan{}, fmt.Errorf("sweep expands to zero tasks — check sweep parameters (nothing would be submitted)")
	}
	for _, task := range plan.Tasks {
		fmt.Fprintf(&s, "task %s: outputs=%s checkpoints=%s WANDB_DIR=%s\n", task.TaskID, task.OutputDir, task.CheckpointDir, task.WandbDir)
	}
	t := plan.Tasks[0]
	runsh := path.Join(t.TaskDir, runScriptName)
	// Render through THE submit renderer — a private copy of its vars map is
	// how preview and submit drift apart (deep-test P1: preview missed the
	// {{project}}/{{log_path}} vars the runq preset requires).
	cmd, err := renderSubmitCmd(b.Cfg.SubmitTemplate, t, plan, runsh)
	if err != nil {
		return "", submitplan.Plan{}, fmt.Errorf("render submit_template: %w", err)
	}

	fmt.Fprintf(&s, "\n── submit command (task 1 of %d) ──\n%s%s\n", len(plan.Tasks), b.envPrelude(plan.Env).Render(), cmd)
	fmt.Fprintf(&s, "\n── run.sh (task 1 of %d) ──\n%s", len(plan.Tasks), b.buildRunScript(t, plan))
	return s.String(), plan, nil
}
