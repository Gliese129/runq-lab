package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// consumerReplacement is what replaced runq-managed consumers: the script
// runs wherever you start it and reads the job itself.
const consumerReplacement = "run the scoring script yourself; it reads the experiment with `runq.experiment()` and writes results runq ingests (docs/views.md)"

// Removed commands, each a hidden stub that fails with what replaced it.
// A stub swallows its old subcommands and flags (`runq job archive jb1`).
func init() {
	for _, r := range []struct {
		parent      *cobra.Command
		use         string
		replacement string
	}{
		{rootCmd, "task", "use `runq show <tk…>`"},
		{rootCmd, "job", "use `runq archive <ex…>` (`--restore` to undo); experiments are listed by `runq ps`"},
		{rootCmd, "results", "use `runq view --experiment <ex…>`"},
		{rootCmd, "consumer", consumerReplacement},
		{rootCmd, "config", "edit ~/.runq/config.yaml directly (`runq doctor` prints its path, `runq target check` validates it)"},
		{rootCmd, "gpu", "GPU allocation is shown on the dashboard's Targets page (`runq dashboard`)"},
		{rootCmd, "open", "use `runq dashboard`; experiment, version and draft pages are linked from `runq show`, `runq ps` and `runq draft`"},
		{rootCmd, "version", "use `runq --version`"},
		{rootCmd, "connect", "use `runq target connect`"},
		{projectCmd, "add", "use `runq project <project.yaml>` (registers or updates)"},
		{projectCmd, "show", "use `runq show <pj… | project-name>`"},
		{projectCmd, "archive", "use `runq archive <pj… | project-name>`"},
		{projectCmd, "unarchive", "use `runq archive <pj… | project-name> --restore`"},
		{targetCmd, "use", "pass `-t <target>` to the command that needs it; the default target is default_target in ~/.runq/config.yaml"},
		{draftCmd, "show", "read a draft with `runq show dr… [--yaml]`"},
	} {
		stub := &cobra.Command{Use: r.use}
		r.parent.AddCommand(stub)
		retire(stub, r.replacement)
	}
}

// consumerRetired answers a cs… id given to show/logs/kill.
func consumerRetired(id string) error {
	if !strings.HasPrefix(id, "cs") {
		return nil
	}
	return fmt.Errorf("consumers (%s) were removed — %s", id, consumerReplacement)
}

// retire turns a removed command into a hidden stub: calling it fails with
// the command that replaced it, so old habits (and old agent prompts) get a
// one-line fix instead of "unknown command". Hidden, so help, docs and the
// skill never teach it.
func retire(cmd *cobra.Command, replacement string) {
	cmd.Hidden = true
	cmd.DisableFlagParsing = true
	cmd.Args = cobra.ArbitraryArgs
	cmd.Run = nil
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		return fmt.Errorf("`%s` was removed — %s", c.CommandPath(), replacement)
	}
	for _, sub := range cmd.Commands() {
		if !sub.Hidden {
			retire(sub, replacement)
		}
	}
}

// retireFlag keeps a removed flag parseable but hidden; checkRetiredFlags
// turns its use into the same kind of one-line fix.
func retireFlag(cmd *cobra.Command, name, replacement string) {
	f := cmd.Flags().Lookup(name)
	if f == nil {
		return
	}
	f.Hidden = true
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations["retired:"+name] = replacement
}

func checkRetiredFlags(cmd *cobra.Command) error {
	for key, replacement := range cmd.Annotations {
		name, ok := cutPrefix(key, "retired:")
		if ok && cmd.Flags().Changed(name) {
			return fmt.Errorf("`%s --%s` was removed in rc6 — %s", cmd.CommandPath(), name, replacement)
		}
	}
	return nil
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return s, false
}
