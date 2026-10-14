package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/consumer"
	"github.com/gliese129/runq-lab/internal/logfile"
	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/spf13/cobra"
)

func init() {
	command := &cobra.Command{Use: "consumer", Short: "Prepare and inspect manually started job consumers", GroupID: groupManagement}
	command.AddCommand(&cobra.Command{Use: "add <job_id> <name> -- <command...>", Short: "Prepare a consumer; start its run.sh in your own session", Args: cobra.MinimumNArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		return withBackend(cmd, func(be backend.Backend) error {
			parts := make([]string, len(args)-2)
			for i, s := range args[2:] {
				parts[i] = utils.ShellQuote(s)
			}
			c, err := be.(backend.Consumers).AddConsumer(cmd.Context(), args[0], consumer.Config{Name: args[1], Command: strings.Join(parts, " ")})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s: bash %s\n", c.ID, c.Name, utils.ShellQuote(c.RunSH))
			return nil
		})
	}})
	command.AddCommand(&cobra.Command{Use: "ls <job_id>", Short: "List a job's consumers", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withBackend(cmd, func(be backend.Backend) error {
			list, err := be.(backend.Consumers).ListConsumers(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			printJSON(list)
			return nil
		})
	}})
	command.AddCommand(&cobra.Command{Use: "show <consumer_id>", Short: "Show consumer state and launch command", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withBackend(cmd, func(be backend.Backend) error {
			c, err := be.(backend.Consumers).GetConsumer(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			printJSON(c)
			return nil
		})
	}})
	rootCmd.AddCommand(command)
}
func consumerLogs(cmd *cobra.Command, be backend.Backend, id string, noFollow bool) error {
	b := be.(backend.Consumers)
	req := logfile.PageRequest{Tail: true, MaxBytes: 8192}
	for {
		page, err := b.ConsumerLog(cmd.Context(), id, req)
		if err != nil {
			return err
		}
		for _, line := range page.Lines {
			fmt.Fprintln(cmd.OutOrStdout(), line)
		}
		if noFollow {
			return nil
		}
		req.Tail = false
		req.Offset = page.NextOffset
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-time.After(time.Second):
		}
	}
}
