package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/spf13/cobra"
)

// ── runq kill (shortcut for task kill) ──

var killCmd = &cobra.Command{
	Use:   "kill <experiment | version>",
	Short: "Kill an experiment's running versions, or one version (tk…, ex…/<cell>[@vN])",
	Args:  cobra.ExactArgs(1),
	RunE:  runKill,
}

func runKill(cmd *cobra.Command, args []string) error {
	id := args[0]
	jsonOut, _ := cmd.Flags().GetBool("json")

	if err := consumerRetired(id); err != nil {
		return err
	}
	return withBackend(cmd, func(be backend.Backend) error {
		ctx := cmd.Context()
		if target := resolveTarget(cmd); target != "" {
			return killOnTarget(ctx, be, id, target, jsonOut)
		}
		// Short ids resolve as in ps/show; an ambiguous prefix is an answer.
		var fn func(context.Context, backend.Backend, string) (string, error)
		switch {
		case isVersionRef(id):
			fn = resolveTaskRef
		case isExperimentRef(id):
			fn = resolveJobRef
		}
		if fn != nil {
			full, err := fn(ctx, be, id)
			if err != nil && !errors.Is(err, backend.ErrNotFound) {
				return err
			}
			if err == nil {
				id = full
			}
		}

		// Prefix dispatch: try task, fall through to job — but ONLY on
		// not-found (D21: branch on the code, never on error presence).
		// Any other error is a real answer: a partial-cancel report
		// ("killed 2; could not cancel 2: ...") is the most valuable
		// output this command produces — swallowing it into "no task or
		// job found" was RQ-65 retest #3.
		taskErr := be.KillTask(ctx, id)
		if taskErr == nil {
			if jsonOut {
				printJSON(map[string]bool{"ok": true})
				return nil
			}
			fmt.Printf("version %s killed\n", id)
			return nil
		}
		if !errors.Is(taskErr, backend.ErrNotFound) {
			return taskErr
		}

		jobErr := be.KillJob(ctx, id)
		if jobErr == nil {
			if jsonOut {
				printJSON(map[string]bool{"ok": true})
				return nil
			}
			fmt.Printf("experiment %s killed\n", id)
			return nil
		}
		if !errors.Is(jobErr, backend.ErrNotFound) {
			return jobErr
		}

		return fmt.Errorf("no version or experiment found with id %q", id)
	})
}

// killOnTarget is `kill <ref> -t <target>`: the ref resolves as without
// -t, and the kill acts only on that target. A version on another target
// is refused; an experiment loses only its versions on the target (its
// submissions may run elsewhere) and is refused when it has none there.
func killOnTarget(ctx context.Context, be backend.Backend, id, target string, jsonOut bool) error {
	done := func(what string) error {
		if jsonOut {
			printJSON(map[string]bool{"ok": true})
			return nil
		}
		fmt.Println(what)
		return nil
	}
	if !isExperimentRef(id) {
		v, err := be.GetTask(ctx, id)
		switch {
		case err == nil:
			if v.Target != target {
				return fmt.Errorf("version %s runs on target %q, not %q: %w", v.ID, v.Target, target, backend.ErrNotOnTarget)
			}
			if err := be.KillTask(ctx, v.ID); err != nil {
				return err
			}
			return done(fmt.Sprintf("version %s killed", v.ID))
		case !errors.Is(err, backend.ErrNotFound):
			return err
		}
	}
	d, err := be.GetJob(ctx, id)
	if errors.Is(err, backend.ErrNotFound) {
		return fmt.Errorf("no version or experiment found with id %q", id)
	}
	if err != nil {
		return err
	}
	taskTargets := make([]string, len(d.Tasks))
	for i, t := range d.Tasks {
		taskTargets[i] = t.Target
	}
	on := backend.JobTargets(d.Job.Target, taskTargets)
	if !slices.Contains(on, target) {
		return backend.NotOnTargetError("experiment", d.Job.ID, target, on)
	}
	tk, ok := be.(backend.TargetKiller)
	if !ok {
		return fmt.Errorf("kill -t: %w", backend.ErrNotSupported)
	}
	if err := tk.KillJobOnTarget(ctx, d.Job.ID, target); err != nil {
		return err
	}
	if len(on) > 1 {
		return done(fmt.Sprintf("experiment %s killed on target %s (its versions on %s keep running)",
			d.Job.ID, target, strings.Join(slices.DeleteFunc(on, func(t string) bool { return t == target }), ", ")))
	}
	return done(fmt.Sprintf("experiment %s killed", d.Job.ID))
}

func init() {
	killCmd.Flags().Bool("json", false, "output raw JSON")
	killCmd.Flags().StringP("target", "t", "", "Only kill on this target: refuse a version elsewhere, kill only an experiment's versions here")

	killCmd.GroupID = groupCore
	rootCmd.AddCommand(killCmd)
}
