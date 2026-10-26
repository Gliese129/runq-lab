package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

// The submit target is only what -t says: a query environment never picks it.
func TestSubmitSeparatesExplicitTargetFromQueryEnvironment(t *testing.T) {
	t.Setenv("RUNQ_TARGET", "query-only")
	cmd := &cobra.Command{Use: "submit"}
	cmd.Flags().StringP("target", "t", "", "")
	if got := explicitSubmitTarget(cmd); got != "" {
		t.Fatalf("submit target %q without -t, want none", got)
	}
	if err := cmd.Flags().Set("target", "explicit"); err != nil {
		t.Fatal(err)
	}
	if got := explicitSubmitTarget(cmd); got != "explicit" {
		t.Fatalf("submit target %q, want explicit", got)
	}
}
