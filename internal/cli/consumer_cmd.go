package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// consumerReplacement is what replaced runq-managed consumers: the script
// runs wherever you start it and reads the job itself.
const consumerReplacement = "run the scoring script yourself; it reads the job with `runq.job()` and writes results runq ingests (docs/views.md)"

func init() {
	command := &cobra.Command{Use: "consumer"}
	for _, name := range []string{"add", "ls", "show"} {
		command.AddCommand(&cobra.Command{Use: name})
	}
	rootCmd.AddCommand(command)
	retire(command, consumerReplacement)
}

// consumerRetired answers a cs… id given to show/logs/kill.
func consumerRetired(id string) error {
	if !strings.HasPrefix(id, "cs") {
		return nil
	}
	return fmt.Errorf("consumers (%s) were removed — %s", id, consumerReplacement)
}
