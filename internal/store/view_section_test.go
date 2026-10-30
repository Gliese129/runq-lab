package store

import (
	"context"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/views"
)

// A view filed under a section reads that section's experiments by
// default, plus `jobs:` extras; removing the section makes it project-level.
func TestViewSection(t *testing.T) {
	s := newSectionStore(t)
	ctx := context.Background()
	if _, err := s.CreateSection(ctx, "kl", "ablation"); err != nil {
		t.Fatal(err)
	}
	abl, _ := s.ResolveSection(ctx, "kl", "ablation")
	addJob(t, s, "exa", "a", abl)
	addJob(t, s, "exb", "b", abl)
	addJob(t, s, "exbase", "baseline", "")

	spec := views.Spec{Name: "suite", Project: "kl", Section: "eval"} // spec.Section = result group, not the filing
	if err := s.SaveView(ctx, SavedView{Spec: spec}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetViewSection(ctx, "kl", "suite", "nope"); err == nil || !strings.Contains(err.Error(), "runq section create") {
		t.Fatalf("unknown section must list how to create one, got %v", err)
	}
	if err := s.SetViewSection(ctx, "kl", "suite", "ablation"); err != nil {
		t.Fatal(err)
	}
	v, _ := s.GetView(ctx, "kl", "suite")
	if v.InSection != "ablation" || v.Spec.Section != "eval" {
		t.Fatalf("filing and result group must stay apart, got in_section=%q section=%q", v.InSection, v.Spec.Section)
	}

	ids := func(spec views.Spec, override []string) []string {
		rows, err := s.ViewJobs(ctx, spec, override)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, r := range rows {
			out = append(out, r.ID)
		}
		return out
	}
	if got := strings.Join(ids(spec, nil), ","); got != "exa,exb" {
		t.Fatalf("default = the section's experiments, got %s", got)
	}
	addJob(t, s, "exc", "c", abl)
	if got := strings.Join(ids(spec, nil), ","); got != "exa,exb,exc" {
		t.Fatalf("a new experiment in the section must join, got %s", got)
	}
	withBase := spec
	withBase.Jobs = []string{"exbase"}
	if got := strings.Join(ids(withBase, nil), ","); got != "exa,exb,exc,exbase" {
		t.Fatalf("jobs: adds extras, got %s", got)
	}
	if got := strings.Join(ids(spec, []string{"exb"}), ","); got != "exb" {
		t.Fatalf("an explicit pick wins, got %s", got)
	}

	if _, err := s.RemoveSection(ctx, "kl", "ablation"); err != nil {
		t.Fatal(err)
	}
	v, _ = s.GetView(ctx, "kl", "suite")
	if v.InSection != "" {
		t.Fatalf("removing the section makes the view project-level, got %q", v.InSection)
	}
}
