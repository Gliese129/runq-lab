package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Register and inspect projects (a code base and how it runs; one directory may hold several)",
}

var projectAddCmd = &cobra.Command{
	Use:   "add [name | .]",
	Short: "Register a new project",
	Long: `Register a new project from CLI flags or from a project.yaml file.

When "." is given as the argument, runq looks for project.yaml in the
current directory. The project_name field in YAML is used unless a name
is also provided on the command line (CLI takes priority).`,
	Example: `  # From a YAML file in current directory
  runq project add .

  # From a YAML file with CLI name override
  runq project add myproject --file ./project.yaml

  # Inline (minimal)
  runq project add myproject --dir . --cmd "python train.py {{args}}" -t local`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := project.Config{}
		var yamlPath string
		var projectName string

		if len(args) > 0 {
			if args[0] == "." {
				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				yamlPath = filepath.Join(wd, "project.yaml")
			} else {
				projectName = args[0]
			}
		}

		// --file flag overrides "." resolution (but not explicit project name)
		if file, _ := cmd.Flags().GetString("file"); file != "" && yamlPath == "" {
			yamlPath = file
		}

		if yamlPath != "" {
			buf, err := os.ReadFile(yamlPath)
			if err != nil {
				return err
			}
			if err := yaml.Unmarshal(buf, &cfg); err != nil {
				return err
			}
		}

		// CLI flags override YAML fields
		if projectName != "" {
			cfg.ProjectName = projectName
		}
		if dir, _ := cmd.Flags().GetString("dir"); dir != "" {
			cfg.WorkingDir = dir
		}
		if cmdTpl, _ := cmd.Flags().GetString("cmd"); cmdTpl != "" {
			cfg.CmdTemplate = cmdTpl
		}

		if cmd.Flags().Changed("target") {
			cfg.Target, _ = cmd.Flags().GetString("target")
		}
		return withBackend(cmd, func(be backend.Backend) error {
			if err := be.CreateProject(cmd.Context(), cfg); err != nil {
				return err
			}
			fmt.Printf("project %q registered\n", cfg.ProjectName)
			return nil
		})
	},
}

var projectLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List all registered projects",
	RunE:    runProjectLs,
}

func runProjectLs(cmd *cobra.Command, args []string) error {
	return withBackend(cmd, func(be backend.Backend) error {
		ctx := cmd.Context()
		summaries, err := be.ListProjects(ctx)
		if err != nil {
			return err
		}
		if len(summaries) == 0 {
			fmt.Println("no projects registered")
			return nil
		}

		w := newTable()
		fmt.Fprintf(w, "NAME\tDIR\tGPUs/TASK\tJOBS\n")
		for _, s := range summaries {
			// Fetch full config for display fields not in ProjectSummary.
			gpus := 1
			if cfg, err := be.GetProject(ctx, s.Name); err == nil {
				if cfg.Defaults.GPUsPerTask > 0 {
					gpus = cfg.Defaults.GPUsPerTask
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%d\t%d\n", s.Name, s.WorkDir, gpus, s.JobCount)
		}
		w.Flush()
		return nil
	})
}

var projectShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show project details",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectShow,
}

func runProjectShow(cmd *cobra.Command, args []string) error {
	return withBackend(cmd, func(be backend.Backend) error {
		cfg, err := be.GetProject(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		printJSON(cfg)
		return nil
	})
}

func init() {
	// Flags for project add
	projectAddCmd.Flags().StringP("file", "f", "", "Path to project.yaml")
	projectAddCmd.Flags().String("dir", "", "Working directory")
	projectAddCmd.Flags().StringP("target", "t", "", "Project source/default execution target (registration only)")
	projectAddCmd.Flags().String("cmd", "", "Default command (a job.yaml `command:` overrides it)")

	projectCmd.AddCommand(projectAddCmd)
	projectCmd.AddCommand(projectLsCmd)
	projectCmd.AddCommand(projectShowCmd)
	projectCmd.AddCommand(projectArchiveCmd)
	projectCmd.AddCommand(projectUnarchiveCmd)
	projectCmd.GroupID = groupManagement
	rootCmd.AddCommand(projectCmd)
}

// ── archive / unarchive (mode-aware) ──

var projectArchiveCmd = &cobra.Command{
	Use:   "archive <name>",
	Short: "Hide a project (and its jobs in global lists) from default views; reversible",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runProjectArchive(cmd, args[0], true) },
}

var projectUnarchiveCmd = &cobra.Command{
	Use:   "unarchive <name>",
	Short: "Bring an archived project back",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runProjectArchive(cmd, args[0], false) },
}

func runProjectArchive(cmd *cobra.Command, name string, archive bool) error {
	verb := "archived"
	if !archive {
		verb = "unarchived"
	}
	return withBackend(cmd, func(be backend.Backend) error {
		var err error
		if archive {
			err = be.ArchiveProject(cmd.Context(), name)
		} else {
			err = be.UnarchiveProject(cmd.Context(), name)
		}
		if err != nil {
			return err
		}
		fmt.Printf("project %q %s\n", name, verb)
		return nil
	})
}
