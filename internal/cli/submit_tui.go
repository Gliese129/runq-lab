package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gliese129/runq-lab/internal/backend"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

// submitTUI is the human entry point (`runq submit job.yaml` in a terminal):
// the job file becomes a draft first, so the screen shows exactly what
// `runq draft` would; then one key decides — submit, edit, switch project,
// or quit keeping the draft. Everything it touches is injected for tests.
type submitTUI struct {
	ctx           context.Context
	d             backend.Drafter
	out           io.Writer
	key           func() (byte, error)     // one keypress
	edit          func(path string) error  // $EDITOR on a file
	projects      func() ([]string, error) // candidates, best first
	req           backend.DraftRequest
	skipPreflight bool
	fileWarning   func(project string) string // project.yaml changed since registration

	draft   *backend.DraftResult
	planErr error
	pending string // temp file holding edits that failed to save
}

func (t *submitTUI) run() error {
	t.plan()
	for {
		t.render()
		k, err := t.key()
		if err != nil {
			return err
		}
		switch k {
		case 's', 'S':
			if t.draft == nil {
				fmt.Fprintln(t.out, "nothing to submit yet — fix the plan with [e] or pick a project with [p]")
				continue
			}
			res, err := t.d.SubmitDraft(t.ctx, t.draft.Draft.ID, backend.DraftSubmitOptions{Rev: t.draft.Draft.Rev, SkipPreflight: t.skipPreflight})
			if err != nil {
				fmt.Fprintf(t.out, "submit failed: %v\nthe draft is kept: runq submit %s\n", err, t.draft.Draft.ID)
				return err
			}
			fmt.Fprintln(t.out, submittedLine(res))
			return nil
		case 'e', 'E':
			t.editPlan()
		case 'p', 'P':
			t.pickProject()
		case 'q', 'Q', 3, 4: // q, Ctrl-C, Ctrl-D
			if t.draft != nil {
				fmt.Fprintf(t.out, "draft %s kept — submit it later with: runq submit %s (list: runq draft ps)\n", t.draft.Draft.ID, t.draft.Draft.ID)
			}
			return nil
		}
	}
}

// plan (re)creates the draft from t.req; a planning error is shown, not fatal.
func (t *submitTUI) plan() {
	t.draft, t.planErr = nil, nil
	res, err := t.d.CreateDraft(t.ctx, t.req)
	if err != nil {
		t.planErr = err
		return
	}
	t.draft = res
	t.req.Project = res.Draft.Project
	if t.fileWarning != nil {
		if w := t.fileWarning(res.Draft.Project); w != "" {
			res.Warnings = append(res.Warnings, strings.TrimPrefix(w, "warning: "))
		}
	}
}

func (t *submitTUI) render() {
	fmt.Fprintln(t.out)
	if t.draft != nil {
		for _, w := range t.draft.Warnings {
			fmt.Fprintf(t.out, "warning: %s\n", w)
		}
		fmt.Fprint(t.out, t.draft.Preview)
		fmt.Fprintln(t.out)
		fmt.Fprint(t.out, t.draft.Table)
		v := t.draft.Draft
		fmt.Fprintln(t.out, draftLine(v))
		fmt.Fprintf(t.out, "[s] submit  [e] edit  [p] project (%s)  [q] quit, keep the draft\n", v.Project)
		return
	}
	project := t.req.Project
	if project == "" {
		project = "none"
	}
	fmt.Fprintf(t.out, "the plan does not validate:\n  %v\n", t.planErr)
	fmt.Fprintf(t.out, "[e] edit  [p] project (%s)  [q] quit\n", project)
}

// editPlan opens the draft (or, with no draft yet, the job file's text) in
// the editor and re-plans. Edits that fail to save stay in a temp file and
// reopen on the next [e].
func (t *submitTUI) editPlan() {
	var current []byte
	if t.draft != nil {
		y, err := t.d.DraftYAML(t.ctx, t.draft.Draft.ID)
		if err != nil {
			fmt.Fprintf(t.out, "read draft: %v\n", err)
			return
		}
		current = y
	} else {
		current = []byte(t.req.JobYAML)
	}
	path := t.pending
	if path == "" {
		f, err := os.CreateTemp("", "runq-plan-*.yaml")
		if err != nil {
			fmt.Fprintf(t.out, "temp file: %v\n", err)
			return
		}
		_, _ = f.Write(current)
		f.Close()
		path = f.Name()
	}
	if err := t.edit(path); err != nil {
		fmt.Fprintf(t.out, "editor: %v (edits in %s)\n", err, path)
		t.pending = path
		return
	}
	edited, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(t.out, "read %s: %v\n", path, err)
		return
	}
	if string(edited) == string(current) {
		os.Remove(path)
		t.pending = ""
		return
	}
	if t.draft == nil {
		os.Remove(path)
		t.pending = ""
		t.req.JobYAML = string(edited)
		t.plan()
		return
	}
	res, err := t.d.EditDraft(t.ctx, t.draft.Draft.ID, string(edited))
	if err != nil {
		fmt.Fprintf(t.out, "not saved: %v\nyour edits are kept; [e] reopens them\n", err)
		t.pending = path
		return
	}
	os.Remove(path)
	t.pending = ""
	t.draft = res
}

// pickProject lists up to 9 projects (this directory's first, most
// recently used first) and re-plans with the chosen one. A draft keeps its
// project, so the old draft is discarded and a new one made.
func (t *submitTUI) pickProject() {
	names, err := t.projects()
	if err != nil {
		fmt.Fprintf(t.out, "list projects: %v\n", err)
		return
	}
	if len(names) > 9 {
		names = names[:9]
	}
	if len(names) == 0 {
		fmt.Fprintln(t.out, "no projects registered (runq project add -f project.yaml)")
		return
	}
	for i, n := range names {
		fmt.Fprintf(t.out, "  [%d] %s\n", i+1, n)
	}
	fmt.Fprintln(t.out, "project number (any other key keeps the current one):")
	k, err := t.key()
	if err != nil || k < '1' || int(k-'0') > len(names) {
		return
	}
	chosen := names[k-'1']
	if chosen == t.req.Project && t.draft != nil {
		return
	}
	if t.draft != nil {
		_ = t.d.DiscardDraft(t.ctx, t.draft.Draft.ID)
	}
	t.req.Project = chosen
	t.plan()
}

// ── wiring for the real terminal ──

// interactive reports whether a human is at a terminal: stdout is one and
// /dev/tty opens (stdin may be the job file, as in `runq submit -`).
func interactive() (*os.File, bool) {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return nil, false
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, false
	}
	return tty, true
}

// ttyKey reads one keypress from tty in raw mode, restoring it after.
func ttyKey(tty *os.File) func() (byte, error) {
	return func() (byte, error) {
		fd := int(tty.Fd())
		old, err := term.MakeRaw(fd)
		if err != nil {
			return 0, err
		}
		defer term.Restore(fd, old)
		var b [1]byte
		if _, err := tty.Read(b[:]); err != nil {
			return 0, err
		}
		return b[0], nil
	}
}

// openEditor runs $VISUAL / $EDITOR (default vi) on path, attached to tty.
func openEditor(tty *os.File) func(string) error {
	return func(path string) error {
		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		if editor == "" {
			editor = "vi"
		}
		ed := exec.Command("sh", "-c", editor+` "$1"`, "sh", path)
		ed.Stdin, ed.Stdout, ed.Stderr = tty, tty, tty
		return ed.Run()
	}
}

// projectCandidates orders projects for the picker: those whose working_dir
// holds the current directory first, each group by most recent job.
func projectCandidates(ctx context.Context, be backend.Backend) func() ([]string, error) {
	return func() ([]string, error) {
		all, err := be.ListProjects(ctx)
		if err != nil {
			return nil, err
		}
		recent := map[string]int64{}
		if jobs, err := be.ListJobs(ctx, ""); err == nil {
			for _, j := range jobs {
				if j.CreatedAt > recent[j.Project] {
					recent[j.Project] = j.CreatedAt
				}
			}
		}
		here := map[string]bool{}
		if wd, err := os.Getwd(); err == nil {
			if m, err := be.MatchProjects(ctx, wd); err == nil {
				for _, p := range m {
					here[p.Name] = true
				}
			}
		}
		var names []string
		for _, p := range all {
			if !p.Archived {
				names = append(names, p.Name)
			}
		}
		sort.SliceStable(names, func(i, j int) bool {
			a, b := names[i], names[j]
			if here[a] != here[b] {
				return here[a]
			}
			return recent[a] > recent[b]
		})
		return names, nil
	}
}

// defaultProject is the job file's project, else this directory's most
// recently used project ("" when none matches).
func defaultProject(ctx context.Context, be backend.Backend, text string, candidates func() ([]string, error)) string {
	var head struct {
		Project string `yaml:"project"`
	}
	if yaml.Unmarshal([]byte(text), &head) == nil && head.Project != "" {
		return head.Project
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	m, err := be.MatchProjects(ctx, wd)
	if err != nil || len(m) == 0 {
		return ""
	}
	names, err := candidates()
	if err != nil || len(names) == 0 {
		return m[0].Name
	}
	return names[0] // here-first ordering puts a match first
}

const jobTemplate = `# New job for project %[1]s — save and quit to plan it; nothing runs until you press [s].
project: %[1]s
name: ""            # what this run set tests, in a few words
description: ""
# section: smoke    # an existing section (runq section ls %[1]s)
fixed_params: {}
tasks:
  - { run: B0 }     # one row per run: {run: R1 lr, lr: 2e-6}
`

// templateYAML opens a fresh job file in the editor; "" when left empty
// or unchanged.
func templateYAML(project string, edit func(string) error) (string, error) {
	if project == "" {
		project = "REPLACE-ME"
	}
	f, err := os.CreateTemp("", "runq-job-*.yaml")
	if err != nil {
		return "", err
	}
	start := fmt.Sprintf(jobTemplate, project)
	_, _ = f.WriteString(start)
	f.Close()
	defer os.Remove(f.Name())
	if err := edit(f.Name()); err != nil {
		return "", err
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		return "", err
	}
	if s := string(data); strings.TrimSpace(s) != "" && s != start {
		return s, nil
	}
	return "", nil
}

// sourcePathFor records a job file path as given (for path rules).
func sourcePathFor(arg string) string {
	switch arg {
	case "", "-":
		return ""
	case ".":
		return "job.yaml"
	}
	return filepath.ToSlash(arg)
}
