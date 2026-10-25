package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/dashboard"
	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var draftCmd = &cobra.Command{
	Use:   "draft <job.yaml | -> | draft ps|show|edit|discard",
	Short: "Plan a job as an editable draft (preview included); submit it with runq submit dr…",
	Long: `Create a draft from a job file ('-' reads stdin). The draft is the real,
expanded plan — the same rows, names, rendered command and submit line that
submitting would use — kept server-side as dr…. Nothing is synced, probed,
queued or written to the repo. Edit it (from a file or in $EDITOR), then
freeze it into a job with 'runq submit dr…'.

The project must be explicit: job.yaml 'project:' or -p. Each project keeps
its newest 5 open drafts; creating a 6th discards the oldest.`,
	Example: `  runq draft configs/runq/a1-batch1.yaml
  runq draft job.yaml -s "KL 消融" -t tsubame
  runq draft show dr8f2k --yaml > plan.yaml   # edit, then:
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
		req := backend.DraftRequest{JobYAML: string(data), Project: project, Section: section, Target: explicitSubmitTarget(cmd)}
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
			fmt.Print(res.Preview)
			fmt.Println()
			fmt.Print(res.Table)
			for _, id := range res.Discarded {
				fmt.Printf("discarded oldest draft %s (5 open drafts per project)\n", id)
			}
			printDraftLine(res.Draft)
			fmt.Printf("next: runq submit %s\n", res.Draft.ID)
			return nil
		})
	},
}

func printDraftLine(v backend.DraftView) {
	name := v.Name
	if name == "" {
		name = "(no name)"
	}
	fmt.Printf("draft %s (rev %d) · project %s · %q · %d run(s)", utils.IDColor(v.ID), v.Rev, v.Project, name, v.Rows)
	if v.NeedsCode > 0 {
		fmt.Printf(" · %d need code", v.NeedsCode)
	}
	fmt.Println()
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

var draftShowCmd = &cobra.Command{
	Use:   "show <dr…>",
	Short: "Show a draft's plan table (--yaml: its editable form)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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
			return nil
		})
	},
}

var draftEditCmd = &cobra.Command{
	Use:   "edit <dr…> [-f file]",
	Short: "Replace a draft's plan from a file (-f) or in $EDITOR",
	Long: `Edit a draft. With -f, the file is the draft's editable YAML (start from
'runq draft show dr… --yaml'); its rev: must be current. Without -f, the
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
			hint = fmt.Sprintf("\nre-read it: runq draft show %s --yaml > plan.yaml", ref)
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
func submitDraft(cmd *cobra.Command, ref string, rev int, noPreflight, jsonOut bool) error {
	return withBackend(cmd, func(be backend.Backend) error {
		d, ok := be.(backend.Drafter)
		if !ok {
			return errors.New("this backend does not support drafts")
		}
		res, err := d.SubmitDraft(cmd.Context(), ref, rev, noPreflight)
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
		if res.AlreadySubmitted {
			fmt.Printf("draft %s was already submitted as job %s (%d tasks)\n", res.DraftID, utils.IDColor(res.JobID), res.TotalTasks)
		} else {
			fmt.Printf("submitted job %s (%d tasks) from draft %s\n", utils.IDColor(res.JobID), res.TotalTasks, res.DraftID)
		}
		if target != "" {
			fmt.Printf("target: %s (%s)\n", target, source)
		}
		printDashboardLink(os.Stdout, url)
		return nil
	})
}

func init() {
	draftCmd.Flags().StringP("project", "p", "", "project (name or id); default: the job file's project:")
	draftCmd.Flags().StringP("section", "s", "", "section to file the job under (must exist)")
	draftCmd.Flags().StringP("target", "t", "", "execution target override (-t > job.target > project.target)")
	draftCmd.Flags().Bool("json", false, "output raw JSON")
	draftPsCmd.Flags().StringP("project", "p", "", "only this project")
	draftPsCmd.Flags().Bool("json", false, "output raw JSON")
	draftShowCmd.Flags().Bool("yaml", false, "print the editable YAML (for draft edit -f)")
	draftShowCmd.Flags().Bool("json", false, "output raw JSON")
	draftEditCmd.Flags().StringP("file", "f", "", "the edited draft YAML ('-' = stdin)")
	draftCmd.AddCommand(draftPsCmd, draftShowCmd, draftEditCmd, draftDiscardCmd)
	draftCmd.GroupID = groupCore
	rootCmd.AddCommand(draftCmd)
}
