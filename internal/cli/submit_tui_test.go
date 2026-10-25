package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/backend"
)

// fakeDrafter records what the TUI asks of the draft service.
type fakeDrafter struct {
	n         int
	projects  []string // project of each created draft
	submitted []string
	discarded []string
	edits     []string
	failFor   string // CreateDraft fails for this project
}

func (f *fakeDrafter) CreateDraft(_ context.Context, req backend.DraftRequest) (*backend.DraftResult, error) {
	if req.Project == "" || req.Project == f.failFor {
		return nil, errors.New("draft needs an explicit project")
	}
	f.n++
	f.projects = append(f.projects, req.Project)
	id := fmt.Sprintf("dr%08d", f.n)
	return &backend.DraftResult{Draft: backend.DraftView{ID: id, Project: req.Project, Rev: 1, Rows: 2}, Table: "RUN\nB0 (baseline)\n", Preview: "name: x\n"}, nil
}
func (f *fakeDrafter) ListDrafts(context.Context, string) ([]backend.DraftView, error) {
	return nil, nil
}
func (f *fakeDrafter) GetDraft(context.Context, string) (*backend.DraftResult, error) {
	return nil, nil
}
func (f *fakeDrafter) DraftYAML(_ context.Context, id string) ([]byte, error) {
	return []byte("draft: " + id + "\nrev: 1\ntasks:\n  - {run: B0}\n"), nil
}
func (f *fakeDrafter) EditDraft(_ context.Context, id, text string) (*backend.DraftResult, error) {
	f.edits = append(f.edits, text)
	return &backend.DraftResult{Draft: backend.DraftView{ID: id, Project: f.projects[len(f.projects)-1], Rev: 2, Rows: 3}, Table: "RUN\nB0\nR1\n"}, nil
}
func (f *fakeDrafter) SubmitDraft(_ context.Context, id string, rev int, _ bool) (*backend.DraftSubmitResult, error) {
	f.submitted = append(f.submitted, fmt.Sprintf("%s@%d", id, rev))
	return &backend.DraftSubmitResult{JobID: "jb00000001", TotalTasks: 2, DraftID: id}, nil
}
func (f *fakeDrafter) DiscardDraft(_ context.Context, id string) error {
	f.discarded = append(f.discarded, id)
	return nil
}

func scriptedTUI(f *fakeDrafter, keys string, project string, edit func(string) error) (*submitTUI, *bytes.Buffer) {
	out := &bytes.Buffer{}
	i := 0
	return &submitTUI{
		ctx: context.Background(), d: f, out: out,
		key: func() (byte, error) {
			if i >= len(keys) {
				return 0, errors.New("script ran out of keys")
			}
			i++
			return keys[i-1], nil
		},
		edit:     edit,
		projects: func() ([]string, error) { return []string{"kl", "eval"}, nil },
		req:      backend.DraftRequest{JobYAML: "tasks: [{run: B0}]", Project: project},
	}, out
}

func TestTUISubmitAndQuit(t *testing.T) {
	f := &fakeDrafter{}
	ui, out := scriptedTUI(f, "s", "kl", nil)
	if err := ui.run(); err != nil {
		t.Fatal(err)
	}
	if len(f.submitted) != 1 || f.submitted[0] != "dr00000001@1" || !strings.Contains(out.String(), "submitted job") {
		t.Fatalf("s should submit the shown rev: %v\n%s", f.submitted, out)
	}

	f = &fakeDrafter{}
	ui, out = scriptedTUI(f, "xq", "kl", nil) // unknown keys are ignored
	if err := ui.run(); err != nil {
		t.Fatal(err)
	}
	if len(f.submitted) != 0 || len(f.discarded) != 0 || !strings.Contains(out.String(), "dr00000001 kept") {
		t.Fatalf("q must keep the draft and submit nothing:\n%s", out)
	}
}

func TestTUIEditThenSubmit(t *testing.T) {
	f := &fakeDrafter{}
	edit := func(path string) error {
		return os.WriteFile(path, []byte("draft: dr00000001\nrev: 1\ntasks:\n  - {run: B0}\n  - {run: R1, lr: 2}\n"), 0o644)
	}
	ui, out := scriptedTUI(f, "es", "kl", edit)
	if err := ui.run(); err != nil {
		t.Fatal(err)
	}
	if len(f.edits) != 1 || !strings.Contains(f.edits[0], "R1") {
		t.Fatalf("e should save the edited plan: %v", f.edits)
	}
	if len(f.submitted) != 1 || f.submitted[0] != "dr00000001@2" {
		t.Fatalf("submit must name the edited rev: %v\n%s", f.submitted, out)
	}
}

func TestTUIPickProjectReplansAndDiscards(t *testing.T) {
	f := &fakeDrafter{}
	ui, _ := scriptedTUI(f, "p2s", "kl", nil)
	if err := ui.run(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.projects, ",") != "kl,eval" || len(f.discarded) != 1 || f.submitted[0] != "dr00000002@1" {
		t.Fatalf("project switch: created %v, discarded %v, submitted %v", f.projects, f.discarded, f.submitted)
	}

	// No project yet: the plan error is shown, [p] fixes it, [s] before that does nothing.
	f = &fakeDrafter{}
	ui, out := scriptedTUI(f, "sp1s", "", nil)
	if err := ui.run(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "does not validate") || len(f.submitted) != 1 || f.projects[0] != "kl" {
		t.Fatalf("missing project flow:\n%s", out)
	}
}

func TestTemplateYAML(t *testing.T) {
	untouched := func(string) error { return nil }
	if text, err := templateYAML("kl", untouched); err != nil || text != "" {
		t.Fatalf("an untouched template plans nothing: %q %v", text, err)
	}
	filled := func(p string) error {
		return os.WriteFile(p, []byte("project: kl\ntasks:\n  - {run: B0}\n"), 0o644)
	}
	if text, err := templateYAML("kl", filled); err != nil || !strings.Contains(text, "run: B0") {
		t.Fatalf("edited template: %q %v", text, err)
	}
}
