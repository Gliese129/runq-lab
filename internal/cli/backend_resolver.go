package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gliese129/runq-lab/internal/api"
	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/spf13/cobra"
)

// withBackend opens a Proxy connected to the running daemon's Unix socket,
// applies the command's -t/--target as the query scope, and runs fn. This is the single entry point for ALL CLI commands that need
// a Backend — every command automatically gets target resolution.
//
// The resolved target is applied as Proxy.TargetFilter, which:
//   - scopes list operations (ListJobs, ListArchivedJobs, Clean)
//
// Explicit submission/preview targets are separate from query filters.
func withBackend(cmd *cobra.Command, fn func(backend.Backend) error) error {
	be := api.NewProxy(getSocketPath())
	be.TargetFilter = resolveTarget(cmd)
	return fn(be)
}

// applyFresh implements the global --fresh flag (spec §7, D22): before the
// command's read, force a TTL-bypassing refresh — job-scoped when a jobID
// is at hand, target-scoped otherwise. The receipt is surfaced honestly:
// when the server's 5min floor blocked the refresh, say so instead of
// silently reading stale data. Refresh failures never block the command
// (the read below will surface real connectivity problems).
func applyFresh(cmd *cobra.Command, be backend.Backend, jobID string) {
	if fresh, _ := cmd.Root().PersistentFlags().GetBool("fresh"); !fresh {
		return
	}
	p, ok := be.(*api.Proxy)
	if !ok {
		return
	}
	var receipt *api.RefreshReceipt
	var err error
	switch {
	case jobID != "":
		receipt, err = p.RefreshJobReceipt(cmd.Context(), jobID)
	case p.TargetFilter != "":
		receipt, err = p.RefreshTarget(cmd.Context(), p.TargetFilter)
	default:
		fmt.Fprintln(os.Stderr, "--fresh: no target resolved (set --target or a default); reading cache")
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "--fresh: refresh failed (%v); reading cache\n", err)
		return
	}
	if !receipt.Refreshed {
		ago := time.Now().Unix() - receipt.RefreshedAt
		fmt.Fprintf(os.Stderr, "--fresh: refreshed %ds ago, not re-pulled (%s)\n", ago, receipt.Reason)
	}
}

// resolveTarget is the target the command line names with -t/--target;
// "" means none (list = all targets, submit = job/project target). rc6:
// query scope is never implicit — RUNQ_TARGET and `target use` are gone.
func resolveTarget(cmd *cobra.Command) string {
	if f := cmd.Flags().Lookup("target"); f != nil && cmd.Flags().Changed("target") {
		t, _ := cmd.Flags().GetString("target")
		return t
	}
	return ""
}

// resolveJobRef and resolveTaskRef are the CLI's one way to read an id the
// user typed: the service resolves a full id or a unique prefix (and says
// which ids an ambiguous prefix matches), so every command accepts the same
// short ids that ps and show print.
func resolveJobRef(ctx context.Context, be backend.Backend, ref string) (string, error) {
	d, err := be.GetJob(ctx, ref)
	if err != nil {
		return "", err
	}
	return d.Job.ID, nil
}

func resolveTaskRef(ctx context.Context, be backend.Backend, ref string) (string, error) {
	v, err := be.GetTask(ctx, ref)
	if err != nil {
		return "", err
	}
	return v.ID, nil
}
