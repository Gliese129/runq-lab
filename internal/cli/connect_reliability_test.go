package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReleaseDownloadRejectsFailureAndOversize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte("too large"))
	}))
	defer srv.Close()
	if _, err := downloadRelease(context.Background(), srv.URL+"/missing", 100); err == nil {
		t.Fatal("missing release accepted")
	}
	if _, err := downloadRelease(context.Background(), srv.URL+"/large", 3); err == nil {
		t.Fatal("oversized asset accepted")
	}
}
