package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func newSectionStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.DB().Exec(`INSERT INTO projects (name, id, config_json) VALUES ('kl', 'pj00000001', '{}')`); err != nil {
		t.Fatal(err)
	}
	return s
}

func addJob(t *testing.T, s *Store, id, name, section string) {
	t.Helper()
	if err := s.InsertJob(context.Background(), &JobRow{ID: id, ProjectName: "kl", Name: name, SectionID: section, ConfigJSON: "{}", Status: "done", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func TestSectionsBuiltinsAndRules(t *testing.T) {
	s := newSectionStore(t)
	ctx := context.Background()

	secs, err := s.ListSections(ctx, "kl")
	if err != nil {
		t.Fatal(err)
	}
	if len(secs) != 2 || secs[0].Name != SectionUnsorted || secs[1].Name != SectionSmoke || len(secs[1].Rules) != 1 {
		t.Fatalf("want unsorted + smoke with its default rule, got %+v", secs)
	}
	if _, err := s.CreateSection(ctx, "kl", "unsorted"); err == nil {
		t.Fatal("unsorted is reserved")
	}
	if _, err := s.CreateSection(ctx, "kl", "KL 消融"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSection(ctx, "kl", "KL 消融"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate section must fail, got %v", err)
	}
	if _, err := s.ResolveSection(ctx, "kl", "nope"); err == nil || !strings.Contains(err.Error(), "runq section create") {
		t.Fatalf("miss must say how to create it, got %v", err)
	}

	// Rules apply in order; first match wins. The default smoke rule is first.
	if _, err := s.AddRule(ctx, "kl", "KL 消融", RuleName, "kl*"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRule(ctx, "kl", "KL 消融", RulePath, "exp/kl/**"); err != nil {
		t.Fatal(err)
	}
	klID, _ := s.ResolveSection(ctx, "kl", "KL 消融")
	smokeID, _ := s.ResolveSection(ctx, "kl", SectionSmoke)
	for _, c := range []struct{ name, path, want string }{
		{"kl smoke", "", smokeID},
		{"KL=0.1", "", klID},
		{"baseline", "exp/kl/deep/a.yaml", klID},
		{"baseline", "exp/other.yaml", ""},
	} {
		got, _, err := s.MatchSection(ctx, "kl", c.name, c.path)
		if err != nil || got != c.want {
			t.Errorf("MatchSection(%q, %q) = %q, %v; want %q", c.name, c.path, got, err, c.want)
		}
	}
}

func TestSectionMoveSortAndRemove(t *testing.T) {
	s := newSectionStore(t)
	ctx := context.Background()
	if _, err := s.CreateSection(ctx, "kl", "main"); err != nil {
		t.Fatal(err)
	}
	addJob(t, s, "jbaa000001", "a1 launch smoke", "")
	addJob(t, s, "jbaa000002", "A1 原始基线", "")

	if ids, err := s.SetJobSection(ctx, "main", []string{"jbaa000002"}); err != nil || ids[0] != "jbaa000002" {
		t.Fatalf("move: %v %v", ids, err)
	}
	if moved, err := s.SortUnsorted(ctx, "kl"); err != nil || moved != 1 {
		t.Fatalf("sort should move the smoke job only: %d %v", moved, err)
	}
	if j, _ := s.GetJob(ctx, "jbaa000001"); j.Section != SectionSmoke {
		t.Fatalf("smoke job landed in %q", j.Section)
	}
	if _, err := s.RemoveSection(ctx, "kl", SectionSmoke); err == nil {
		t.Fatal("built-in smoke cannot be removed")
	}
	if moved, err := s.RemoveSection(ctx, "kl", "main"); err != nil || moved != 1 {
		t.Fatalf("removing main should unsort its job: %d %v", moved, err)
	}
	if j, _ := s.GetJob(ctx, "jbaa000002"); j.SectionID != "" {
		t.Fatalf("job should be unsorted, got %q", j.Section)
	}
}

// Section counts are of live experiments: an archived one leaves its tab's
// number (it is listed under archived, not in the section).
func TestSectionCountsLeaveArchivedOut(t *testing.T) {
	s := newSectionStore(t)
	ctx := context.Background()
	sec, err := s.CreateSection(ctx, "kl", "main")
	if err != nil {
		t.Fatal(err)
	}
	addJob(t, s, "jbcount0001", "a", sec.ID)
	addJob(t, s, "jbcount0002", "b", sec.ID)
	addJob(t, s, "jbcount0003", "c", "")
	addJob(t, s, "jbcount0004", "d", "")
	for _, id := range []string{"jbcount0002", "jbcount0004"} {
		if err := s.ArchiveJob(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	secs, err := s.ListSections(ctx, "kl")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, x := range secs {
		got[x.Name] = x.Jobs
	}
	if got["main"] != 1 || got[SectionUnsorted] != 1 {
		t.Fatalf("counts = %v", got)
	}
}

// Sections take the order they are given (unsorted stays first); a list
// that misses or repeats a section is refused.
func TestOrderSections(t *testing.T) {
	s := newSectionStore(t)
	ctx := context.Background()
	for _, n := range []string{"a", "b"} {
		if _, err := s.CreateSection(ctx, "kl", n); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.OrderSections(ctx, "kl", []string{"b", "smoke", "a"}); err != nil {
		t.Fatal(err)
	}
	secs, err := s.ListSections(ctx, "kl")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, x := range secs {
		got = append(got, x.Name)
	}
	if strings.Join(got, ",") != "unsorted,b,smoke,a" {
		t.Fatalf("order = %v", got)
	}
	for _, bad := range [][]string{{"a", "b"}, {"a", "a", "b", "smoke"}, {"a", "b", "smoke", "nope"}} {
		if err := s.OrderSections(ctx, "kl", bad); err == nil {
			t.Errorf("order %v: want an error", bad)
		}
	}
}
