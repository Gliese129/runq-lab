package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

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
