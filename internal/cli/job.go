package cli

import (
	"fmt"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/spf13/cobra"
)

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Archive or unarchive jobs (list them with `runq ps`)",
}

func init() {
	jobCmd.AddCommand(jobArchiveCmd)
	jobCmd.AddCommand(jobUnarchiveCmd)
	jobCmd.GroupID = groupManagement
	rootCmd.AddCommand(jobCmd)
}

// ── archive / unarchive (mode-aware: daemon API or HPC store) ──

var jobArchiveCmd = &cobra.Command{
	Use:   "archive <job_id>",
	Short: "Hide a job from default lists (data and workspace untouched; reversible)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runJobArchive(cmd, args[0], true)
	},
}

var jobUnarchiveCmd = &cobra.Command{
	Use:   "unarchive <job_id>",
	Short: "Bring an archived job back to the default lists",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runJobArchive(cmd, args[0], false)
	},
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
		fmt.Printf("job %s %s\n", utils.IDColor(jobID), verb)
		return nil
	})
}
