package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/dashboard"
	"github.com/gliese129/runq-lab/internal/preflight"
	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var draftCmd = &cobra.Command{
	Use:   "draft <job.yaml | -> | draft ps|show|edit|discard",
	Short: "Plan an experiment (or an append to one) as an editable draft; submit it with runq submit dr…",
	Long: `Create a draft from a job file ('-' reads stdin). The draft is the real,
expanded plan — the same rows, names, rendered command and submit line that
submitting would use — kept server-side as dr…. Nothing is synced, probed,
queued or written to the repo. Edit it (from a file or in $EDITOR), then
freeze it into a job with 'runq submit dr…'.

The project must be explicit: job.yaml 'project:' or -p. Each project keeps
its newest 5 open drafts; creating a 6th discards the oldest.

--into ex… makes the draft an append to an existing experiment of the same
project and target. A cell is a row name: a row whose name the experiment has is a
new version of that cell when its selected version failed or was killed, and
is skipped when it succeeded or still runs (--rerun <cell> runs a succeeded
one again). Such a row must keep the cell's params, except the ones only the
scheduler reads (walltime, reservation id); a changed command is allowed and
flagged.`,
	Example: `  runq draft configs/runq/a1-batch1.yaml
  runq draft job.yaml -s "KL 消融" -t tsubame
  runq draft more-seeds.yaml --into ex6573   # add cells to an existing experiment
  runq show dr8f2k --yaml > plan.yaml         # edit, then:
  runq draft edit dr8f2k -f plan.yaml
  runq submit dr8f2k`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		src := args[0]
		var data []byte
		var err error
		if src == "-" {
			data, err = io.ReadAll(os.Stdin)
		} else {
			if src == "." {
				src = "job.yaml"
			}
			data, err = os.ReadFile(src)
		}
		if err != nil {
			return err
		}
		project, _ := cmd.Flags().GetString("project")
		section, _ := cmd.Flags().GetString("section")
		jsonOut, _ := cmd.Flags().GetBool("json")
		into, _ := cmd.Flags().GetString("into")
		rerun, _ := cmd.Flags().GetStringArray("rerun")
		req := backend.DraftRequest{JobYAML: string(data), Project: project, Section: section, Target: explicitSubmitTarget(cmd),
			Into: into, Rerun: rerun}
		if src != "-" {
			req.SourcePath = src
		}
		return withDrafter(cmd, func(d backend.Drafter) error {
			res, err := d.CreateDraft(cmd.Context(), req)
			if err != nil {
				return err
			}
			if jsonOut {
				printJSON(res)
				return nil
			}
			for _, w := range res.Warnings {
				fmt.Fprintf(os.Stderr, "%s: %s\n", filepath.Base(src), w)
			}
			if w := projectFileWarning(cmd, res.Draft.Project); w != "" {
				fmt.Fprintln(os.Stderr, w)
			}
			fmt.Print(res.Preview)
			fmt.Println()
			fmt.Print(res.Table)
			for _, id := range res.Discarded {
				fmt.Printf("discarded oldest draft %s (5 open drafts per project)\n", id)
			}
			printDraftLine(res.Draft)
			printDashboardLink(os.Stdout, dashboard.DraftURL(dashboardBase(cmd.Context(), d.(backend.Backend)), res.Draft.ID))
			fmt.Printf("next: runq submit %s\n", res.Draft.ID)
			return nil
		})
	},
}

func printDraftLine(v backend.DraftView) {
	fmt.Println(draftLine(v))
}

func draftLine(v backend.DraftView) string {
	name := v.Name
	if name == "" {
		name = "(no name)"
	}
	line := fmt.Sprintf("draft %s (rev %d) · project %s · %q · %d run(s)", utils.IDColor(v.ID), v.Rev, v.Project, name, v.Rows)
	if v.NeedsCode > 0 {
		line += fmt.Sprintf(" · %d need code", v.NeedsCode)
	}
	if v.Into != "" {
		line += " · into " + v.Into
		if len(v.Rerun) > 0 {
			line += " (rerun " + strings.Join(v.Rerun, ", ") + ")"
		}
	}
	return line
}

// submittedLine reports a draft submit (a new job, or an append).
func submittedLine(res *backend.DraftSubmitResult) string {
	switch {
	case res.AlreadySubmitted:
		return fmt.Sprintf("draft %s was already submitted as experiment %s (%d versions)", res.DraftID, utils.IDColor(res.JobID), res.TotalTasks)
	case res.Append != nil:
		return fmt.Sprintf("appended %d version(s) to experiment %s as submission %d (%s) from draft %s", res.Append.Submits(), utils.IDColor(res.JobID), res.Append.Batch, res.Append.Line(), res.DraftID)
	}
	return fmt.Sprintf("submitted experiment %s (%d versions) from draft %s", utils.IDColor(res.JobID), res.TotalTasks, res.DraftID)
}

// printPreflightWarnings lists a submit's preflight warnings — it went
// through anyway; they say what may fail once it runs.
func printPreflightWarnings(w io.Writer, warns []preflight.CheckResult) {
	if len(warns) == 0 {
		return
	}
	fmt.Fprintf(w, "preflight: %d warning(s) — submitted anyway; a smoke experiment tells you whether it runs\n", len(warns))
	fmt.Fprint(w, preflight.Format(warns))
}

func withDrafter(cmd *cobra.Command, fn func(backend.Drafter) error) error {
	return withBackend(cmd, func(be backend.Backend) error {
		d, ok := be.(backend.Drafter)
		if !ok {
			return fmt.Errorf("this backend does not support drafts")
		}
		return fn(d)
	})
}

var draftPsCmd = &cobra.Command{
	Use:   "ps",
	Short: "List open drafts, newest first",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		project, _ := cmd.Flags().GetString("project")
		jsonOut, _ := cmd.Flags().GetBool("json")
		return withDrafter(cmd, func(d backend.Drafter) error {
			list, err := d.ListDrafts(cmd.Context(), project)
			if err != nil {
				return err
			}
			if jsonOut {
				printJSON(list)
				return nil
			}
			if len(list) == 0 {
				fmt.Println("no open drafts")
				return nil
			}
			w := newTable()
			fmt.Fprintf(w, "DRAFT\tPROJECT\tNAME\tRUNS\tREV\tTARGET\tSOURCE\n")
			for _, v := range list {
				rev := fmt.Sprint(v.Rev)
				if v.Edited {
					rev += " (edited)"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n", utils.IDColor(v.ID), v.Project, v.Name, v.Rows, rev, v.Target, v.SourcePath)
			}
			return w.Flush()
		})
	},
}

// showDraft is `runq show dr…`: the plan table, or with --yaml the
// editable form for `runq draft edit -f`.
func showDraft(cmd *cobra.Command, args []string) error {
	asYAML, _ := cmd.Flags().GetBool("yaml")
	jsonOut, _ := cmd.Flags().GetBool("json")
	return withDrafter(cmd, func(d backend.Drafter) error {
		if asYAML {
			out, err := d.DraftYAML(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			_, err = os.Stdout.Write(out)
			return err
		}
		res, err := d.GetDraft(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if jsonOut {
			printJSON(res)
			return nil
		}
		printDraftLine(res.Draft)
		if res.Draft.Status == "submitted" {
			fmt.Printf("submitted as %s\n", res.Draft.JobID)
		}
		if res.Draft.Description != "" {
			fmt.Println(res.Draft.Description)
		}
		fmt.Println()
		fmt.Print(res.Table)
		if res.Append != nil {
			fmt.Println()
			fmt.Print(res.Append.Report())
		}
		return nil
	})
}

var draftEditCmd = &cobra.Command{
	Use:   "edit <dr…> [-f file]",
	Short: "Replace a draft's plan from a file (-f) or in $EDITOR",
	Long: `Edit a draft. With -f, the file is the draft's editable YAML (start from
'runq show dr… --yaml'); its rev: must be current. Without -f, the
draft opens in $EDITOR (default vi); on save it is validated and stored. A
failed save keeps the temporary file and prints its path. Rows, names,
params, fixed_params, baseline, name and description are editable; project
and target stay fixed (start a new draft for those).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		file, _ := cmd.Flags().GetString("file")
		return withDrafter(cmd, func(d backend.Drafter) error {
			if file != "" {
				var data []byte
				var err error
				if file == "-" {
					data, err = io.ReadAll(os.Stdin)
				} else {
					data, err = os.ReadFile(file)
				}
				if err != nil {
					return err
				}
				return saveDraftEdit(cmd, d, args[0], string(data), "")
			}
			if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
				return fmt.Errorf("no terminal; use: runq draft edit %s -f plan.yaml", args[0])
			}
			current, err := d.DraftYAML(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			tmp, err := os.CreateTemp("", "runq-draft-*.yaml")
			if err != nil {
				return err
			}
			if _, err := tmp.Write(current); err != nil {
				tmp.Close()
				return err
			}
			tmp.Close()
			editor := os.Getenv("VISUAL")
			if editor == "" {
				editor = os.Getenv("EDITOR")
			}
			if editor == "" {
				editor = "vi"
			}
			ed := exec.Command("sh", "-c", editor+` "$1"`, "sh", tmp.Name())
			ed.Stdin, ed.Stdout, ed.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := ed.Run(); err != nil {
				return fmt.Errorf("editor: %w (your edits are in %s)", err, tmp.Name())
			}
			edited, err := os.ReadFile(tmp.Name())
			if err != nil {
				return err
			}
			if string(edited) == string(current) {
				os.Remove(tmp.Name())
				fmt.Println("no changes")
				return nil
			}
			return saveDraftEdit(cmd, d, args[0], string(edited), tmp.Name())
		})
	},
}

// saveDraftEdit stores an edit and prints the new plan; on failure a kept
// temp file is named so the edits are not lost.
func saveDraftEdit(cmd *cobra.Command, d backend.Drafter, ref, text, tmp string) error {
	res, err := d.EditDraft(cmd.Context(), ref, text)
	if err != nil {
		hint := ""
		if strings.Contains(err.Error(), "changed") {
			hint = fmt.Sprintf("\nre-read it: runq show %s --yaml > plan.yaml", ref)
		}
		if tmp != "" {
			hint += "\nyour edits are kept in " + tmp
		}
		return fmt.Errorf("%w%s", err, hint)
	}
	if tmp != "" {
		os.Remove(tmp)
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	printDraftLine(res.Draft)
	fmt.Println()
	fmt.Print(res.Table)
	fmt.Printf("next: runq submit %s\n", res.Draft.ID)
	return nil
}

var draftDiscardCmd = &cobra.Command{
	Use:   "discard <dr…>",
	Short: "Delete an open draft",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withDrafter(cmd, func(d backend.Drafter) error {
			if err := d.DiscardDraft(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Printf("draft %s discarded\n", args[0])
			return nil
		})
	},
}

// submitDraft is `runq submit dr…`.
func submitDraft(cmd *cobra.Command, ref string, opts backend.DraftSubmitOptions, jsonOut bool) error {
	return withBackend(cmd, func(be backend.Backend) error {
		d, ok := be.(backend.Drafter)
		if !ok {
			return errors.New("this backend does not support drafts")
		}
		res, err := d.SubmitDraft(cmd.Context(), ref, opts)
		if err != nil {
			return err
		}
		project, target, source := "", "", ""
		if detail, err := be.GetJob(cmd.Context(), res.JobID); err == nil && detail != nil {
			project, target, source = detail.Job.Project, detail.Job.Target, detail.Job.TargetSource
		}
		url := ""
		if project != "" {
			url = dashboard.JobURL(dashboardBase(cmd.Context(), be), project, res.JobID)
		}
		if jsonOut {
			printJSON(struct {
				*backend.DraftSubmitResult
				Target string `json:"target,omitempty"`
				URL    string `json:"url,omitempty"`
			}{res, target, url})
			return nil
		}
		fmt.Println(submittedLine(res))
		printPreflightWarnings(os.Stdout, res.PreflightWarnings)
		if target != "" {
			fmt.Printf("target: %s (%s)\n", target, source)
		}
		printDashboardLink(os.Stdout, url)
		printGPUHint(cmd.Context(), be)
		return nil
	})
}

func init() {
	draftCmd.Flags().StringP("project", "p", "", "project (name or id); default: the job file's project:")
	draftCmd.Flags().StringP("section", "s", "", "section to file the experiment under (must exist)")
	draftCmd.Flags().StringP("target", "t", "", "execution target override (-t > job.target > project.target)")
	draftCmd.Flags().String("into", "", "append to this experiment (ex…) instead of creating one; failed cells get a new version, succeeded ones are skipped")
	draftCmd.Flags().StringArray("rerun", nil, "with --into: run this succeeded cell again (repeatable)")
	draftCmd.Flags().Bool("rerun-failed", false, "")
	retireFlag(draftCmd, "rerun-failed", "with --into, failed and killed cells get a new version by default; --rerun <cell> runs a succeeded one again")
	draftCmd.Flags().Bool("json", false, "output raw JSON")
	draftPsCmd.Flags().StringP("project", "p", "", "only this project")
	draftPsCmd.Flags().Bool("json", false, "output raw JSON")
	draftEditCmd.Flags().StringP("file", "f", "", "the edited draft YAML ('-' = stdin)")
	draftCmd.AddCommand(draftPsCmd, draftEditCmd, draftDiscardCmd)
	draftCmd.GroupID = groupCore
	rootCmd.AddCommand(draftCmd)
}

// projectFileWarning: runq never re-reads project.yaml, so when the file a
// project was registered from has changed since, say how to apply it.
func projectFileWarning(cmd *cobra.Command, project string) string {
	var out string
	_ = withBackend(cmd, func(be backend.Backend) error {
		cfg, err := be.GetProject(cmd.Context(), project)
		if err != nil || cfg.SourcePath == "" || cfg.SourceSHA == "" {
			return nil
		}
		data, err := os.ReadFile(cfg.SourcePath)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != cfg.SourceSHA {
			out = fmt.Sprintf("warning: %s changed since project %s was registered; apply it with: runq project %s", cfg.SourcePath, project, utils.ShellQuote(cfg.SourcePath))
		}
		return nil
	})
	return out
}
