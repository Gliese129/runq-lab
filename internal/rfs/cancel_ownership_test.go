package rfs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

// One caller's deadline ends that caller's work. The transport, and everyone
// else's work on it, carries on: the daemon's 30s background passes must not be
// able to break a submission that happens to be running at the same moment.

func bothTransports(t *testing.T, run func(*testing.T, *faultSSH, *SSHFS)) {
	t.Helper()
	for _, system := range []bool{false, true} {
		name := "native"
		if system {
			name = "openssh"
		}
		t.Run(name, func(t *testing.T) {
			f := newFaultSSH(t)
			cfg := f.cfg
			if system {
				cfg = systemTestConfig(t, f)
			}
			cfg.OperationTimeout = 2 * time.Second
			fs := NewSSHFS(cfg)
			t.Cleanup(func() { _ = fs.Close() })
			run(t, f, fs)
		})
	}
}

func currentTransport(fs *SSHFS) *sshTransport {
	fs.conn.mu.Lock()
	defer fs.conn.mu.Unlock()
	return fs.conn.client
}

func TestExecDeadlineSparesOtherSessions(t *testing.T) {
	bothTransports(t, func(t *testing.T, _ *faultSSH, fs *SSHFS) {
		if _, err := fs.Home(); err != nil {
			t.Fatal(err)
		}
		transport := currentTransport(fs)
		sibling := make(chan error, 1)
		go func() {
			_, _, code, err := fs.Exec(context.Background(), testSlowCommand)
			if err == nil && code != 0 {
				err = errors.New("sibling exit code changed")
			}
			sibling <- err
		}()
		time.Sleep(50 * time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		started := time.Now()
		_, _, _, err := fs.Exec(ctx, testHangCommand)
		if !errors.Is(err, ErrExecInterrupted) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("own deadline must read as interrupted and expired: %v", err)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("deadline took %v to settle", elapsed)
		}
		if err := <-sibling; err != nil {
			t.Fatalf("another caller's deadline broke this command: %v", err)
		}
		if currentTransport(fs) != transport {
			t.Fatal("transport was replaced")
		}
		if _, _, code, err := fs.Exec(context.Background(), "exit 7"); err != nil || code != 7 {
			t.Fatalf("transport unusable afterwards: code=%d err=%v", code, err)
		}
	})
}

func TestSFTPDeadlineSparesOtherRequests(t *testing.T) {
	bothTransports(t, func(t *testing.T, _ *faultSSH, fs *SSHFS) {
		if _, err := fs.Home(); err != nil {
			t.Fatal(err)
		}
		transport := currentTransport(fs)
		fs.conn.mu.Lock()
		first := fs.conn.sftp
		fs.conn.mu.Unlock()

		entered, proceed := make(chan struct{}), make(chan struct{})
		sibling := make(chan error, 1)
		go func() {
			_, err := sftpOpContext(fs, context.Background(), func(sc *sftp.Client) (string, error) {
				close(entered)
				<-proceed
				return sc.Getwd()
			})
			sibling <- err
		}()
		<-entered

		stuck := make(chan struct{})
		defer close(stuck)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err := sftpOpContext(fs, ctx, func(*sftp.Client) (bool, error) { <-stuck; return true, nil })
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("own deadline: %v", err)
		}

		// New work moves to a fresh channel at once, on the same transport.
		if _, err := fs.Home(); err != nil {
			t.Fatalf("next request failed: %v", err)
		}
		fs.conn.mu.Lock()
		second := fs.conn.sftp
		firstClosed := first.closed
		fs.conn.mu.Unlock()
		if second == first || firstClosed {
			t.Fatalf("expected a fresh channel beside the retired one: closed=%v", firstClosed)
		}

		// The request already in flight still gets its answer...
		close(proceed)
		if err := <-sibling; err != nil {
			t.Fatalf("another caller's deadline broke this request: %v", err)
		}
		if currentTransport(fs) != transport {
			t.Fatal("transport was replaced")
		}
		// ...and only then does the retired channel close.
		fs.conn.mu.Lock()
		firstClosed = first.closed
		fs.conn.mu.Unlock()
		if !firstClosed {
			t.Fatal("drained channel stayed open")
		}
	})
}

// A hung transport is still detected: work that cannot even be aborted is the
// evidence that lets the manager close the connection.
func TestUnacknowledgedCancelClosesTransport(t *testing.T) {
	bothTransports(t, func(t *testing.T, _ *faultSSH, fs *SSHFS) {
		if _, err := fs.Home(); err != nil {
			t.Fatal(err)
		}
		transport := currentTransport(fs)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err := ownCall(ctx, fs.conn, transport, nil, func() (bool, error) { return false, transport.Wait() })
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("own deadline: %v", err)
		}
		if currentTransport(fs) != nil {
			t.Fatal("unresponsive transport stayed published")
		}
		if _, err := fs.Home(); err != nil {
			t.Fatalf("did not reconnect: %v", err)
		}
	})
}

// A dead transport shows up as a lost SFTP channel. Finding that out must lead
// to a reconnect, not a stream of failures against the same dead master.
func TestLostChannelOnDeadTransportReconnects(t *testing.T) {
	bothTransports(t, func(t *testing.T, _ *faultSSH, fs *SSHFS) {
		if _, err := fs.Home(); err != nil {
			t.Fatal(err)
		}
		dead := currentTransport(fs)
		_ = dead.Close()
		_, _ = fs.Home() // may report the loss
		if _, err := fs.Home(); err != nil {
			t.Fatalf("no recovery after losing the transport: %v", err)
		}
		if currentTransport(fs) == dead {
			t.Fatal("dead transport stayed published")
		}
	})
}

func TestChannelCloseEndsRequestsLeftBehind(t *testing.T) {
	f := newFaultSSH(t)
	f.mode.Store(faultFile)
	fs := NewSSHFS(f.cfg)
	defer fs.Close()
	if _, err := fs.conn.getConn(); err != nil {
		t.Fatal(err)
	}
	fs.conn.mu.Lock()
	ch := fs.conn.sftp
	fs.conn.mu.Unlock()
	ended := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := sftpOpContext(fs, ctx, func(sc *sftp.Client) (string, error) {
		defer close(ended)
		return sc.Getwd() // never answered: the server froze
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("own deadline: %v", err)
	}
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("request left behind outlived its channel")
	}
	fs.conn.mu.Lock()
	closed := ch.closed
	fs.conn.mu.Unlock()
	if !closed {
		t.Fatal("channel not closed")
	}
}
