package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var projectCmd = &cobra.Command{
	Use:   "project [project.yaml] | project ls",
	Short: "Register or update a project from its file (a code base, one Python env)",
	Long: `Register or update a project from project.yaml (default: ./project.yaml).
The file's project_name decides: a new name registers a project (and prints
its pj… id), a known one updates it and lists what changed. runq never reads
or writes project.yaml on its own — run this again after editing the file;
'runq draft' warns when the registered file has changed since.

One project = one code base with one Python environment; for a second
environment (say training vs evaluation), write a second project file.`,
	Example: `  runq project                    # ./project.yaml
  runq project eval.project.yaml
  runq project ls`,
	Args: cobra.MaximumNArgs(1),
	RunE: runProjectUpsert,
}

func runProjectUpsert(cmd *cobra.Command, args []string) error {
	path := "project.yaml"
	if len(args) == 1 && args[0] != "." {
		path = args[0]
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg project.Config
	if err := yaml.Unmarshal(buf, &cfg); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.ProjectName == "" {
		return fmt.Errorf("%s: project_name is required", path)
	}
	warns, err := project.NormalizeLegacy(&cfg)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for _, w := range warns {
		fmt.Fprintf(os.Stderr, "%s: %s\n", filepath.Base(path), w)
	}
	abs, _ := filepath.Abs(path)
	sum := sha256.Sum256(buf)
	cfg.SourcePath, cfg.SourceSHA = abs, hex.EncodeToString(sum[:])
	return withBackend(cmd, func(be backend.Backend) error {
		ctx := cmd.Context()
		existing, err := be.GetProject(ctx, cfg.ProjectName)
		if err != nil {
			// Renamed with `runq rename`? Then this file still carries the old
			// name — registering it would split the project in two.
			if list, lerr := be.ListProjects(ctx); lerr == nil {
				for _, p := range list {
					if other, gerr := be.GetProject(ctx, p.Name); gerr == nil && other.SourcePath == abs {
						return fmt.Errorf("%s is registered as %s %q (renamed?): set project_name: %s in the file, or give a new project its own file", path, other.ID, other.ProjectName, other.ProjectName)
					}
				}
			}
			if err := be.CreateProject(ctx, cfg); err != nil {
				return err
			}
			id := ""
			if got, gerr := be.GetProject(ctx, cfg.ProjectName); gerr == nil {
				id = got.ID + " "
			}
			fmt.Printf("registered project %s%q\n", id, cfg.ProjectName)
			return nil
		}
		cfg.ID = existing.ID
		changed := changedProjectKeys(existing, &cfg)
		if len(changed) == 0 {
			fmt.Printf("project %s %q unchanged\n", existing.ID, cfg.ProjectName)
			if existing.SourceSHA == cfg.SourceSHA {
				return nil
			}
		}
		if err := be.UpdateProject(ctx, cfg); err != nil {
			return err
		}
		if len(changed) > 0 {
			fmt.Printf("updated project %s %q: %s\n", existing.ID, cfg.ProjectName, strings.Join(changed, ", "))
		}
		return nil
	})
}

// changedProjectKeys lists the top-level project.yaml keys whose values
// differ between two registrations (bookkeeping fields aside).
func changedProjectKeys(a, b *project.Config) []string {
	toMap := func(c *project.Config) map[string]any {
		cp := *c
		cp.ID, cp.SourcePath, cp.SourceSHA = "", "", ""
		raw, _ := yaml.Marshal(cp)
		m := map[string]any{}
		_ = yaml.Unmarshal(raw, &m)
		return m
	}
	am, bm := toMap(a), toMap(b)
	keys := map[string]bool{}
	for k := range am {
		keys[k] = true
	}
	for k := range bm {
		keys[k] = true
	}
	var out []string
	for k := range keys {
		x, _ := yaml.Marshal(am[k])
		y, _ := yaml.Marshal(bm[k])
		if string(x) != string(y) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
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
			id := ""
			if got, err := be.GetProject(cmd.Context(), cfg.ProjectName); err == nil {
				id = got.ID + " "
			}
			fmt.Printf("project %s%q registered\n", id, cfg.ProjectName)
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
		fmt.Fprintf(w, "ID\tNAME\tDIR\tGPUs/TASK\tJOBS\n")
		for _, s := range summaries {
			// Fetch full config for display fields not in ProjectSummary.
			gpus := 1
			if cfg, err := be.GetProject(ctx, s.Name); err == nil {
				if cfg.Defaults.GPUsPerTask > 0 {
					gpus = cfg.Defaults.GPUsPerTask
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\n", s.ID, s.Name, s.WorkDir, gpus, s.JobCount)
		}
		w.Flush()
		return nil
	})
}

var projectShowCmd = &cobra.Command{
	Use:   "show <id|name>",
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
	retire(projectAddCmd, "use `runq project <project.yaml>` (registers or updates)")
	retire(projectShowCmd, "use `runq show <pj… | project-name>`")
	retire(projectArchiveCmd, "use `runq archive <pj… | project-name>`")
	retire(projectUnarchiveCmd, "use `runq archive <pj… | project-name> --restore`")
}

// ── archive / unarchive (mode-aware) ──

var projectArchiveCmd = &cobra.Command{
	Use:   "archive <id|name>",
	Short: "Hide a project (and its jobs in global lists) from default views; reversible",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runProjectArchive(cmd, args[0], true) },
}

var projectUnarchiveCmd = &cobra.Command{
	Use:   "unarchive <id|name>",
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
