package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/spf13/cobra"
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
		if err := checkRetiredFlags(cmd); err != nil {
			return err
		}
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
		jsonFlag, _ := cmd.Flags().GetBool("json")
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
	},
}

func explicitSubmitTarget(cmd *cobra.Command) string {
	if !cmd.Flags().Changed("target") {
		return ""
	}
	target, _ := cmd.Flags().GetString("target")
	return target
}

func init() {
	// Retired in rc6: kept parseable so their use names the replacement.
	submitCmd.Flags().Bool("dry", false, "")
	submitCmd.Flags().Bool("dry-run", false, "")
	submitCmd.Flags().Bool("watch", false, "")
	submitCmd.Flags().String("project", "", "")
	submitCmd.Flags().StringP("note", "n", "", "")
	submitCmd.Flags().String("project-file", "", "")
	retireFlag(submitCmd, "dry", "`runq draft <job.yaml>` shows the plan and keeps it as a draft")
	retireFlag(submitCmd, "dry-run", "`runq draft <job.yaml>` shows the plan and keeps it as a draft")
	retireFlag(submitCmd, "watch", "follow progress with `runq ps <jb…>` or on the dashboard")
	retireFlag(submitCmd, "project", "set project: in the job file (or `runq draft -p <project>`)")
	retireFlag(submitCmd, "note", "set name: in the job file")
	retireFlag(submitCmd, "project-file", "register or update the project first with `runq project <project.yaml>`")
	submitCmd.Flags().Int("rev", 0, "with dr…: refuse unless the draft is still at this rev")
	submitCmd.Flags().StringP("section", "s", "", "Section to file the job under (must exist; default: the project's rules, else unsorted)")
	submitCmd.Flags().Bool("json", false, "output raw JSON")
	submitCmd.Flags().Bool(
		"no-preflight",
		false,
		"Skip submit-time checks (imports, pip check, path args, writability). "+
			"Use when preflight misclassifies a runtime-only path or conditional import.",
	)
	submitCmd.Flags().StringP("target", "t", "", "Execution target override (-t > job.target > project.target)")

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
	section, _ := cmd.Flags().GetString("section")
	noPreflight, _ := cmd.Flags().GetBool("no-preflight")
	edit := openEditor(tty)
	return withBackend(cmd, func(be backend.Backend) error {
		ctx := cmd.Context()
		d, ok := be.(backend.Drafter)
		if !ok {
			return fmt.Errorf("this backend does not support drafts")
		}
		candidates := projectCandidates(ctx, be)
		var text string
		switch file {
		case "":
			t, err := templateYAML(defaultProject(ctx, be, "", candidates), edit)
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
			fileWarning:   func(project string) string { return projectFileWarning(cmd, project) },
			req: backend.DraftRequest{JobYAML: text, SourcePath: sourcePathFor(file), Section: section,
				Target: explicitSubmitTarget(cmd), Project: defaultProject(ctx, be, text, candidates)},
		}
		return ui.run()
	})
}
