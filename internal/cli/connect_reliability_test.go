package cli

import (
	"context"
	"errors"
	"github.com/gliese129/runq-lab/internal/version"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
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

type installerFixture struct {
	files             map[string][]byte
	modes             map[string]os.FileMode
	uploadErr         error
	cancel            context.CancelFunc
	exit              int
	verifyErr         error
	renameErr         error
	cleanupContextErr error
	cleanupBound      time.Duration
	removed           []string
}

func (f *installerFixture) WriteFileContext(ctx context.Context, path string, data []byte, mode os.FileMode) error {
	f.files[path] = append([]byte(nil), data[:min(len(data), 2)]...)
	f.modes[path] = mode
	if f.cancel != nil {
		f.cancel()
		return ctx.Err()
	}
	return f.uploadErr
}
func (f *installerFixture) Exec(context.Context, string, ...string) ([]byte, []byte, int, error) {
	return []byte(version.Version), nil, f.exit, f.verifyErr
}
func (f *installerFixture) Rename(from, to string) error {
	if f.renameErr != nil {
		return f.renameErr
	}
	f.files[to] = f.files[from]
	f.modes[to] = f.modes[from]
	delete(f.files, from)
	delete(f.modes, from)
	return nil
}
func (f *installerFixture) RemoveContext(ctx context.Context, path string) error {
	f.cleanupContextErr = ctx.Err()
	deadline, ok := ctx.Deadline()
	if ok {
		f.cleanupBound = time.Until(deadline)
	}
	f.removed = append(f.removed, path)
	delete(f.files, path)
	delete(f.modes, path)
	return nil
}
func TestFailedInstallCleansOwnedTempAndPreservesBinary(t *testing.T) {
	for _, phase := range []string{"upload", "cancel", "verify", "rename"} {
		t.Run(phase, func(t *testing.T) {
			binary := "/remote/bin/runq"
			other := binary + ".tmp-other-session"
			f := &installerFixture{files: map[string][]byte{binary: []byte("old executable"), other: []byte("other upload")}, modes: map[string]os.FileMode{binary: 0750, other: 0600}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch phase {
			case "upload":
				f.uploadErr = errors.New("injected write failure")
			case "cancel":
				f.cancel = cancel
			case "verify":
				f.exit = 126
			case "rename":
				f.renameErr = errors.New("injected rename failure")
			}
			err := installRemoteBinaryData(ctx, f, binary, []byte("new binary"))
			if err == nil {
				t.Fatal("failed install returned success")
			}
			if phase == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if string(f.files[binary]) != "old executable" || f.modes[binary] != 0750 {
				t.Fatal("existing executable changed")
			}
			if string(f.files[other]) != "other upload" || f.modes[other] != 0600 {
				t.Fatal("another session's temp removed")
			}
			if len(f.files) != 2 || len(f.removed) != 1 || !strings.HasPrefix(f.removed[0], binary+".tmp-") || f.removed[0] == other {
				t.Fatalf("owned temp survived: %+v", f.files)
			}
			if f.cleanupContextErr != nil || f.cleanupBound <= 0 || f.cleanupBound > 3*time.Second {
				t.Fatalf("cleanup reused cancelled/unbounded context: %v %v", f.cleanupContextErr, f.cleanupBound)
			}
		})
	}
}
