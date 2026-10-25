package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/consumer"
	"github.com/gliese129/runq-lab/internal/dashboard"
	job2 "github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/gosuri/uilive"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ── runq submit (shortcut for job submit) ──

var submitCmd = &cobra.Command{
	Use:   "submit [job.yaml | .]",
	Short: "Submit a job.yaml (one experiment) — preview first with --dry",
	Long: `Submit a job.yaml. A job is one experiment of a registered project:

  project: my-code-base
  note: kl-ablation
  command: python train.py            # optional; overrides the project's command
  target: abci                        # optional; must be in the project's targets
  fixed_params: { model: qwen3-8b }   # same value in every task
  sweep:                              # grid = cross product, list = zip rows
    - method: grid
      parameters: { kl: [0, 0.1], seed: [1, 2] }

Every param is written to the task's params.json and read in code with
ctx.params; only params the command references ({{x}} / {{args}}) appear on
the command line. Target precedence: -t > job.target > project.target.
"." means ./job.yaml.`,
	Example: `  runq submit job.yaml --dry                         # tasks, target, rendered command
  runq submit job.yaml --json                        # {"job_id": ..., "total_tasks": ...}
  runq submit job.yaml --project-file project.yaml   # register/update the project, then submit
  runq submit job.yaml -t abci --dry                 # same job on another declared target`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		file := args[0]
		if file == "" {
			return fmt.Errorf("no job file")
		}
		dryRun, _ := cmd.Flags().GetBool("dry")
		dryRunAlias, _ := cmd.Flags().GetBool("dry-run")
		watch, _ := cmd.Flags().GetBool("watch")
		projectOverride, _ := cmd.Flags().GetString("project")
		noteOverride, _ := cmd.Flags().GetString("note")
		noPreflight, _ := cmd.Flags().GetBool("no-preflight")
		jsonOut, _ := cmd.Flags().GetBool("json")

		if file == "." {
			file = "job.yaml"
		}
		if wd, err := os.Getwd(); err == nil && !filepath.IsAbs(file) {
			file = filepath.Join(wd, file)
		}
		var jobCfg job2.JobConfig
		fs, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if err := yaml.Unmarshal(fs, &jobCfg); err != nil {
			return err
		}
		if projectOverride != "" {
			jobCfg.Project = projectOverride
		}
		if noteOverride != "" {
			jobCfg.Note = noteOverride
		}

		return withBackend(cmd, func(be backend.Backend) error {
			ctx := cmd.Context()

			// HPC may need project registration from --project-file.
			if err := ensureProjectRegistered(cmd, be, jobCfg.Project); err != nil {
				return err
			}

			// ── dry-run ──
			if dryRun || dryRunAlias {
				return submitDryRun(ctx, be, jobCfg, noPreflight, jsonOut)
			}

			// ── submit ──
			jobID, n, err := be.SubmitJob(ctx, jobCfg, backend.SubmitOptions{
				SkipPreflight: noPreflight,
				Target:        explicitSubmitTarget(cmd),
			})
			if err != nil {
				return err
			}
			target, source, project := "", "", jobCfg.Project
			var consumers []consumer.Consumer
			if detail, err := be.GetJob(ctx, jobID); err == nil && detail != nil {
				target, source = detail.Job.Target, detail.Job.TargetSource
				consumers = detail.Consumers
				if detail.Job.Project != "" {
					project = detail.Job.Project
				}
			}
			jobURL := dashboard.JobURL(dashboardBase(ctx, be), project, jobID)
			if jsonOut {
				printJSON(struct {
					Consumers    []consumer.Consumer `json:"consumers"`
					JobID        string              `json:"job_id"`
					TotalTasks   int                 `json:"total_tasks"`
					Target       string              `json:"target,omitempty"`
					TargetSource string              `json:"target_source,omitempty"`
					URL          string              `json:"url,omitempty"`
				}{JobID: jobID, TotalTasks: n, Target: target, TargetSource: source, Consumers: consumers, URL: jobURL})
				return nil
			}
			fmt.Printf("submitted job %s (%d tasks)\n", utils.IDColor(jobID), n)
			if target != "" {
				fmt.Printf("target: %s (%s)\n", target, source)
			}
			printDashboardLink(os.Stdout, jobURL)
			if len(jobCfg.Consumers) > 0 {
				list, err := be.(backend.Consumers).ListConsumers(ctx, jobID)
				if err != nil {
					return err
				}
				for _, c := range list {
					fmt.Printf("consumer %s (%s): run on a CPU node with workspace access: bash %s\n", c.Name, c.ID, utils.ShellQuote(c.RunSH))
				}
			}
			printGPUHint(ctx, be)

			if watch {
				return watchJob(ctx, be, jobID)
			}
			return nil
		})
	},
}

// ensureProjectRegistered registers a project from --project-file if provided.
// The project is registered in the DB so SubmitJob can find it by name.
func ensureProjectRegistered(cmd *cobra.Command, be backend.Backend, jobProject string) error {
	pf, _ := cmd.Flags().GetString("project-file")
	if pf == "" {
		return nil
	}
	buf, err := os.ReadFile(pf)
	if err != nil {
		return err
	}
	var cfg project.Config
	if err := yaml.Unmarshal(buf, &cfg); err != nil {
		return fmt.Errorf("parse %s: %w", pf, err)
	}
	if cfg.ProjectName == "" {
		cfg.ProjectName = jobProject
	}
	if cfg.Target == "" {
		return fmt.Errorf("%s: project.target is required", pf)
	}
	ctx := cmd.Context()
	if _, err := be.GetProject(ctx, cfg.ProjectName); err != nil {
		return be.CreateProject(ctx, cfg)
	}
	return be.UpdateProject(ctx, cfg)
}

// submitDryRun shows the task expansion table plus optional preview info.
// HPC backends render the full run.sh + submit command via PreviewSubmit;
// other backends get a parameter table + command preview from DryRun.
// The preview attempt is always made — the daemon routes to the correct
// target backend and returns ErrNotSupported for backends without a
// submit template.
func submitDryRun(ctx context.Context, be backend.Backend, jobCfg job2.JobConfig, noPreflight, jsonOut bool) error {
	out, err := be.PreviewSubmit(ctx, jobCfg, noPreflight)
	if err != nil {
		return err
	}
	if jsonOut {
		printJSON(out)
	} else {
		fmt.Println(out.Preview)
	}
	if !out.Preflight.OK() {
		return fmt.Errorf("preflight failed; nothing submitted")
	}
	return nil
}

func explicitSubmitTarget(cmd *cobra.Command) string {
	if !cmd.Flags().Changed("target") {
		return ""
	}
	target, _ := cmd.Flags().GetString("target")
	return target
}

func watchJob(ctx context.Context, be backend.Backend, jobID string) error {
	writer := uilive.New()
	writer.Start()
	defer writer.Stop()

	ticker := time.NewTicker(time.Second)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	for {
		select {
		case <-sigChan:
			fmt.Println("Kill signal received!")
			return nil
		case <-ticker.C:
			detail, err := be.GetJob(ctx, jobID)
			if err != nil {
				return err
			}
			table := tablewriter.NewTable(writer)
			table.Header([]string{"ID", "STATUS", "GPUS", "SCRIPT"})
			data := make([][]string, 0, len(detail.Tasks))
			for _, task := range detail.Tasks {
				duration := "-"
				if task.ElapsedSec != nil {
					duration = formatTaskDuration(task.ElapsedSec, task.TimeEstimated)
				}
				data = append(data, []string{
					task.ID, task.Status, task.GPUs,
					duration,
				})
			}
			if err := table.Bulk(data); err != nil {
				return err
			}
			if err := table.Render(); err != nil {
				return err
			}
		}
	}
}

func init() {
	submitCmd.Flags().Bool("dry", false, "Expand sweep and print tasks without submitting")
	submitCmd.Flags().Bool("dry-run", false, "Expand sweep and print tasks without submitting")
	submitCmd.Flags().Bool("watch", false, "Block and show live progress after submit")
	submitCmd.Flags().String("project", "", "Override the project name in the YAML")
	submitCmd.Flags().StringP("note", "n", "", "Experiment note (overrides YAML note: field)")
	submitCmd.Flags().String("project-file", "", "Upsert project config from a YAML file (also writes on --dry)")
	submitCmd.Flags().Bool("json", false, "output raw JSON")
	submitCmd.Flags().Bool(
		"no-preflight",
		false,
		"Skip submit-time checks (imports, pip check, path args, writability). "+
			"Use when preflight misclassifies a runtime-only path or conditional import.",
	)
	submitCmd.Flags().StringP("target", "t", "", "Execution target override (-t > job.target > project.target)")
	// One documented spelling each. --dry-run is a legacy alias; --watch blocks
	// the caller (agents should check with `runq ps <job> --json` instead).
	_ = submitCmd.Flags().MarkHidden("dry-run")
	_ = submitCmd.Flags().MarkHidden("watch")

	submitCmd.GroupID = groupCore
	rootCmd.AddCommand(submitCmd)
}

// printGPUHint prints a one-line free-GPU hint after submit (local GPU
// targets only; HPC backends report no GPU slots and stay silent).
func printGPUHint(ctx context.Context, be backend.Backend) {
	gpus, err := be.GPUStatus(ctx)
	if err != nil || len(gpus) == 0 {
		return
	}
	total := len(gpus)
	free := 0
	for _, g := range gpus {
		if g.TaskID == "" {
			free++
		}
	}
	if free == 0 {
		fmt.Printf("  queued: waiting for GPUs (0/%d free)\n", total)
	} else if free < total {
		fmt.Printf("  %d/%d GPUs free — some tasks may queue\n", free, total)
	}
}
