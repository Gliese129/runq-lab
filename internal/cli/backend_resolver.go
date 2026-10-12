package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gliese129/runq-lab/internal/api"
	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/config"
	"github.com/spf13/cobra"
)

// withBackend opens a Proxy connected to the running daemon's Unix socket,
// resolves the active target (--target / $RUNQ_TARGET / .active-target),
// and runs fn. This is the single entry point for ALL CLI commands that need
// a Backend — every command automatically gets target resolution.
//
// The resolved target is applied as Proxy.TargetFilter, which:
//   - scopes list operations (ListJobs, ListArchivedJobs, Clean)
//
// Explicit submission/preview targets are separate from query filters.
func withBackend(cmd *cobra.Command, fn func(backend.Backend) error) error {
	be := api.NewProxy(getSocketPath())
	be.TargetFilter = resolveTarget(cmd)
	if cmd.Name() == "submit" {
		be.SubmitTarget = explicitSubmitTarget(cmd)
	}
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

// resolveTarget reads the active target from the CLI --target flag, the
// RUNQ_TARGET env var, or the .active-target session file.
// Returns "" if no target is explicitly set (list = all targets,
// submit = daemon's default_target).
//
// Priority: --target flag > $RUNQ_TARGET > <configDir>/.active-target
// implicitTargetScope reports a query scope that the caller did NOT pass on
// this command line (RUNQ_TARGET or a leftover `runq target use`). An agent
// that reads "No jobs" without knowing the list was filtered draws the wrong
// conclusion, so listings name the scope on stderr.
func implicitTargetScope(cmd *cobra.Command) (target, source string) {
	if cmd.Flags().Lookup("target") != nil && cmd.Flags().Changed("target") {
		return "", ""
	}
	if t := os.Getenv("RUNQ_TARGET"); t != "" {
		return t, "RUNQ_TARGET"
	}
	path := filepath.Join(config.ConfigDir(), ".active-target")
	if buf, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(buf)); t != "" {
			return t, "`runq target use` (" + path + ")"
		}
	}
	return "", ""
}

func resolveTarget(cmd *cobra.Command) string {
	if cmd.Flags().Changed("target") {
		t, _ := cmd.Flags().GetString("target")
		return t
	}
	if t := os.Getenv("RUNQ_TARGET"); t != "" {
		return t
	}
	path := filepath.Join(config.ConfigDir(), ".active-target")
	if buf, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(buf)); t != "" {
			return t
		}
	}
	return ""
}
