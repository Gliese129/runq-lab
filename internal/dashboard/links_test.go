package dashboard

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/config"
)

func TestBaseURL(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:8077": "http://127.0.0.1:8077",
		"localhost:9000": "http://localhost:9000",
		"0.0.0.0:8077":   "http://127.0.0.1:8077",
		"[::]:8077":      "http://127.0.0.1:8077",
		":8077":          "http://127.0.0.1:8077",
		"[::1]:8077":     "http://[::1]:8077",
	}
	for listen, want := range cases {
		if got := BaseURL(listen); got != want {
			t.Errorf("BaseURL(%q) = %q, want %q", listen, got, want)
		}
	}
}

// Paths must match web/dashboard/src/plugins/router.ts.
func TestPageURLs(t *testing.T) {
	base := "http://127.0.0.1:8077/"
	if got, want := JobURL(base, "grpo", "jb1a2b3c4d"), "http://127.0.0.1:8077/#/projects/grpo/jb1a2b3c4d"; got != want {
		t.Errorf("JobURL = %q, want %q", got, want)
	}
	if got, want := TaskURL(base, "my proj", "jb1", "tk2"), "http://127.0.0.1:8077/#/projects/my%20proj/jb1/tk2"; got != want {
		t.Errorf("TaskURL = %q, want %q", got, want)
	}
	if got, want := ViewURL(base, "grpo", "paper"), "http://127.0.0.1:8077/#/projects/grpo/views/paper"; got != want {
		t.Errorf("ViewURL = %q, want %q", got, want)
	}
	if got, want := DraftURL(base, "dr8f2k1c00"), "http://127.0.0.1:8077/#/drafts/dr8f2k1c00"; got != want {
		t.Errorf("DraftURL = %q, want %q", got, want)
	}
	if got := JobURL("", "grpo", "jb1"); got != "" {
		t.Errorf("JobURL without base = %q, want empty", got)
	}
	if got := TaskURL(base, "", "jb1", "tk2"); got != "" {
		t.Errorf("TaskURL without project = %q, want empty", got)
	}
}

// /health advertises dashboard_url only when the UI assets are installed.
func TestHealthDashboardURL(t *testing.T) {
	health := func(s *Server) map[string]any {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/health", nil))
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	s := NewServer(backend.NewUnavailableBackend(errors.New("offline")), &config.GlobalConfig{})
	s.SetDashboardURL("http://127.0.0.1:8077")

	s.static = nil
	if _, ok := health(s)["dashboard_url"]; ok {
		t.Error("dashboard_url advertised without UI assets")
	}
	s.static = fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	if got := health(s)["dashboard_url"]; got != "http://127.0.0.1:8077" {
		t.Errorf("dashboard_url = %v, want http://127.0.0.1:8077", got)
	}
}
