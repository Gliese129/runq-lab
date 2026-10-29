package cli

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gliese129/runq-lab/internal/backend"
)

// fakeKillService serves the reads kill resolves with and records the kill
// requests it receives ("POST /path?query").
type fakeKillService struct {
	mu    sync.Mutex
	kills []string
}

func (f *fakeKillService) serve(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rqkill")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			if r.URL.Path == "/api/v1/tasks/exb/kill" { // not a version: kill falls through to the experiment
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"code":"not_found","error":"not found"}`))
				return
			}
			f.mu.Lock()
			f.kills = append(f.kills, r.URL.Path+"?"+r.URL.RawQuery)
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		switch r.URL.Path {
		case "/api/v1/jobs/exb": // every version on b
			_, _ = w.Write([]byte(`{"job":{"id":"exb","target":"b"},"tasks":[{"id":"tkb1","target":"b"}]}`))
		case "/api/v1/jobs/exm": // first submission on a, an append on b
			_, _ = w.Write([]byte(`{"job":{"id":"exm","target":"a"},"tasks":[{"id":"tka1","target":"a"},{"id":"tkb2","target":"b"}]}`))
		case "/api/v1/tasks/tkb1":
			_, _ = w.Write([]byte(`{"id":"tkb1","job_id":"exb","target":"b"}`))
		case "/api/v1/tasks/tka1":
			_, _ = w.Write([]byte(`{"id":"tka1","job_id":"exm","target":"a"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"not_found","error":"not found"}`))
		}
	})}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	_ = rootCmd.PersistentFlags().Set("socket", sock)
	t.Cleanup(func() { _ = rootCmd.PersistentFlags().Set("socket", "") })
}

func runKillWithTarget(t *testing.T, ref, target string) error {
	t.Helper()
	_ = killCmd.Flags().Set("target", target)
	if target == "" {
		killCmd.Flags().Lookup("target").Changed = false
	}
	t.Cleanup(func() {
		_ = killCmd.Flags().Set("target", "")
		killCmd.Flags().Lookup("target").Changed = false
	})
	killCmd.SetContext(context.Background())
	var err error
	captureStdout(t, func() { err = runKill(killCmd, []string{ref}) })
	return err
}

// kill -t scopes both the resolution and the kill: an experiment or a
// version with nothing on the target is refused without any kill request,
// and an experiment spanning targets loses only its versions on the target.
func TestKillTargetScopesTheKill(t *testing.T) {
	f := &fakeKillService{}
	f.serve(t)

	if err := runKillWithTarget(t, "exb", "a"); !errors.Is(err, backend.ErrNotOnTarget) {
		t.Fatalf("kill exb -t a = %v, want a not-on-target refusal", err)
	}
	if err := runKillWithTarget(t, "tkb1", "a"); !errors.Is(err, backend.ErrNotOnTarget) {
		t.Fatalf("kill tkb1 -t a = %v, want a not-on-target refusal", err)
	}
	if len(f.kills) != 0 {
		t.Fatalf("refused kills still reached the service: %v", f.kills)
	}

	if err := runKillWithTarget(t, "exm", "a"); err != nil {
		t.Fatal(err)
	}
	if err := runKillWithTarget(t, "tka1", "a"); err != nil {
		t.Fatal(err)
	}
	if err := runKillWithTarget(t, "exb", ""); err != nil {
		t.Fatal(err)
	}
	want := []string{"/api/v1/jobs/exm/kill?target=a", "/api/v1/tasks/tka1/kill?", "/api/v1/jobs/exb/kill?"}
	if len(f.kills) != len(want) {
		t.Fatalf("kills = %v, want %v", f.kills, want)
	}
	for i := range want {
		if f.kills[i] != want[i] {
			t.Fatalf("kills = %v, want %v", f.kills, want)
		}
	}
}
