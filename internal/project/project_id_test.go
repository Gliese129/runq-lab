package project

import (
	"context"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/store"
)

func newIDRegistry(t *testing.T) (*Registry, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewRegistry(st.DB()), st
}

func TestProjectIDResolveAndRename(t *testing.T) {
	r, st := newIDRegistry(t)
	ctx := context.Background()
	if err := r.Add(ctx, Config{ProjectName: "kl", WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	cfg, err := r.Get(ctx, "kl")
	if err != nil {
		t.Fatal(err)
	}
	if !idShapedName.MatchString(cfg.ID) {
		t.Fatalf("Add should assign a pj… id, got %q", cfg.ID)
	}
	for _, ref := range []string{"kl", cfg.ID, cfg.ID[:5]} {
		if got, err := r.Resolve(ctx, ref); err != nil || got != "kl" {
			t.Errorf("Resolve(%q) = %q, %v; want kl", ref, got, err)
		}
	}

	// A view (and its cache row) carry the name; rename must move them too.
	db := st.DB()
	if _, err := db.Exec(`INSERT INTO views (project_name, name, spec_json, updated_at) VALUES ('kl','v','{}',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO view_source_cache (project_name, view_name, source, ran_at, ok) VALUES ('kl','v','s',0,1)`); err != nil {
		t.Fatal(err)
	}
	if err := r.Rename(ctx, cfg.ID, "kl-ablation"); err != nil {
		t.Fatal(err)
	}
	renamed, err := r.Get(ctx, cfg.ID)
	if err != nil || renamed.ProjectName != "kl-ablation" || renamed.ID != cfg.ID {
		t.Fatalf("after rename: %+v, %v (id must stay %s)", renamed, err, cfg.ID)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM view_source_cache WHERE project_name = 'kl-ablation'`).Scan(&n)
	if n != 1 {
		t.Fatalf("view cache not renamed")
	}
}

func TestProjectResolveErrorsListCandidates(t *testing.T) {
	r, _ := newIDRegistry(t)
	ctx := context.Background()
	for _, n := range []string{"a", "b"} {
		if err := r.Add(ctx, Config{ProjectName: n, WorkingDir: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Resolve(ctx, "pj"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("bare pj prefix should be ambiguous, got %v", err)
	}
	if _, err := r.Resolve(ctx, "nope"); err == nil || !strings.Contains(err.Error(), "known:") {
		t.Fatalf("miss should list known projects, got %v", err)
	}
	if err := r.Add(ctx, Config{ProjectName: "pj0123abcd", WorkingDir: t.TempDir()}); err == nil {
		t.Fatal("an id-shaped name must be rejected")
	}
}

func TestMigrateBackfillsProjectIDs(t *testing.T) {
	_, st := newIDRegistry(t)
	db := st.DB()
	if _, err := db.Exec(`INSERT INTO projects (name, config_json) VALUES ('old', '{}')`); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := db.QueryRow(`SELECT id FROM projects WHERE name = 'old'`).Scan(&id); err != nil || !idShapedName.MatchString(id) {
		t.Fatalf("pre-rc6 project should get an id on migrate, got %q %v", id, err)
	}
}
