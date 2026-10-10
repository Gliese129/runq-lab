package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccessLogRecordsRoutePatternAndClient(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush() // SSE handlers rely on Flush surviving the wrapper
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hello"))
	})
	var buf bytes.Buffer
	h := accessLogMiddleware(&buf, mux)

	web := httptest.NewRequest("GET", "/api/v1/jobs/jb123", nil)
	h.ServeHTTP(httptest.NewRecorder(), web)
	cli := httptest.NewRequest("GET", "/api/v1/jobs/jb456", nil)
	cli.Header.Set("X-Runq-Version", "0.5.0")
	h.ServeHTTP(httptest.NewRecorder(), cli)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/assets/app.js", nil))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (static skipped): %q", len(lines), buf.String())
	}
	var e accessEntry
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatal(err)
	}
	if e.Route != "GET /api/v1/jobs/{id}" || e.Client != "web" || e.Status != http.StatusOK || e.Bytes != 5 {
		// Flush before WriteHeader commits 200; the recorder must agree with the wire.
		t.Fatalf("web entry = %+v", e)
	}
	if strings.Contains(lines[0], "jb123") {
		t.Fatalf("access log leaked a job id: %s", lines[0])
	}
	if err := json.Unmarshal([]byte(lines[1]), &e); err != nil || e.Client != "cli" {
		t.Fatalf("cli entry = %+v, %v", e, err)
	}
}

func TestRotatingFileRotatesPastLimit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "access.log")
	if err := os.WriteFile(p, bytes.Repeat([]byte("x"), accessLogMaxBytes), 0o644); err != nil {
		t.Fatal(err)
	}
	rf, err := OpenRotatingFile(p)
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()
	if _, err := rf.Write([]byte("new\n")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "new\n" {
		t.Fatalf("current log = %d bytes, want just the new line", len(got))
	}
	if info, err := os.Stat(p + ".1"); err != nil || info.Size() != accessLogMaxBytes {
		t.Fatalf("rotated generation missing: %v", err)
	}
}
