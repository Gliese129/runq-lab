package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/dashboard"
	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/spf13/cobra"
)

// ── runq ps / ls（D16 双视角）──
//
// 无参数 = job 表（GET /jobs）；带 job_id = 该 job 的 task 表（GET
// /tasks?job=）。两个视角共用 --json / --target / --fresh。

var psCmd = &cobra.Command{
	Use:     "ps [job_id]",
	Aliases: []string{"ls"},
	Short:   "List jobs, or the tasks of one job",
	Example: `  runq ps               # job table
  runq ps 8f2a           # task table for job 8f2a
  runq ps --json
  runq ps -t tsubame --fresh
  runq ps 8f2a --wait --json   # block until a task changes state (max 30m)`,
	Args:        cobra.MaximumNArgs(1),
	Annotations: map[string]string{waitAnnotation: "ps"},
	RunE:        runPs,
}

func runPs(cmd *cobra.Command, args []string) error {
	jsonOut, _ := cmd.Flags().GetBool("json")

	return withBackend(cmd, func(be backend.Backend) error {
		// Task view: ps <job_id> → flat task table.
		if len(args) == 1 {
			jobID, err := resolveJobRef(cmd.Context(), be, args[0]) // unique prefix is enough
			if err != nil {
				return err
			}
			applyFresh(cmd, be, jobID)
			opts := backend.TaskListOptions{JobID: jobID}
			wait, err := runWait(cmd, func(ctx context.Context) (waitSnapshot, error) {
				tasks, _, err := be.ListTasks(ctx, opts)
				return tasksSnapshot(tasks), err
			})
			if err != nil {
				return err
			}
			tasks, _, err := be.ListTasks(cmd.Context(), opts)
			if err != nil {
				return err
			}
			base := dashboardBase(cmd.Context(), be)
			for i := range tasks {
				t := &tasks[i]
				t.URL = dashboard.TaskURL(base, t.Project, t.JobID, t.ID)
			}
			if jsonOut {
				printJSON(tasks)
				wait.print(os.Stderr) // stdout stays the array; tasks carry refreshed_at
				return nil
			}
			if err := printDashboardTasks(tasks); err != nil {
				return err
			}
			if len(tasks) > 0 {
				printDashboardLink(os.Stdout, dashboard.JobURL(base, tasks[0].Project, tasks[0].JobID))
			}
			wait.print(os.Stdout)
			return nil
		}

		// Job view.
		applyFresh(cmd, be, "")
		wait, err := runWait(cmd, func(ctx context.Context) (waitSnapshot, error) {
			jobs, err := be.ListJobs(ctx, "")
			return jobsSnapshot(jobs), err
		})
		if err != nil {
			return err
		}
		jobs, err := be.ListJobs(cmd.Context(), "")
		if err != nil {
			return err
		}
		base := dashboardBase(cmd.Context(), be)
		for i := range jobs {
			jobs[i].URL = dashboard.JobURL(base, jobs[i].Project, jobs[i].ID)
		}
		if jsonOut {
			printJSON(jobs)
			wait.print(os.Stderr)
			return nil
		}
		if err := printDashboardJobs(jobs); err != nil {
			return err
		}
		if d, ok := be.(backend.Drafter); ok {
			if drafts, err := d.ListDrafts(cmd.Context(), ""); err == nil && len(drafts) > 0 {
				fmt.Println(utils.Dimf("%d open draft(s) (runq draft ps)", len(drafts)))
			}
		}
		printDashboardLink(os.Stdout, base)
		wait.print(os.Stdout)
		return nil
	})
}

func init() {
	psCmd.Flags().Bool("json", false, "output raw JSON")
	psCmd.Flags().StringP("target", "t", "", "Filter by compute target")

	psCmd.GroupID = groupCore
	rootCmd.AddCommand(psCmd)
}
