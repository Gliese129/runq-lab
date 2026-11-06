package remote

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/rfs"
)

// observedFS is a filesystem that reports its own transport outcomes and fails
// every call the paths under test make.
type observedFS struct {
	*rfs.LocalFS
	observe func(uint64, error)
	err     error
}

func (f *observedFS) SetContactObserver(observe func(uint64, error)) { f.observe = observe }
func (f *observedFS) ReadDir(string) ([]fs.DirEntry, error)          { return nil, f.err }
func (f *observedFS) Exec(context.Context, string, ...string) ([]byte, []byte, int, error) {
	return nil, nil, -1, f.err
}

func newObservedBackend(fsys rfs.FS) *Backend {
	return NewWithFS(&config.TargetConfig{Name: "lab", DoneDir: "/remote/.runq-done"}, nil, nil, fsys)
}

func TestOlderGenerationCannotOverwriteContact(t *testing.T) {
	fsys := &observedFS{LocalFS: rfs.NewLocalFS()}
	b := newObservedBackend(fsys)
	if fsys.observe == nil {
		t.Fatal("observer not installed")
	}
	reachable := func() bool { _, ok, _ := b.LastContact(); return ok }

	fsys.observe(2, nil)
	if !reachable() {
		t.Fatal("recovery not applied")
	}
	fsys.observe(1, errors.New("late failure from the old connection"))
	if !reachable() {
		t.Fatal("old generation overwrote the newer connection's success")
	}
	fsys.observe(2, errors.New("the current connection failed"))
	if reachable() {
		t.Fatal("failure on the current connection dropped")
	}
	fsys.observe(1, nil)
	if reachable() {
		t.Fatal("old generation's success overwrote the newer connection's failure")
	}
	fsys.observe(3, nil)
	if !reachable() {
		t.Fatal("newer generation not applied")
	}
	// A proof from outside the transport has no generation and still counts.
	fsys.observe(3, errors.New("failure"))
	b.RecordContactOK()
	if !reachable() {
		t.Fatal("daemon-observed proof dropped")
	}
}

// The filesystem has already classified each outcome, and a local wait or a
// replaced connection may be deliberately unreported. Callers of the filesystem
// must not publish those errors behind its back.
func TestCallersOfObservedFSDoNotRecordContact(t *testing.T) {
	wait := context.DeadlineExceeded
	for name, call := range map[string]func(*Backend){
		"shell":   func(b *Backend) { _, _, _ = b.shellRunClassified(context.Background(), "true") },
		"markers": func(b *Backend) { _ = b.ScanDoneMarkers(context.Background()) },
	} {
		t.Run(name, func(t *testing.T) {
			b := newObservedBackend(&observedFS{LocalFS: rfs.NewLocalFS(), err: wait})
			call(b)
			if at, _, lastErr := b.LastContact(); !at.IsZero() || lastErr != "" {
				t.Fatalf("recorded behind the filesystem's back: at=%v err=%q", at, lastErr)
			}
		})
	}
	t.Run("absent marker dir", func(t *testing.T) {
		b := newObservedBackend(&observedFS{LocalFS: rfs.NewLocalFS(), err: fs.ErrNotExist})
		if err := b.ScanDoneMarkers(context.Background()); err != nil {
			t.Fatal(err)
		}
		if at, _, _ := b.LastContact(); !at.IsZero() {
			t.Fatalf("recorded behind the filesystem's back: at=%v", at)
		}
	})
}

// A filesystem that reports nothing itself keeps its callers' records.
func TestCallersOfUnobservedFSRecordContact(t *testing.T) {
	want := errors.New("injected marker transport failure")
	b := newObservedBackend(&readDirErrorFS{LocalFS: rfs.NewLocalFS(), err: want})
	if err := b.ScanDoneMarkers(context.Background()); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if at, ok, lastErr := b.LastContact(); at.IsZero() || ok || lastErr != want.Error() {
		t.Fatalf("contact: at=%v ok=%v err=%q", at, ok, lastErr)
	}
}
