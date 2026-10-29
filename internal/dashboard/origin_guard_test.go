package dashboard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/config"
)

// The TCP listener refuses cross-site writes (403, nothing done) and keeps
// same-host browser writes and header-less clients working.
func TestTCPHandlerRefusesCrossSiteWrites(t *testing.T) {
	for _, tt := range []struct {
		name    string
		host    string
		headers map[string]string
		body    string
		allowed bool
	}{
		{name: "foreign origin, text/plain", host: "127.0.0.1:8765",
			headers: map[string]string{"Origin": "https://untrusted.example", "Content-Type": "text/plain"}},
		{name: "foreign origin, json", host: "127.0.0.1:8765",
			headers: map[string]string{"Origin": "https://untrusted.example", "Content-Type": "application/json"}},
		{name: "other port on the same machine", host: "127.0.0.1:8765",
			headers: map[string]string{"Origin": "http://127.0.0.1:3000", "Content-Type": "application/json"}},
		{name: "opaque origin", host: "127.0.0.1:8765",
			headers: map[string]string{"Origin": "null", "Content-Type": "application/json"}},
		{name: "foreign referer without origin", host: "127.0.0.1:8765",
			headers: map[string]string{"Referer": "https://untrusted.example/page", "Content-Type": "application/json"}},
		{name: "same host, form post", host: "127.0.0.1:8765",
			headers: map[string]string{"Origin": "http://127.0.0.1:8765", "Content-Type": "application/x-www-form-urlencoded"}},
		{name: "no origin, text/plain body", host: "127.0.0.1:8765",
			headers: map[string]string{"Content-Type": "text/plain"}},
		{name: "no origin, body without a type", host: "127.0.0.1:8765"},

		{name: "dashboard on loopback", host: "127.0.0.1:8765", allowed: true,
			headers: map[string]string{"Origin": "http://127.0.0.1:8765", "Content-Type": "application/json"}},
		{name: "dashboard on a tailnet name", host: "box.tail1234.ts.net", allowed: true,
			headers: map[string]string{"Origin": "https://box.tail1234.ts.net", "Content-Type": "application/json; charset=utf-8"}},
		{name: "dashboard behind a reverse proxy", host: "127.0.0.1:8765", allowed: true,
			headers: map[string]string{"Origin": "https://box.tail1234.ts.net", "X-Forwarded-Host": "box.tail1234.ts.net", "Content-Type": "application/json"}},
		{name: "same-host referer", host: "localhost:5173", allowed: true,
			headers: map[string]string{"Referer": "http://localhost:5173/projects", "Content-Type": "application/json"}},
		{name: "curl", host: "127.0.0.1:8765", allowed: true,
			headers: map[string]string{"Content-Type": "application/json"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			be := &renameCaptureBackend{UnavailableBackend: backend.NewUnavailableBackend(errors.New("unused"))}
			s := NewServer(be, &config.GlobalConfig{})
			t.Cleanup(s.Close)
			req := httptest.NewRequest("POST", "http://"+tt.host+"/api/v1/projects/old-name/rename", strings.NewReader(`{"new_name":"changed"}`))
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			res := httptest.NewRecorder()
			s.TCPHandler().ServeHTTP(res, req)
			if tt.allowed {
				if res.Code != http.StatusOK || be.newName != "changed" {
					t.Fatalf("code=%d renamed=%q body=%s, want the write to go through", res.Code, be.newName, res.Body.String())
				}
				return
			}
			if res.Code != http.StatusForbidden || be.newName != "" {
				t.Fatalf("code=%d renamed=%q body=%s, want 403 and no rename", res.Code, be.newName, res.Body.String())
			}
			if !strings.Contains(res.Body.String(), "refused") {
				t.Fatalf("403 without a reason: %s", res.Body.String())
			}
		})
	}
}

// Reads stay open (CORS still governs what a page may read), and a body-less
// same-host action such as a kill passes.
func TestTCPHandlerLeavesReadsAndBodylessActions(t *testing.T) {
	s := NewServer(backend.NewUnavailableBackend(errors.New("down")), &config.GlobalConfig{})
	t.Cleanup(s.Close)
	req := httptest.NewRequest("GET", "http://127.0.0.1:8765/api/v1/health", nil)
	req.Header.Set("Origin", "https://untrusted.example")
	res := httptest.NewRecorder()
	s.TCPHandler().ServeHTTP(res, req)
	if res.Code == http.StatusForbidden {
		t.Fatalf("a read was refused: %s", res.Body.String())
	}
	req = httptest.NewRequest("POST", "http://127.0.0.1:8765/api/v1/jobs/ex1/kill", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8765")
	res = httptest.NewRecorder()
	s.TCPHandler().ServeHTTP(res, req)
	if res.Code == http.StatusForbidden {
		t.Fatalf("a same-host kill was refused: %s", res.Body.String())
	}
}

// A draft edit may send its YAML raw from the dashboard's own page.
func TestTCPHandlerAcceptsRawYAMLDraftEdit(t *testing.T) {
	req := httptest.NewRequest("PUT", "http://127.0.0.1:8765/api/v1/drafts/dr1", strings.NewReader("project: p\n"))
	req.Header.Set("Content-Type", "application/yaml")
	if msg := crossSiteWrite(req); msg != "" {
		t.Fatalf("raw YAML draft edit refused: %s", msg)
	}
	req = httptest.NewRequest("POST", "http://127.0.0.1:8765/api/v1/drafts", strings.NewReader("project: p\n"))
	req.Header.Set("Content-Type", "application/yaml")
	if msg := crossSiteWrite(req); msg == "" {
		t.Fatal("YAML accepted on a JSON endpoint")
	}
}
