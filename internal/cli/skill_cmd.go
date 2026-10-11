package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gliese129/runq-lab/internal/skill"
	"github.com/spf13/cobra"
)

// ── runq skill ── the agent guide (SKILL.md + references) is embedded in the
// binary; `install` copies the version that matches THIS runq into the skill
// roots agents read. Re-run after every upgrade.

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Install or print the built-in agent skill (Codex / Claude Code)",
}

var skillInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the runq skill for Codex and Claude Code (re-run after upgrading runq)",
	Example: `  runq skill install                 # ~/.codex/skills/runq and ~/.claude/skills/runq
  runq skill install --dir .agents/skills   # a project-local skills root`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("dir")
		var roots []string
		if dir != "" {
			roots = []string{dir}
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			roots = []string{
				filepath.Join(home, ".codex", "skills"),
				filepath.Join(home, ".claude", "skills"),
			}
		}
		for _, root := range roots {
			dest, err := skill.Install(root)
			if err != nil {
				return fmt.Errorf("install into %s: %w", root, err)
			}
			fmt.Printf("installed %s\n", dest)
		}
		return nil
	},
}

var skillShowCmd = &cobra.Command{
	Use:   "show [file]",
	Short: "Print SKILL.md (or a reference, e.g. references/hpc.md)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := "SKILL.md"
		if len(args) == 1 {
			name = args[0]
		}
		data, err := fs.ReadFile(skill.FS(), name)
		if err != nil {
			return fmt.Errorf("%s: not in the built-in skill (try SKILL.md, references/hpc.md, references/sdk.md, references/troubleshooting.md)", name)
		}
		_, err = os.Stdout.Write(data)
		return err
	},
}

func init() {
	skillInstallCmd.Flags().String("dir", "", "install into <dir>/runq instead of the Codex and Claude Code roots")
	skillCmd.AddCommand(skillInstallCmd, skillShowCmd)
	skillCmd.GroupID = groupManagement
	rootCmd.AddCommand(skillCmd)
}
