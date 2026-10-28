package cli

import (
	"fmt"
	"os"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export <jb…>",
	Short: "Write an experiment back as job.yaml (cells: rows, a snapshot)",
	Long: `Write a submitted job back as a job.yaml: every task as a named row under
tasks: (grid/list blocks are not reconstructed), params shared by all rows
under fixed_params. The file is a snapshot — editing it does not change the
job — and submitting it again reproduces the same rows, run names and params.
Use it to bring a plan edited elsewhere back into the repo.`,
	Example: `  runq export jb7817 -o configs/runq/a1-batch1.yaml
  runq export jb7817 | less`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, _ := cmd.Flags().GetString("output")
		return withBackend(cmd, func(be backend.Backend) error {
			ex, ok := be.(backend.JobExporter)
			if !ok {
				return fmt.Errorf("this backend cannot export experiments")
			}
			data, err := ex.ExportJob(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if out == "" || out == "-" {
				_, err = os.Stdout.Write(data)
				return err
			}
			if err := os.WriteFile(out, data, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s\n", out)
			return nil
		})
	},
}

func init() {
	exportCmd.Flags().StringP("output", "o", "", "write to this file instead of stdout")
	exportCmd.GroupID = groupManagement
	rootCmd.AddCommand(exportCmd)
}
