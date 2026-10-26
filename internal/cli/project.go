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

func init() {
	projectCmd.AddCommand(projectLsCmd)
	projectCmd.GroupID = groupManagement
	rootCmd.AddCommand(projectCmd)
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
