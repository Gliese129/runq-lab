package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/store"
)

func TestRC2RemovedRoutes(t *testing.T) {
	s := NewServer(backend.NewUnavailableBackend(nil), &config.GlobalConfig{})
	defer s.Close()
	for _, route := range []string{
		"GET /api/v1/ui", "PUT /api/v1/ui", "PUT /api/v1/config",
		"GET /api/v1/targets/presets", "PUT /api/v1/targets/a", "DELETE /api/v1/targets/a", "POST /api/v1/targets/a/check",
		"GET /api/v1/targets/a/fs/read", "GET /api/v1/targets/a/fs/glob", "POST /api/v1/targets/a/fs/parse-script", "GET /api/v1/targets/a/python-envs",
		"POST /api/v1/log-sessions", "GET /api/v1/log-sessions/id", "DELETE /api/v1/log-sessions/id", "GET /api/v1/log-sessions/id/search",
		"POST /api/v1/jobs/plan", "POST /api/v1/jobs/id/pause", "POST /api/v1/jobs/id/resume",
	} {
		method, path, _ := strings.Cut(route, " ")
		res := httptest.NewRecorder()
		s.Handler().ServeHTTP(res, httptest.NewRequest(method, path, strings.NewReader(`{}`)))
		if res.Code != http.StatusNotFound {
			t.Fatalf("removed %s: %d %s", route, res.Code, res.Body.String())
		}
	}
}

type forwardSubmitBackend struct {
	*submitCaptureBackend
	projectTarget string
}

func (b *forwardSubmitBackend) ResolveSubmitTarget(_ context.Context, cfg job.JobConfig, explicit string) (string, string, error) {
	target, source := b.projectTarget, "project"
	if cfg.Target != "" {
		target, source = cfg.Target, "job"
	}
	if explicit != "" {
		target, source = explicit, "cli"
	}
	return target, source, nil
}
func TestRC2ForwardVerifiesResolvedTarget(t *testing.T) {
	be := &forwardSubmitBackend{submitCaptureBackend: &submitCaptureBackend{UnavailableBackend: backend.NewUnavailableBackend(nil)}, projectTarget: "b"}
	s := NewServer(be, &config.GlobalConfig{})
	defer s.Close()
	for _, tt := range []struct {
		body   string
		status int
		target string
	}{
		{`{"config":{"project":"p"}}`, 403, ""},
		{`{"config":{"project":"p","target":"b"}}`, 403, ""},
		{`{"config":{"project":"p","target":"a"}}`, 200, ""},
		{`{"config":{"project":"p","target":"b"},"target":"a"}`, 200, "a"},
	} {
		res := httptest.NewRecorder()
		s.RemoteCLIHandler("a").ServeHTTP(res, httptest.NewRequest("POST", "/api/v1/jobs", strings.NewReader(tt.body)))
		if res.Code != tt.status {
			t.Fatalf("%s: %d %s", tt.body, res.Code, res.Body.String())
		}
		if tt.status == 200 && be.opts.Target != tt.target {
			t.Fatal("guard rewrote target precedence")
		}
	}
}

func TestRC2LocalHistoryPagingAndRestore(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m, err := backend.NewMultiBackend(map[string]backend.Backend{}, st, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 61; i++ {
		name := fmt.Sprintf("p%02d", i)
		raw, _ := json.Marshal(project.Config{ProjectName: name, Target: "removed", WorkingDir: "/remote/project"})
		archived := any(time.Now().Unix())
		if i == 60 {
			archived = nil
		}
		if _, err := st.DB().ExecContext(ctx, `INSERT INTO projects(name,config_json,archived_at)VALUES(?,?,?)`, name, string(raw), archived); err != nil {
			t.Fatal(err)
		}
	}
	s := NewServer(m, &config.GlobalConfig{})
	defer s.Close()
	get := func(path string) listEnvelope[backend.ProjectSummary] {
		res := httptest.NewRecorder()
		s.Handler().ServeHTTP(res, httptest.NewRequest("GET", path, nil))
		if res.Code != 200 {
			t.Fatalf("read failed: %d %s", res.Code, res.Body.String())
		}
		var out listEnvelope[backend.ProjectSummary]
		if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	visible := get("/api/v1/projects?archived=false")
	if len(visible.Items) != 1 {
		t.Fatalf("archived projects in main list: %+v", visible)
	}
	history := get("/api/v1/projects?archived=true&limit=25&offset=25")
	if len(history.Items) != 25 || *history.Total != 60 || history.Items[0].Name != "p25" {
		t.Fatalf("unbounded/wrong history: %+v", history)
	}
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, httptest.NewRequest("POST", "/api/v1/projects/p25/unarchive", nil))
	if res.Code != 200 {
		t.Fatalf("restore without target: %d %s", res.Code, res.Body.String())
	}
	if len(get("/api/v1/projects?archived=false").Items) != 2 || *get("/api/v1/projects?archived=true&limit=25").Total != 59 {
		t.Fatal("restore visibility inconsistent")
	}
}
