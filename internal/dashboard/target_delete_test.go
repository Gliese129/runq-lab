package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/store"
)

// projectsBackend overrides the project lookups the delete guard reads.
type projectsBackend struct {
	backend.Backend
	projects map[string]project.Config
	archived map[string]bool
}

func (b projectsBackend) ListProjects(context.Context) ([]backend.ProjectSummary, error) {
	var out []backend.ProjectSummary
	for name := range b.projects {
		out = append(out, backend.ProjectSummary{Name: name, Archived: b.archived[name]})
	}
	return out, nil
}

func (b projectsBackend) GetProject(_ context.Context, name string) (*project.Config, error) {
	c := b.projects[name]
	return &c, nil
}

func TestDeleteTarget(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RUNQ_DATA_DIR", dir)
	cfgYAML := `default_target: a
targets:
  - name: a
    scheduler: slurm
    submit_template: sbatch {{run_sh}}
  - name: b
    scheduler: slurm
    submit_template: sbatch {{run_sh}}
  - name: c
    scheduler: slurm
    submit_template: sbatch {{run_sh}}
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfgYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	multi, err := backend.NewMultiBackend(map[string]backend.Backend{}, st, "")
	if err != nil {
		t.Fatal(err)
	}
	be := projectsBackend{
		Backend: multi,
		projects: map[string]project.Config{
			"home-b":   {ProjectName: "home-b", Target: "b"},
			"member-c": {ProjectName: "member-c", Target: "a", Targets: []string{"a", "c"}},
			"old":      {ProjectName: "old", Target: "a"},
		},
		archived: map[string]bool{"member-c": true},
	}
	server := NewServer(be, &config.GlobalConfig{})
	changed := 0
	server.SetConfigChanged(func() { changed++ })
	del := func(name string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/targets/"+name, nil))
		return rec
	}

	if rec := del("b"); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "home-b") {
		t.Fatalf("delete of a project's home target: %d %s", rec.Code, rec.Body.String())
	}
	if rec := del("nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("delete of unknown target: %d %s", rec.Code, rec.Body.String())
	}
	// c is only named by an archived project — fine to delete.
	if rec := del("c"); rec.Code != http.StatusOK {
		t.Fatalf("delete c: %d %s", rec.Code, rec.Body.String())
	}
	be.projects["old"] = project.Config{ProjectName: "old", Target: "b"}
	// "home-b" and "old" now live on b; nothing visible uses a.
	rec := del("a")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"was_default":true`) {
		t.Fatalf("delete default target a: %d %s", rec.Code, rec.Body.String())
	}
	after, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Targets) != 1 || after.Targets[0].Name != "b" || after.DefaultTarget != "" {
		t.Fatalf("config after deletes: targets=%v default=%q", after.Targets, after.DefaultTarget)
	}
	if changed != 2 {
		t.Fatalf("config reconciler notified %d times, want 2", changed)
	}
}
