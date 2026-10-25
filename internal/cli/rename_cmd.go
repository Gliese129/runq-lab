package cli

import (
	"fmt"
	"strings"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/spf13/cobra"
)

var renameCmd = &cobra.Command{
	Use:   "rename <jb…|pj…|project-name> <new-name>",
	Short: "Rename a job or a project (ids never change)",
	Long: `Rename a job or a project. Both are "immutable id + renamable name":
the id keeps naming directories and links, so a rename moves nothing.

A job reference is its id or a unique id prefix (jb…). Anything else is a
project: its id, a unique id prefix (pj…), or its current name.`,
	Example: `  runq rename jb7817 "A1 原始基线"
  runq rename pj3f2a kl-ablation
  runq rename tij-slime-guru-a1-tmp tij-guru-a1`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ref, newName := args[0], strings.TrimSpace(args[1])
		return withBackend(cmd, func(be backend.Backend) error {
			ctx := cmd.Context()
			if strings.HasPrefix(ref, "jb") {
				renamer, ok := be.(backend.JobRenamer)
				if !ok {
					return fmt.Errorf("this backend cannot rename jobs")
				}
				id, err := renamer.RenameJob(ctx, ref, newName)
				if err != nil {
					return err
				}
				fmt.Printf("job %s renamed to %q\n", id, newName)
				return nil
			}
			before, err := be.GetProject(ctx, ref)
			if err != nil {
				return err
			}
			if err := be.RenameProject(ctx, before.ProjectName, newName); err != nil {
				return err
			}
			fmt.Printf("project %s renamed %q → %q\n", before.ID, before.ProjectName, newName)
			return nil
		})
	},
}

func init() {
	renameCmd.GroupID = groupManagement
	rootCmd.AddCommand(renameCmd)
}
