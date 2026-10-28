package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gliese129/runq-lab/internal/api"
	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/dashboard"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/views"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func viewProject(cmd *cobra.Command, p *api.Proxy, name string) (string, error) {
	project, _ := cmd.Flags().GetString("project")
	if project != "" {
		return project, nil
	}
	raw, err := os.ReadFile("project.yaml")
	if err == nil {
		var cfg struct {
			Name string `yaml:"project_name"`
		}
		if yaml.Unmarshal(raw, &cfg) == nil && cfg.Name != "" {
			return cfg.Name, nil
		}
	}
	if name == "" {
		return "", nil
	}
	list, err := p.ListViews(cmd.Context(), "")
	if err != nil {
		return "", err
	}
	matches := []string{}
	for _, v := range list {
		if v.Name == name {
			matches = append(matches, v.Project)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("view %q not found; save a view file first", name)
	}
	return "", fmt.Errorf("view %q exists in %v; use --project", name, matches)
}

var viewCmd = &cobra.Command{Use: "view <file.yaml | name> | view --experiment <ex…>", Short: "Render a saved research view (cached data); --experiment alone shows an experiment's recorded results", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		jobs, _ := cmd.Flags().GetStringArray("experiment")
		if len(jobs) != 1 {
			return fmt.Errorf("give a view (file or name), or exactly one --experiment <ex…> for that experiment's recorded results")
		}
		return runJobView(cmd, jobs[0])
	}
	if spec, _ := cmd.Flags().GetBool("spec"); spec {
		return fmt.Errorf("--spec prints an experiment's default views: runq view --experiment <ex…> --spec (a saved view's YAML is the file you saved)")
	}
	return withBackend(cmd, func(be backend.Backend) error {
		p := be.(*api.Proxy)
		name := args[0]
		var project string
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			raw, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			var spec views.Spec
			dec := yaml.NewDecoder(bytes.NewReader(raw))
			dec.KnownFields(true)
			if err = dec.Decode(&spec); err != nil {
				return err
			}
			flag, _ := cmd.Flags().GetString("project")
			if flag != "" && spec.Project != "" && flag != spec.Project {
				return fmt.Errorf("--project conflicts with file project")
			}
			if flag != "" {
				spec.Project = flag
			}
			if spec.Project == "" {
				spec.Project, err = viewProject(cmd, p, "")
				if err != nil {
					return err
				}
			}
			absolute, err := filepath.Abs(name)
			if err != nil {
				return err
			}
			v := store.SavedView{Spec: spec, SourcePath: filepath.Clean(absolute), Templates: map[string]string{}}
			for kind, file := range spec.Custom {
				path := file
				if !filepath.IsAbs(path) {
					path = filepath.Join(filepath.Dir(absolute), path)
				}
				info, err := os.Stat(path)
				if err != nil {
					return err
				}
				if info.Size() > 256<<10 {
					return fmt.Errorf("template %s exceeds 256 KB", file)
				}
				content, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				v.Templates[kind] = string(content)
			}
			if err = p.SaveView(cmd.Context(), v); err != nil {
				return err
			}
			name = spec.Name
			project = spec.Project
		} else {
			var err error
			project, err = viewProject(cmd, p, name)
			if err != nil {
				return err
			}
		}
		openView, _ := cmd.Flags().GetBool("open")
		if openView {
			address := dashboard.ViewURL(dashboardBase(cmd.Context(), p), project, name)
			if address == "" {
				return fmt.Errorf("the runq service is not serving the dashboard UI (check `runq serve status`, or reinstall with the dashboard)")
			}
			fmt.Fprintln(cmd.OutOrStdout(), address)
			return openBrowser(cmd.Context(), cmd.ErrOrStderr(), address)
		}
		jobs, _ := cmd.Flags().GetStringArray("experiment")
		for i, ref := range jobs { // short ids, as everywhere else
			id, err := resolveJobRef(cmd.Context(), be, ref)
			if err != nil {
				return err
			}
			jobs[i] = id
		}
		refresh, _ := cmd.Flags().GetBool("refresh")
		r, err := p.RenderView(cmd.Context(), project, name, jobs, refresh)
		if err != nil {
			return err
		}
		format, _ := cmd.Flags().GetString("format")
		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			format = "json"
		}
		wide, _ := cmd.Flags().GetBool("wide")
		color, _ := cmd.Flags().GetBool("latex-color")
		text, err := views.Export(r, format, wide, color)
		if format == "latex" || format == "markdown" {
			saved, getErr := p.GetView(cmd.Context(), project, name)
			if getErr != nil {
				return getErr
			}
			if tpl, ok := saved.Templates[format]; ok {
				text, err = views.RenderTemplate(tpl, format, r)
			}
		}
		if err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), text)
		return nil
	})
}}

// runJobView is `runq view --job jb…` with no view named: the job through
// the view engine (views.JobViews), one table per producer and section, in
// the same formats as a saved view.
func runJobView(cmd *cobra.Command, ref string) error {
	if refresh, _ := cmd.Flags().GetBool("refresh"); refresh {
		return fmt.Errorf("--refresh re-runs a saved view's script sources; an experiment's own view has none (use --fresh to re-pull its status)")
	}
	return withBackend(cmd, func(be backend.Backend) error {
		p := be.(*api.Proxy)
		d, err := be.GetJob(cmd.Context(), ref)
		if err != nil {
			return err
		}
		applyFresh(cmd, be, d.Job.ID)
		if openView, _ := cmd.Flags().GetBool("open"); openView {
			address := dashboard.JobURL(dashboardBase(cmd.Context(), p), d.Job.Project, d.Job.ID)
			if address == "" {
				return fmt.Errorf("the runq service is not serving the dashboard UI (check `runq serve status`, or reinstall with the dashboard)")
			}
			fmt.Fprintln(cmd.OutOrStdout(), address)
			return openBrowser(cmd.Context(), cmd.ErrOrStderr(), address)
		}
		res, err := p.RenderJobView(cmd.Context(), d.Job.ID)
		if err != nil {
			return err
		}
		if spec, _ := cmd.Flags().GetBool("spec"); spec {
			return printSpecs(cmd, res.Specs)
		}
		format, _ := cmd.Flags().GetString("format")
		if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut || format == "json" {
			printJSON(res)
			return nil
		}
		if len(res.Views) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "no recorded results — write them with runq.result(values, section=…) in your script")
			return nil
		}
		wide, _ := cmd.Flags().GetBool("wide")
		color, _ := cmd.Flags().GetBool("latex-color")
		for i, r := range res.Views {
			text, err := views.Export(r, format, wide, color)
			if err != nil {
				return err
			}
			if i > 0 {
				fmt.Fprintln(cmd.OutOrStdout())
			}
			fmt.Fprint(cmd.OutOrStdout(), text)
		}
		return nil
	})
}

// printSpecs prints an experiment's default views as view YAML, one
// document per view: save one as a file, edit it, and `runq view file.yaml`.
func printSpecs(cmd *cobra.Command, specs []views.Spec) error {
	if len(specs) == 0 {
		return fmt.Errorf("no recorded results, so no default view to print")
	}
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "# runq's default views for this experiment. Save one document as a .yaml")
	fmt.Fprintln(out, "# file, edit it, then `runq view <file>` saves and renders it.")
	for i, s := range specs {
		if i > 0 {
			fmt.Fprintln(out, "---")
		}
		raw, err := yaml.Marshal(s)
		if err != nil {
			return err
		}
		fmt.Fprint(out, string(raw))
	}
	return nil
}

func init() {
	viewCmd.Flags().String("project", "", "Project containing the view")
	viewCmd.Flags().StringArray("experiment", nil, "Use this experiment for this render only (repeatable)")
	viewCmd.Flags().StringArray("job", nil, "")
	retireFlag(viewCmd, "job", "use --experiment <ex…> (a job is now an experiment)")
	viewCmd.Flags().Bool("json", false, "Output the render API response")
	viewCmd.Flags().Bool("wide", false, "Include source IDs and timestamps")
	viewCmd.Flags().String("format", "text", "text, json, latex, markdown or csv")
	viewCmd.Flags().Bool("latex-color", false, "Color baseline deltas (requires xcolor)")
	viewCmd.Flags().Bool("open", false, "Open this view in the dashboard")
	viewCmd.Flags().Bool("refresh", false, "Refresh on-demand script sources")
	viewCmd.Flags().Bool("spec", false, "With --experiment alone: print its default views as editable view YAML")
	ls := &cobra.Command{Use: "ls", Short: "List saved views", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return withBackend(cmd, func(be backend.Backend) error {
			p := be.(*api.Proxy)
			project, err := viewProject(cmd, p, "")
			if err != nil {
				return err
			}
			list, err := p.ListViews(cmd.Context(), project)
			if err != nil {
				return err
			}
			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				printJSON(list)
				return nil
			}
			for _, v := range list {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", v.Project, v.Name, v.Title)
			}
			return nil
		})
	}}
	ls.Flags().Bool("json", false, "Output JSON")
	rm := &cobra.Command{Use: "rm <name>", Short: "Delete a saved view", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withBackend(cmd, func(be backend.Backend) error {
			p := be.(*api.Proxy)
			project, err := viewProject(cmd, p, args[0])
			if err != nil {
				return err
			}
			return p.DeleteView(cmd.Context(), project, args[0])
		})
	}}
	for _, c := range []*cobra.Command{ls, rm} {
		c.Flags().String("project", "", "Project containing the view")
		viewCmd.AddCommand(c)
	}
	viewCmd.GroupID = groupCore
	rootCmd.AddCommand(viewCmd)
}
