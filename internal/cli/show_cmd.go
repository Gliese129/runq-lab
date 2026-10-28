package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/dashboard"
	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ── runq show / archive / dashboard (rc6: one entry point per verb) ──

var showCmd = &cobra.Command{
	Use:   "show <jb… | tk… | dr… | pj… | project-name>",
	Short: "Show one experiment, version, draft or project",
	Long: `Show anything by its id (a unique prefix is enough): a job (jb…: name,
status counts, target, section, draft, and its run table), a task (tk…), a
draft (dr…), or a project (pj… or its name).`,
	Example: `  runq show jb7817
  runq show tk5e6f7a8b --json
  runq show tij-slime-guru-a1
  runq show jb7817 --wait=2h   # block until the job's state changes or it finishes`,
	Args:        cobra.ExactArgs(1),
	Annotations: map[string]string{waitAnnotation: "show"},
	RunE: func(cmd *cobra.Command, args []string) error {
		ref := args[0]
		jsonOut, _ := cmd.Flags().GetBool("json")
		if max, _ := waitMax(cmd); max > 0 && !isExperimentRef(ref) && !isVersionRef(ref) {
			return errWaitUnsupported
		}
		switch {
		case isVersionRef(ref):
			return runTaskShow(cmd, args)
		case strings.HasPrefix(ref, "dr"):
			return showDraft(cmd, args)
		case strings.HasPrefix(ref, "cs"):
			return consumerRetired(ref)
		}
		return withBackend(cmd, func(be backend.Backend) error {
			ctx := cmd.Context()
			switch {
			case isExperimentRef(ref):
				return showJob(cmd, be, ref, jsonOut)
			}
			cfg, err := be.GetProject(ctx, ref)
			if err != nil {
				return err
			}
			printJSON(cfg)
			return nil
		})
	},
}

func showJob(cmd *cobra.Command, be backend.Backend, ref string, jsonOut bool) error {
	ctx := cmd.Context()
	id, err := resolveJobRef(ctx, be, ref)
	if err != nil {
		return err
	}
	applyFresh(cmd, be, id)
	wait, err := runWait(cmd, func(ctx context.Context) (waitSnapshot, error) {
		d, err := be.GetJob(ctx, id)
		if err != nil {
			return waitSnapshot{}, err
		}
		return jobDetailSnapshot(d), nil
	})
	if err != nil {
		return err
	}
	detail, err := be.GetJob(ctx, id)
	if err != nil {
		return err
	}
	if jsonOut {
		printJSON(struct {
			*backend.JobDetail
			Wait *waitResult `json:"wait,omitempty"`
		}{detail, wait})
		return nil
	}
	j := detail.Job
	name := j.Name
	if name == "" {
		name = "(no name)"
	}
	fmt.Printf("%s  %s\n", name, utils.IDColor(j.ID))
	fmt.Printf("project %s · section %s · target %s · %s\n", j.Project, j.Section, j.Target, utils.StatusColor(j.Status))
	fmt.Printf("tasks: %d running, %d pending, %d failed, %d done\n", j.Tasks.Running, j.Tasks.Pending, j.Tasks.Failed, j.Tasks.Completed)
	if j.DraftID != "" {
		fmt.Printf("from draft %s\n", j.DraftID)
	}
	if len(detail.Batches) > 1 {
		parts := make([]string, len(detail.Batches))
		for i, b := range detail.Batches {
			parts[i] = fmt.Sprintf("%d: %d task(s) %s", b.Seq, b.TaskCount, time.Unix(b.SubmittedAt, 0).Format("2006-01-02 15:04"))
			if b.DraftID != "" {
				parts[i] += " from " + b.DraftID
			}
		}
		fmt.Printf("batches: %s\n", strings.Join(parts, " · "))
	}
	reruns := 0
	for _, t := range detail.Tasks {
		if t.SupersededBy != "" {
			reruns++
		}
	}
	if reruns > 0 {
		fmt.Printf("%d failed task(s) rerun; the old attempts stay as history (runq ps %s)\n", reruns, j.ID)
	}
	if j.Description != "" {
		fmt.Println(j.Description)
	}
	if ex, ok := be.(backend.JobExporter); ok {
		if text, err := ex.ExportJob(ctx, j.ID); err == nil {
			var cfg job.JobConfig
			if yaml.Unmarshal(text, &cfg) == nil {
				if rows, err := job.Rows(&cfg); err == nil {
					fmt.Println()
					fmt.Print(job.PlanTable(rows, cfg.Baseline, nil))
				}
			}
		}
	}
	printDashboardLink(os.Stdout, dashboard.JobURL(dashboardBase(ctx, be), j.Project, j.ID))
	wait.print(os.Stdout)
	return nil
}

// resolveJobRef expands a unique job-id prefix (archived jobs included).
var archiveCmd = &cobra.Command{
	Use:   "archive <jb… | pj… | project-name>",
	Short: "Hide an experiment or a project from default lists (--restore brings it back)",
	Long: `Archive hides a job, or a project with all its jobs, from default lists.
Nothing is deleted or moved; --restore undoes it. Projects with running or
pending jobs cannot be archived.`,
	Example: `  runq archive jb7817
  runq archive tij-slime-guru-tmp
  runq archive jb7817 --restore`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		restore, _ := cmd.Flags().GetBool("restore")
		ref := args[0]
		if isExperimentRef(ref) {
			return withBackend(cmd, func(be backend.Backend) error {
				id, err := resolveJobRef(cmd.Context(), be, ref)
				if err != nil {
					return err
				}
				return runJobArchive(cmd, id, !restore)
			})
		}
		return runProjectArchive(cmd, ref, !restore)
	},
}

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Open the dashboard in the browser",
	Long: `Print the dashboard URL and open it. Pages for one job, task or draft are
linked from 'runq show', 'runq ps' and 'runq draft' output. On a machine
without a browser the printed URL is the result.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return withBackend(cmd, func(be backend.Backend) error {
			base := dashboardBase(cmd.Context(), be)
			if base == "" {
				return fmt.Errorf("the runq service is not serving the dashboard UI (check `runq serve status`, or reinstall with the dashboard)")
			}
			fmt.Fprintln(cmd.OutOrStdout(), base)
			return openBrowser(cmd.Context(), cmd.ErrOrStderr(), base)
		})
	},
}

func init() {
	showCmd.Flags().Bool("json", false, "output raw JSON")
	showCmd.Flags().Bool("yaml", false, "with dr…: the draft's editable YAML")
	archiveCmd.Flags().Bool("restore", false, "bring it back to the default lists")
	showCmd.GroupID = groupCore
	archiveCmd.GroupID = groupManagement
	dashboardCmd.GroupID = groupCore
	rootCmd.AddCommand(showCmd, archiveCmd, dashboardCmd)
}

func runJobArchive(cmd *cobra.Command, jobID string, archive bool) error {
	verb := "archived"
	if !archive {
		verb = "unarchived"
	}
	return withBackend(cmd, func(be backend.Backend) error {
		var err error
		if archive {
			err = be.ArchiveJob(cmd.Context(), jobID)
		} else {
			err = be.UnarchiveJob(cmd.Context(), jobID)
		}
		if err != nil {
			return err
		}
		fmt.Printf("experiment %s %s\n", utils.IDColor(jobID), verb)
		return nil
	})
}
