package cli

import (
	"context"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/dashboard"
	"github.com/spf13/cobra"
)

func runTaskShow(cmd *cobra.Command, args []string) error {
	return withBackend(cmd, func(b backend.Backend) error {
		if fresh, _ := cmd.Root().PersistentFlags().GetBool("fresh"); fresh {
			if view, err := b.GetTask(cmd.Context(), args[0]); err == nil {
				applyFresh(cmd, b, view.JobID) // refresh is job-scoped
			}
		}
		wait, err := runWait(cmd, func(ctx context.Context) (waitSnapshot, error) {
			view, err := b.GetTask(ctx, args[0])
			if err != nil {
				return waitSnapshot{}, err
			}
			return taskSnapshot(view), nil
		})
		if err != nil {
			return err
		}
		view, err := b.GetTask(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		view.URL = dashboard.TaskURL(dashboardBase(cmd.Context(), b), view.Project, view.JobID, view.ID)
		printJSON(struct {
			*backend.TaskView
			Wait *waitResult `json:"wait,omitempty"`
		}{view, wait})
		return nil
	})
}
