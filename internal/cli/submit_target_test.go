package cli

import (
	"github.com/gliese129/runq-lab/internal/api"
	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/spf13/cobra"
	"testing"
)

func TestSubmitSeparatesExplicitTargetFromQueryEnvironment(t *testing.T) {
	t.Setenv("RUNQ_TARGET", "query-only")
	t.Setenv("RUNQ_DATA_DIR", t.TempDir())
	cmd := &cobra.Command{Use: "submit"}
	cmd.Flags().StringP("target", "t", "", "")
	check := func(want string) {
		t.Helper()
		if err := withBackend(cmd, func(be backend.Backend) error {
			p := be.(*api.Proxy)
			if p.SubmitTarget != want {
				t.Fatalf("preview target %q, want %q", p.SubmitTarget, want)
			}
			if explicitSubmitTarget(cmd) != want {
				t.Fatal("submit and preview disagree")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	check("")
	if err := cmd.Flags().Set("target", "explicit"); err != nil {
		t.Fatal(err)
	}
	check("explicit")
}
