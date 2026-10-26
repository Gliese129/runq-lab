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
)

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
