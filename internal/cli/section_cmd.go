package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/spf13/cobra"
)

var sectionCmd = &cobra.Command{
	Use:   "section",
	Short: "Organize a project's experiments into sections (studies, smoke)",
	Long: `Sections organize a project's jobs; they carry no execution config.

Every project has "unsorted" (experiments no rule matched) and "smoke" (with a
default rule: name *smoke*). A new job lands in the section named by
'submit -s' or job.yaml 'section:' (it must exist), else in the first
section whose rule matches, else in unsorted. Moving a job changes nothing
on disk.`,
}

func withSectioner(cmd *cobra.Command, fn func(backend.Sectioner) error) error {
	return withBackend(cmd, func(be backend.Backend) error {
		sec, ok := be.(backend.Sectioner)
		if !ok {
			return fmt.Errorf("this backend does not support sections")
		}
		return fn(sec)
	})
}

var sectionLsCmd = &cobra.Command{
	Use:     "ls <project>",
	Aliases: []string{"list"},
	Short:   "List a project's sections with experiment counts and rules",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		return withSectioner(cmd, func(sec backend.Sectioner) error {
			secs, err := sec.ListSections(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if jsonOut {
				printJSON(secs)
				return nil
			}
			w := newTable()
			fmt.Fprintf(w, "SECTION\tJOBS\tRULES\n")
			for _, s := range secs {
				var rules []string
				for _, r := range s.Rules {
					rules = append(rules, fmt.Sprintf("#%d %s %s", r.ID, r.Kind, r.Pattern))
				}
				if len(rules) == 0 {
					rules = []string{"-"}
				}
				fmt.Fprintf(w, "%s\t%d\t%s\n", s.Name, s.Jobs, strings.Join(rules, ", "))
			}
			return w.Flush()
		})
	},
}

var sectionCreateCmd = &cobra.Command{
	Use:     "create <project> <name>",
	Short:   "Create a section",
	Example: `  runq section create kl "KL 消融"`,
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withSectioner(cmd, func(sec backend.Sectioner) error {
			s, err := sec.CreateSection(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Printf("section %q created (%s)\n", s.Name, s.ID)
			return nil
		})
	},
}

var sectionRmCmd = &cobra.Command{
	Use:   "rm <project> <name>",
	Short: "Remove a section; its experiments become unsorted",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withSectioner(cmd, func(sec backend.Sectioner) error {
			n, err := sec.RemoveSection(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Printf("section %q removed; %d experiment(s) now unsorted\n", args[1], n)
			return nil
		})
	},
}

var sectionMoveCmd = &cobra.Command{
	Use:   "move <section> <ex…> [ex…] | move <section> --view <name>",
	Short: "File experiments or a saved view under a section of their project (\"unsorted\" clears it)",
	Example: `  runq section move smoke ex7817 exf051
  runq section move ablation --view a1-suite`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		view, _ := cmd.Flags().GetString("view")
		if view != "" {
			if len(args) != 1 {
				return fmt.Errorf("with --view, give only the section: runq section move <section> --view <name>")
			}
			project, _ := cmd.Flags().GetString("project")
			return withSectioner(cmd, func(sec backend.Sectioner) error {
				p, err := sec.MoveView(cmd.Context(), project, view, args[0])
				if err != nil {
					return err
				}
				fmt.Printf("view %s/%s → %s\n", p, view, args[0])
				return nil
			})
		}
		if len(args) < 2 {
			return fmt.Errorf("name the experiments to move, or a view with --view")
		}
		return withSectioner(cmd, func(sec backend.Sectioner) error {
			for _, ref := range args[1:] {
				id, err := sec.MoveJob(cmd.Context(), ref, args[0])
				if err != nil {
					return err
				}
				fmt.Printf("experiment %s → %s\n", id, args[0])
			}
			return nil
		})
	},
}

var sectionSortCmd = &cobra.Command{
	Use:   "sort <project>",
	Short: "Apply the project's rules to its unsorted experiments (by name)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withSectioner(cmd, func(sec backend.Sectioner) error {
			n, err := sec.SortSections(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("%d unsorted experiment(s) filed\n", n)
			return nil
		})
	},
}

var sectionRuleCmd = &cobra.Command{
	Use:   "rule",
	Short: "Rules that file new experiments into sections (first match wins)",
}

var sectionRuleAddCmd = &cobra.Command{
	Use:   "add <project> <section> (--name GLOB | --path GLOB)",
	Short: "File new experiments whose name or job-file path matches into a section",
	Long: `Add a rule. Rules apply in order to new jobs without an explicit
section; the first match wins. Globs: * and ? stay within one path segment,
** crosses '/'; matching ignores case. A --path pattern without '/' matches
the job file's base name; with '/', the path as passed to 'runq submit'.`,
	Example: `  runq section rule add kl "KL 消融" --name 'kl*'
  runq section rule add kl main --path 'exp/main/**'`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		pth, _ := cmd.Flags().GetString("path")
		if (name == "") == (pth == "") {
			return fmt.Errorf("give exactly one of --name or --path")
		}
		kind, pattern := "name", name
		if pth != "" {
			kind, pattern = "path", pth
		}
		return withSectioner(cmd, func(sec backend.Sectioner) error {
			r, err := sec.AddSectionRule(cmd.Context(), args[0], args[1], kind, pattern)
			if err != nil {
				return err
			}
			fmt.Printf("rule #%d: %s %s → %s\n", r.ID, r.Kind, r.Pattern, args[1])
			return nil
		})
	},
}

var sectionRuleRmCmd = &cobra.Command{
	Use:   "rm <project> <rule-id>",
	Short: "Remove a rule (ids are shown by 'runq section ls')",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(strings.TrimPrefix(args[1], "#"), 10, 64)
		if err != nil {
			return fmt.Errorf("rule id must be a number (see runq section ls %s)", args[0])
		}
		return withSectioner(cmd, func(sec backend.Sectioner) error {
			if err := sec.RemoveSectionRule(cmd.Context(), args[0], id); err != nil {
				return err
			}
			fmt.Printf("rule #%d removed\n", id)
			return nil
		})
	},
}

func init() {
	sectionLsCmd.Flags().Bool("json", false, "output raw JSON")
	sectionRuleAddCmd.Flags().String("name", "", "glob on the experiment name")
	sectionRuleAddCmd.Flags().String("path", "", "glob on the job file path")
	sectionMoveCmd.Flags().String("view", "", "file this saved view instead of experiments")
	sectionMoveCmd.Flags().String("project", "", "the view's project, when several projects have a view by that name")
	sectionRuleCmd.AddCommand(sectionRuleAddCmd, sectionRuleRmCmd)
	sectionCmd.AddCommand(sectionLsCmd, sectionCreateCmd, sectionRmCmd, sectionMoveCmd, sectionSortCmd, sectionRuleCmd)
	sectionCmd.GroupID = groupManagement
	rootCmd.AddCommand(sectionCmd)
}
