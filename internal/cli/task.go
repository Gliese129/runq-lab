package cli

import (
	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/dashboard"
	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Inspect a single task",
}

var taskShowCmd = &cobra.Command{
	Use:   "show <task_id>",
	Short: "Show task details (command, GPU, status, etc.)",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskShow,
}

func runTaskShow(cmd *cobra.Command, args []string) error {
	return withBackend(cmd, func(b backend.Backend) error {
		view, err := b.GetTask(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		view.URL = dashboard.TaskURL(dashboardBase(cmd.Context(), b), view.Project, view.JobID, view.ID)
		printJSON(view)
		return nil
	})
}

func init() {
	// Output is always JSON; --json is accepted for parity with ps/status/kill
	// so scripted callers can pass it uniformly.
	taskShowCmd.Flags().Bool("json", true, "output raw JSON (always on; accepted for parity)")
	taskCmd.AddCommand(taskShowCmd)
	rootCmd.AddCommand(taskCmd)
	retire(taskCmd, "use `runq show <tk…>`")
}
