package rfs

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// contactLog records what the observer was told.
type contactLog struct {
	mu   sync.Mutex
	seen []contactOutcome
}

type contactOutcome struct {
	generation uint64
	err        error
}

func (l *contactLog) observe(generation uint64, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, contactOutcome{generation, err})
}

func (l *contactLog) all() []contactOutcome {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]contactOutcome(nil), l.seen...)
}

// Waiting for a local session slot reaches nothing, so it cannot change what is
// known about the target.
func TestLocalSessionWaitIsNotAContactObservation(t *testing.T) {
	bothTransports(t, func(t *testing.T, f *faultSSH, fs *SSHFS) {
		if _, err := fs.Home(); err != nil {
			t.Fatal(err)
		}
		transport := currentTransport(fs)
		var releases []func()
		for range cap(fs.conn.sessionSem) {
			session, _, release, err := fs.conn.newSession(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			releases = append(releases, func() { _ = session.Close(); release() })
		}
		var log contactLog
		fs.SetContactObserver(log.observe)
		before := f.execRequests.Load()

		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		defer cancel()
		if _, _, _, err := fs.Exec(ctx, "true"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Exec must still report the caller's deadline: %v", err)
		}
		if _, err := fs.ExecStream(ctx, "true"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("ExecStream must still report the caller's deadline: %v", err)
		}
		if got := log.all(); len(got) != 0 {
			t.Fatalf("a local wait was reported as contact: %+v", got)
		}
		if f.execRequests.Load() != before {
			t.Fatal("a queued command reached the target")
		}
		if currentTransport(fs) != transport {
			t.Fatal("a local wait replaced the transport")
		}

		// A free slot and a spent deadline race in select; both outcomes are local.
		releases[0]()
		for range 32 {
			if _, _, _, err := fs.Exec(ctx, "true"); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("spent deadline: %v", err)
			}
		}
		if got := log.all(); len(got) != 0 {
			t.Fatalf("a spent deadline was reported as contact: %+v", got)
		}

		// Real contact still reports.
		for _, release := range releases[1:] {
			release()
		}
		if _, _, _, err := fs.Exec(context.Background(), "true"); err != nil {
			t.Fatal(err)
		}
		if got := log.all(); len(got) != 1 || got[0].err != nil || got[0].generation != transport.generation {
			t.Fatalf("real contact after the queue drained: %+v", got)
		}
	})
}

// An outcome carries the generation of the connection it speaks for, so one
// delayed past a reconnect can be told from the newer connection's.
func TestDelayedOutcomeCarriesItsOldGeneration(t *testing.T) {
	bothTransports(t, func(t *testing.T, f *faultSSH, fs *SSHFS) {
		f.mode.Store(faultFile)
		old, err := fs.conn.getConn()
		if err != nil {
			t.Fatal(err)
		}
		arrived, release, oldDone := make(chan contactOutcome, 1), make(chan struct{}), make(chan error, 1)
		var parked atomic.Bool
		var log contactLog
		fs.SetContactObserver(func(generation uint64, err error) {
			// The callback for the old failure is held back past the recovery.
			if parked.CompareAndSwap(false, true) {
				arrived <- contactOutcome{generation, err}
				<-release
				return
			}
			log.observe(generation, err)
		})
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		defer cancel()
		go func() { _, err := fs.WithContext(ctx).Home(); oldDone <- err }()
		var late contactOutcome
		select {
		case late = <-arrived:
		case <-time.After(time.Second):
			t.Fatal("old observation did not arrive")
		}
		if !errors.Is(late.err, context.DeadlineExceeded) || late.generation != old.generation {
			t.Fatalf("old failure: %+v, want deadline on generation %d", late, old.generation)
		}

		fs.conn.CloseTransport()
		f.mode.Store(0)
		if _, err := fs.Home(); err != nil {
			t.Fatalf("recovery: %v", err)
		}
		current := currentTransport(fs)
		if current == old || current.generation <= old.generation {
			t.Fatalf("recovery generation %d, old %d", current.generation, old.generation)
		}
		got := log.all()
		if len(got) != 1 || got[0].err != nil || got[0].generation != current.generation {
			t.Fatalf("recovery outcome: %+v, want success on generation %d", got, current.generation)
		}
		close(release)
		if err := <-oldDone; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	})
}

// An operation that ends after its connection was replaced publishes nothing,
// and its success does not vouch for the new connection either.
func TestOutcomeOfReplacedConnectionIsNotPublished(t *testing.T) {
	bothTransports(t, func(t *testing.T, _ *faultSSH, fs *SSHFS) {
		if _, err := fs.Home(); err != nil {
			t.Fatal(err)
		}
		failing, succeeding := fs.conn.beginOp(), fs.conn.beginOp()
		failing.usedTransport(currentTransport(fs))
		succeeding.usedTransport(currentTransport(fs))
		fs.conn.CloseTransport()
		if _, err := fs.Home(); err != nil {
			t.Fatal(err)
		}
		var log contactLog
		fs.SetContactObserver(log.observe)
		quiet := time.Now().Add(-time.Hour)
		fs.conn.mu.Lock()
		fs.conn.lastUsed = quiet
		fs.conn.mu.Unlock()

		fs.conn.endOpErr(failing, errors.New("late failure"))
		fs.conn.endOpErr(succeeding, nil)

		if got := log.all(); len(got) != 0 {
			t.Fatalf("replaced connection published: %+v", got)
		}
		fs.conn.mu.Lock()
		lastUsed, active := fs.conn.lastUsed, fs.conn.active
		fs.conn.mu.Unlock()
		if !lastUsed.Equal(quiet) {
			t.Fatal("old connection's success vouched for the new transport")
		}
		if active != 0 {
			t.Fatalf("operations still counted in flight: %d", active)
		}
	})
}

// An operation that began before a reconnect and then ran on the new connection
// speaks for it, including when it fails.
func TestOutcomeOnReconnectedTransportIsPublished(t *testing.T) {
	bothTransports(t, func(t *testing.T, f *faultSSH, fs *SSHFS) {
		if _, err := fs.Home(); err != nil {
			t.Fatal(err)
		}
		var log contactLog
		fs.SetContactObserver(log.observe)

		fs.conn.CloseTransport()
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		if _, _, _, err := fs.Exec(ctx, testHangCommand); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Exec: %v", err)
		}
		got := log.all()
		if len(got) != 1 || got[0].err == nil || got[0].generation != currentTransport(fs).generation {
			t.Fatalf("Exec on the new transport: %+v, want a failure on generation %d", got, currentTransport(fs).generation)
		}

		fs.conn.CloseTransport()
		f.mode.Store(faultFile)
		fileCtx, cancelFile := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancelFile()
		if _, err := fs.WithContext(fileCtx).Home(); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Home: %v", err)
		}
		got = log.all()
		if len(got) != 2 || got[1].err == nil || got[1].generation != currentTransport(fs).generation {
			t.Fatalf("file call on the new transport: %+v, want a failure on generation %d", got, currentTransport(fs).generation)
		}
	})
}
