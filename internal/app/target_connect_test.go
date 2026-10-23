package app

import (
	"context"
	"errors"
	"testing"

	"github.com/gliese129/runq-lab/internal/rfs"
)

type connectionProbeFS struct {
	*rfs.LocalFS
	calls int
	err   error
}

func (f *connectionProbeFS) Exec(ctx context.Context, cmd string, args ...string) ([]byte, []byte, int, error) {
	f.calls++
	if _, ok := ctx.Deadline(); !ok {
		panic("connection check must be bounded")
	}
	if cmd != "true" || len(args) != 0 {
		panic("connection check must not mutate remote state")
	}
	return nil, nil, 0, f.err
}

type connectionProbeLane struct {
	*fakeLane
	fs rfs.FS
}

func (l *connectionProbeLane) FS() rfs.FS { return l.fs }

func TestConnectTargetWithoutRemoteCLI(t *testing.T) {
	dir := t.TempDir()
	writeCfg(t, dir, baseCfg+"    ssh:\n      host: test-alias\n")
	d, _, _ := newReconcilerHarness(t, dir)
	fsys := &connectionProbeFS{LocalFS: rfs.NewLocalFS()}
	d.multiBe.SetTarget("a", &connectionProbeLane{fakeLane: d.lanes["a"].(*fakeLane), fs: fsys})
	if err := d.ConnectTarget(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if fsys.calls != 1 || len(d.forwards) != 0 {
		t.Fatalf("calls=%d forwards=%d", fsys.calls, len(d.forwards))
	}
	fsys.err = errors.New("injected SSH failure")
	if err := d.ConnectTarget(context.Background(), "a"); !errors.Is(err, fsys.err) {
		t.Fatalf("failure lost: %v", err)
	}
}
