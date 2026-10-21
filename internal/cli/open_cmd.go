package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/dashboard"
	"github.com/spf13/cobra"
)

// ── runq open ──

var openCmd = &cobra.Command{
	Use:   "open [job_id | task_id]",
	Short: "Open a job or task page in the dashboard",
	Long: `Print the dashboard URL of a job (jb…) or task (tk…) and open it in the
browser. With no argument, opens the dashboard home. On a machine without a
browser the printed URL is the result — open it on your own machine.

The same URL is the "url" field of submit / ps / task show --json.`,
	Example: `  runq open jb1a2b3c4d
  runq open tk5e6f7a8b
  runq open`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withBackend(cmd, func(be backend.Backend) error {
			ctx := cmd.Context()
			base := dashboardBase(ctx, be)
			if base == "" {
				return fmt.Errorf("the runq service is not serving the dashboard UI (check `runq serve status`, or reinstall with the dashboard)")
			}
			address := base
			if len(args) == 1 {
				var err error
				if address, err = pageForID(cmd, be, base, args[0]); err != nil {
					return err
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), address)
			return openBrowser(ctx, cmd.ErrOrStderr(), address)
		})
	},
}

// pageForID resolves a jb…/tk… id to its dashboard page. Ids without a
// known prefix are tried as a job, then as a task.
func pageForID(cmd *cobra.Command, be backend.Backend, base, id string) (string, error) {
	ctx := cmd.Context()
	jobPage := func() (string, error) {
		detail, err := be.GetJob(ctx, id)
		if err != nil {
			return "", err
		}
		return dashboard.JobURL(base, detail.Job.Project, detail.Job.ID), nil
	}
	taskPage := func() (string, error) {
		view, err := be.GetTask(ctx, id)
		if err != nil {
			return "", err
		}
		return dashboard.TaskURL(base, view.Project, view.JobID, view.ID), nil
	}
	switch {
	case strings.HasPrefix(id, "jb"):
		return jobPage()
	case strings.HasPrefix(id, "tk"):
		return taskPage()
	}
	if u, err := jobPage(); err == nil || !errors.Is(err, backend.ErrNotFound) {
		return u, err
	}
	return taskPage()
}

func init() {
	openCmd.GroupID = groupCore
	rootCmd.AddCommand(openCmd)
}
