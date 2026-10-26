package cli

import (
	"fmt"
	"strings"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ── Target management subcommands ──────────────────────────────────────────

var configAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a new compute target to config.yaml",
	Long: `Add a new compute target. Template presets fill in scheduler templates
and signal maps for common HPC schedulers.

Examples:
  runq target add tsubame --template=tsubame --host=login.t4.gsic.titech.ac.jp --user=alice
  runq target add local-a100 --gpus=0,1,2,3
  runq target add abci --template=abci --host=es.abci.local`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigAdd,
}

var configShowCmd = &cobra.Command{
	Use:     "show [<name>]",
	Aliases: []string{"ls"},
	Short:   "Show target configuration (all targets or a specific one)",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runConfigShow,
}

var configCheckCmd = &cobra.Command{
	Use:   "check [<name>]",
	Short: "Validate target templates: placeholders, regex, sample render",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runConfigCheck,
}

func runConfigAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	template, _ := cmd.Flags().GetString("template")
	host, _ := cmd.Flags().GetString("host")
	user, _ := cmd.Flags().GetString("user")
	gpus, _ := cmd.Flags().GetString("gpus")

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Check for duplicate.
	for _, t := range cfg.Targets {
		if t.Name == name {
			return fmt.Errorf("target %q already exists — edit it in ~/.runq/config.yaml, then `runq target check %s`", name, name)
		}
	}

	tc := config.TargetConfig{Name: name}

	// Apply template preset if given.
	if template != "" {
		tc.Scheduler = template
		if preset, err := config.HPCPreset(template); err == nil {
			tc.SubmitTemplate = preset.SubmitTemplate
			tc.SubmitIDRegex = preset.SubmitIDRegex
			tc.StatusTemplate = preset.StatusTemplate
			tc.StatusParser = preset.StatusParser
			tc.StatusListTemplate = preset.StatusListTemplate
			tc.StatusListParser = preset.StatusListParser
			tc.SignalMap = preset.SignalMap
			tc.KillTemplate = preset.KillTemplate
		}
	}

	// SSH config.
	if host != "" {
		tc.SSH = &config.SSHTargetConfig{Host: host, User: user}
	}

	// GPU list for local targets.
	if gpus != "" {
		var gpuList []int
		for _, s := range strings.Split(gpus, ",") {
			s = strings.TrimSpace(s)
			var idx int
			if _, err := fmt.Sscanf(s, "%d", &idx); err != nil {
				return fmt.Errorf("invalid GPU index %q", s)
			}
			gpuList = append(gpuList, idx)
		}
		tc.GPUs = gpuList
	}

	firstTarget := len(cfg.Targets) == 0
	if firstTarget {
		// One atomic write transitions out of the unconfigured state and also
		// repairs any stale default_target left by a hand-edited config.
		cfg.DefaultTarget = name
	}
	cfg.Targets = append(cfg.Targets, tc)
	if err := config.SaveGlobal(cfg); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "added target %q\n", name)
	if firstTarget {
		fmt.Fprintln(cmd.OutOrStdout(), "set as default_target")
	}
	return nil
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if len(args) == 1 {
		// Show specific target.
		tc, err := cfg.FindTarget(args[0])
		if err != nil {
			return err
		}
		out, err := yaml.Marshal(tc)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "# target: %s\n%s", tc.Name, out)
		return nil
	}

	// Show all targets.
	targets := cfg.ResolveTargets()
	if len(targets) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no targets configured")
		return nil
	}
	defaultTarget := cfg.ResolveDefaultTarget()
	for _, t := range targets {
		marker := " "
		if t.Name == defaultTarget {
			marker = "*"
		}
		typeName := t.Type()
		remote := ""
		if t.IsRemote() {
			remote = fmt.Sprintf(" (%s@%s)", t.SSH.User, t.SSH.Host)
		}
		fmt.Fprintf(cmd.OutOrStdout(), " %s %-16s  %s%s\n", marker, t.Name, typeName, remote)
	}
	return nil
}

func runConfigCheck(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	targets := cfg.ResolveTargets()
	if len(args) == 1 {
		// Check specific target only.
		tc, err := cfg.FindTarget(args[0])
		if err != nil {
			return err
		}
		targets = []config.TargetConfig{*tc}
	}

	failed := 0
	for _, tc := range targets {
		if tc.Scheduler == "" {
			continue // local targets have nothing to check
		}
		fmt.Fprintf(cmd.OutOrStdout(), "── %s ──\n", tc.Name)
		for _, r := range tc.CheckHPC() {
			var mark string
			switch r.Status {
			case "ok":
				mark = "✓"
			case "fail":
				mark = "✗"
				failed++
			case "warn":
				mark = "!"
			default:
				mark = "-"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %-18s %s\n", mark, r.Name, r.Detail)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d template check(s) failed", failed)
	}
	return nil
}

func init() {
	configAddCmd.Flags().String("template", "", "HPC scheduler preset: slurm | pbs | sge | tsubame | abci")
	configAddCmd.Flags().String("host", "", "SSH host for remote targets")
	configAddCmd.Flags().String("user", "", "SSH user for remote targets")
	configAddCmd.Flags().String("gpus", "", "Comma-separated GPU indices for local targets (e.g. 0,1,2,3)")
}
