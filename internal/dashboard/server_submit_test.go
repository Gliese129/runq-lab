package dashboard

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/preflight"
)

type submitCaptureBackend struct {
	*backend.UnavailableBackend
	cfg  job.JobConfig
	opts backend.SubmitOptions
}

func (b *submitCaptureBackend) SubmitJob(_ context.Context, cfg job.JobConfig, opts backend.SubmitOptions) (string, int, error) {
	b.cfg = cfg
	b.opts = opts
	if opts.Preflight != nil {
		*opts.Preflight = preflight.Report{Results: []preflight.CheckResult{{Name: "entry", Status: "warning", Detail: "x.py does not exist"}}}
	}
	return "job-test", 1, nil
}

type renameCaptureBackend struct {
	*backend.UnavailableBackend
	oldName string
	newName string
}

func (b *renameCaptureBackend) RenameProject(_ context.Context, oldName, newName string) error {
	b.oldName = oldName
	b.newName = newName
	return nil
}

// Preflight cannot be skipped: a pre-rc6 skip_preflight is ignored, and the
// submit's warnings come back with the job.
func TestHandleSubmitJobReturnsPreflightWarnings(t *testing.T) {
	be := &submitCaptureBackend{UnavailableBackend: backend.NewUnavailableBackend(errors.New("unused"))}
	server := NewServer(be, &config.GlobalConfig{})
	body := `{"config":{"project":"pytrain-example","fixed_params":{"epochs":30},"sweep":[]},"skip_preflight":true}`
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if be.cfg.Project != "pytrain-example" || be.opts.Preflight == nil {
		t.Fatalf("submit not routed with a preflight out-param: %+v %+v", be.cfg, be.opts)
	}
	if !strings.Contains(rec.Body.String(), `"name":"entry","status":"warning"`) {
		t.Fatalf("preflight warnings missing from the response: %s", rec.Body.String())
	}
}

func TestHandleRenameProjectRoutesToBackend(t *testing.T) {
	backend := &renameCaptureBackend{
		UnavailableBackend: backend.NewUnavailableBackend(errors.New("unused")),
	}
	server := NewServer(backend, &config.GlobalConfig{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/old-name/rename", strings.NewReader(`{"new_name":"new-name"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if backend.oldName != "old-name" || backend.newName != "new-name" {
		t.Fatalf("rename = (%q, %q), want (old-name, new-name)", backend.oldName, backend.newName)
	}
}
