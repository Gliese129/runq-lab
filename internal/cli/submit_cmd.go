package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	Use:   "submit <dr…> | submit [job.yaml | . | -]",
	Short: "Submit a draft (dr…); at a terminal, plan and submit a job file interactively",
	Long: `Submit a draft made by 'runq draft': it is frozen into a new job. Submitting
the same draft again returns the same job; --rev N refuses if the draft was
edited since you read rev N.

At a terminal, 'runq submit job.yaml' (or '-' for stdin, or no argument to
start from a template in $EDITOR) plans the file as a draft and shows it —
preview and run table — then waits for one key:

  s  submit        e  edit in $EDITOR        p  pick the project
  q  quit, keeping the draft (runq draft ps)

The project is the file's project:, else -p, else the most recently used
project of this directory. Outside a terminal (scripts, agents) a job file is
refused: use 'runq draft job.yaml', then 'runq submit dr…'.`,
	Example: `  runq draft job.yaml && runq submit dr8f2k --json   # agents and scripts
  runq submit job.yaml                                 # at a terminal: plan, look, press s
  runq submit                                          # at a terminal: start from a template`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		file := ""
		if len(args) == 1 {
			file = args[0]
		}
		if strings.HasPrefix(file, "dr") {
			if _, err := os.Stat(file); err != nil {
				rev, _ := cmd.Flags().GetInt("rev")
				noPreflight, _ := cmd.Flags().GetBool("no-preflight")
				jsonOut, _ := cmd.Flags().GetBool("json")
				return submitDraft(cmd, file, rev, noPreflight, jsonOut)
			}
		}
		dryRun, _ := cmd.Flags().GetBool("dry")
		dryRunAlias, _ := cmd.Flags().GetBool("dry-run")
		jsonFlag, _ := cmd.Flags().GetBool("json")
		if !dryRun && !dryRunAlias {
			tty, ok := interactive()
			if !ok || jsonFlag {
				shown := file
				if shown == "" {
					shown = "job.yaml"
				}
				return fmt.Errorf("outside a terminal, submit takes a draft: runq draft %s, then runq submit dr…", shown)
			}
			defer tty.Close()
			return runSubmitTUI(cmd, tty, file)
		}
		if file == "" {
			return fmt.Errorf("--dry needs a job file (or use: runq draft <job.yaml>)")
		}
		fmt.Fprintf(os.Stderr, "note: --dry is going away; `runq draft %s` shows the same plan and keeps it as a draft\n", file)
		watch, _ := cmd.Flags().GetBool("watch")
		projectOverride, _ := cmd.Flags().GetString("project")
		noteOverride, _ := cmd.Flags().GetString("note")
		sectionOverride, _ := cmd.Flags().GetString("section")
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
			jobCfg.Name, jobCfg.Note = noteOverride, ""
		}
		if sectionOverride != "" {
			jobCfg.Section = sectionOverride
		}
		jobCfg.SourcePath = args[0]
		if jobCfg.SourcePath == "." {
			jobCfg.SourcePath = "job.yaml"
		}
		for _, w := range jobCfg.NormalizeLegacy() {
			fmt.Fprintf(os.Stderr, "%s: %s\n", filepath.Base(file), w)
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
	submitCmd.Flags().StringP("note", "n", "", "Job name (overrides the YAML name: field)")
	submitCmd.Flags().Int("rev", 0, "with dr…: refuse unless the draft is still at this rev")
	submitCmd.Flags().StringP("section", "s", "", "Section to file the job under (must exist; default: the project's rules, else unsorted)")
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

// runSubmitTUI is `runq submit [job.yaml | - ]` at a terminal: read the job
// (a file, stdin, or a fresh template in $EDITOR), then hand it to the TUI.
func runSubmitTUI(cmd *cobra.Command, tty *os.File, file string) error {
	projectFlag, _ := cmd.Flags().GetString("project")
	name, _ := cmd.Flags().GetString("note")
	section, _ := cmd.Flags().GetString("section")
	noPreflight, _ := cmd.Flags().GetBool("no-preflight")
	edit := openEditor(tty)
	return withBackend(cmd, func(be backend.Backend) error {
		ctx := cmd.Context()
		d, ok := be.(backend.Drafter)
		if !ok {
			return fmt.Errorf("this backend does not support drafts")
		}
		if err := ensureProjectRegistered(cmd, be, projectFlag); err != nil {
			return err
		}
		candidates := projectCandidates(ctx, be)
		var text string
		switch file {
		case "":
			t, err := templateYAML(firstNonEmpty(projectFlag, defaultProject(ctx, be, "", candidates)), edit)
			if err != nil {
				return err
			}
			if t == "" {
				fmt.Println("nothing to plan (the template was left as is)")
				return nil
			}
			text = t
		case "-":
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			text = string(data)
		default:
			path := file
			if path == "." {
				path = "job.yaml"
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text = string(data)
		}
		ui := &submitTUI{
			ctx: ctx, d: d, out: os.Stdout, key: ttyKey(tty), edit: edit, projects: candidates,
			skipPreflight: noPreflight,
			req: backend.DraftRequest{JobYAML: text, SourcePath: sourcePathFor(file), Section: section, Name: name,
				Target: explicitSubmitTarget(cmd), Project: firstNonEmpty(projectFlag, defaultProject(ctx, be, text, candidates))},
		}
		return ui.run()
	})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
