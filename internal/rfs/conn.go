package rfs

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// ── Config ──────────────────────────────────────────────────────────────────

// SSHConfig holds the parameters needed to establish an SSH connection.
// Populated from the target config (see internal/config.TargetConfig).
type SSHConfig struct {
	Host string // "login.cluster.edu" or "login.cluster.edu:22"
	User string
	// AuthMethods are tried in order. Convention (RQ-45): ssh-agent first
	// (the daemon never touches key plaintext), key file second. Build
	// with ResolveAuthMethods.
	AuthMethods []ssh.AuthMethod
	// HostKeyCallback: nil = known_hosts verification per HostKeyPolicy
	// (the daemon default). `runq connect` passes a TOFU callback with a
	// human on the other end. InsecureIgnoreHostKey never appears in this
	// codebase.
	HostKeyCallback ssh.HostKeyCallback

	// HostKeyPolicy applies when HostKeyCallback is nil: HostKeyStrict
	// (zero value) or HostKeyAcceptNew — the ssh_config
	// `StrictHostKeyChecking accept-new` passthrough (RQ-74). Resolve with
	// ResolveHostKeyPolicy against the target's ssh alias.
	HostKeyPolicy string

	// IdleTimeout controls how long a connection survives with no in-flight
	// operations before sshConn tears it down. Zero means never idle-close.
	// Callers that want the "look like a normal SSH user" behavior MUST set
	// this (the daemon does; see backend.NewSSHBackend).
	IdleTimeout time.Duration

	// MaxSessions caps the number of concurrent SSH sessions (Exec calls).
	// sftp runs on its own subsystem and does NOT consume a session slot.
	// Default: 3.
	MaxSessions int

	// OperationTimeout bounds connection setup and each file operation.
	OperationTimeout time.Duration
}

// addr returns Host with ":22" appended if no port is present.
func (c SSHConfig) addr() string {
	if _, _, err := net.SplitHostPort(c.Host); err != nil {
		return c.Host + ":22"
	}
	return c.Host
}

// livenessCheckAfter: on reuse, probe the connection only when it has been
// quiet for at least this long — recent successful traffic implies healthy,
// and probing every call would add a round trip to every operation.
const livenessCheckAfter = 30 * time.Second

// livenessTimeout bounds the keepalive probe. A half-open TCP connection
// (network partition) can block SendRequest for minutes; we declare the
// connection dead after this and close its transport before redialing.
const livenessTimeout = 5 * time.Second

// errClosed is returned for operations on a Close()d connection manager.
var errClosed = errors.New("ssh connection manager closed")

// ── Connection manager ──────────────────────────────────────────────────────

// sshConn is a lazy, reconnecting SSH connection manager.
//
// Design invariants:
//   - ONE persistent connection: dial on first use, reuse while healthy.
//   - Liveness: a reused connection that has been quiet is probed with a
//     keepalive; a dead one is torn down and redialed transparently.
//   - sftp.Client rides on the same connection (its own SSH subsystem).
//   - Session concurrency bounded by sessionSem (cap = MaxSessions);
//     acquisition respects ctx cancellation and manager shutdown.
//   - Idle teardown: after IdleTimeout with ZERO in-flight operations
//     (active == 0), the watcher closes the connection. In-flight streams
//     (logs -f, large SFTP copies) hold active > 0 and are never cut.
type sshConn struct {
	mu         sync.Mutex
	connecting chan struct{}
	dialCancel context.CancelFunc
	client     *ssh.Client
	sftp       *sftp.Client
	lastUsed   time.Time
	active     int // in-flight operations; idle teardown requires active == 0

	cfg        SSHConfig
	sessionSem chan struct{} // cap = MaxSessions

	closeOnce sync.Once
	watchOnce sync.Once
	done      chan struct{} // closed by Close() to stop idle watcher + waiters
}

// newSSHConn creates a connection manager. Does NOT dial.
func newSSHConn(cfg SSHConfig) *sshConn {
	maxCnt := cfg.MaxSessions
	if maxCnt <= 0 {
		maxCnt = 3
	}
	return &sshConn{
		cfg:        cfg,
		lastUsed:   time.Now(),
		sessionSem: make(chan struct{}, maxCnt),
		done:       make(chan struct{}),
	}
}

// ── Operation tracking ──────────────────────────────────────────────────────

// beginOp marks an operation in flight. Every public SSHFS operation is
// bracketed by beginOp/endOpErr; for handle-returning operations (Open,
// ExecStream) the bracket extends to the handle's Close, so the idle watcher
// can never cut a live stream.
//
// Deliberately does NOT touch lastUsed: lastUsed means "last moment the
// transport PROVED itself alive", and merely attempting an operation proves
// nothing. If beginOp refreshed it, a stream of failing calls against a dead
// connection would keep resetting the quiet-time clock and the keepalive
// probe in getConn would never fire — the connection could never self-heal.
func (c *sshConn) beginOp() {
	c.mu.Lock()
	c.active++
	c.mu.Unlock()
}

// endOp is endOpErr for paths without an error to classify.
func (c *sshConn) endOp() { c.endOpErr(nil) }

// endOpErr marks an operation finished. lastUsed is refreshed only when the
// outcome proves the transport worked: success, or a REMOTE verdict like
// not-exist/permission (the server answered — that IS a live round trip).
// Transport-level failures leave lastUsed alone, letting quiet time
// accumulate until getConn's keepalive probe fires and heals the connection.
func (c *sshConn) endOpErr(err error) {
	c.mu.Lock()
	c.active--
	if err == nil || errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		c.lastUsed = time.Now()
	}
	c.mu.Unlock()
}

// ── Connection acquisition ──────────────────────────────────────────────────

// getConn returns a live *ssh.Client, dialing or redialing as needed.
// Thread-safe. On reuse after a quiet period, the connection's liveness is
// verified first — a dead connection is torn down and redialed instead of
// poisoning every future call.
func (c *sshConn) timeout() time.Duration {
	if c.cfg.OperationTimeout > 0 {
		return c.cfg.OperationTimeout
	}
	return 10 * time.Second
}

// Network I/O never holds mu. A cancelled setup closes the actual transport;
// waiters can cancel independently without abandoning a handshake goroutine.
func (c *sshConn) getConn() (*ssh.Client, error) { return c.getConnContext(context.Background()) }
func (c *sshConn) getConnContext(parent context.Context) (*ssh.Client, error) {
	ctx, cancel := context.WithTimeout(parent, c.timeout())
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		select {
		case <-c.done:
			c.mu.Unlock()
			return nil, errClosed
		default:
		}
		if c.client != nil {
			client, quiet := c.client, time.Since(c.lastUsed) > livenessCheckAfter
			c.mu.Unlock()
			if quiet {
				probe, stop := context.WithTimeout(ctx, livenessTimeout)
				_, err := sshCall(probe, client, func() (bool, error) { _, _, e := client.SendRequest("keepalive@runq", true, nil); return true, e })
				stop()
				if err != nil {
					c.invalidate(client)
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					continue
				}
				c.mu.Lock()
				if c.client == client {
					c.lastUsed = time.Now()
				}
				c.mu.Unlock()
			}
			return client, nil
		}
		if wait := c.connecting; wait != nil {
			c.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-c.done:
				return nil, errClosed
			}
		}
		wait := make(chan struct{})
		c.connecting = wait
		dialCtx, stop := context.WithCancel(ctx)
		c.dialCancel = stop
		c.mu.Unlock()
		client, err := dialSSH(dialCtx, c.cfg)
		var sc *sftp.Client
		if err == nil {
			sc, err = sshCall(dialCtx, client, func() (*sftp.Client, error) { return sftp.NewClient(client, sftp.UseConcurrentWrites(false)) })
		}
		stop()
		c.mu.Lock()
		select {
		case <-c.done:
			if err == nil {
				err = errClosed
			}
		default:
		}
		if err == nil {
			c.client = client
			c.sftp = sc
			c.lastUsed = time.Now()
			c.startIdleWatcher()
		}
		c.dialCancel = nil
		c.connecting = nil
		close(wait)
		c.mu.Unlock()
		if err != nil && client != nil {
			_ = client.Close()
		}
		return client, err
	}
}

func (c *sshConn) invalidate(client *ssh.Client) {
	c.mu.Lock()
	if c.client == client {
		c.client = nil
		c.sftp = nil
	}
	c.mu.Unlock()
	_ = client.Close()
}
func (c *sshConn) getSFTPContext(ctx context.Context) (*ssh.Client, *sftp.Client, error) {
	client, err := c.getConnContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != client || c.sftp == nil {
		return nil, nil, errClosed
	}
	return client, c.sftp, nil
}
func (c *sshConn) newSession(parent context.Context) (sess *ssh.Session, client *ssh.Client, release func(), err error) {
	ctx, cancel := context.WithTimeout(parent, c.timeout())
	defer cancel()
	select {
	case c.sessionSem <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, nil, ctx.Err()
	case <-c.done:
		return nil, nil, nil, errClosed
	}
	var once sync.Once
	release = func() { once.Do(func() { <-c.sessionSem }) }
	client, err = c.getConnContext(ctx)
	if err != nil {
		release()
		return nil, nil, nil, err
	}
	setup, stop := context.WithTimeout(ctx, c.timeout())
	defer stop()
	sess, err = sshCall(setup, client, func() (*ssh.Session, error) { return client.NewSession() })
	if err != nil {
		var rejection *ssh.OpenChannelError
		if setup.Err() != nil || !errors.As(err, &rejection) {
			c.invalidate(client)
		}
		release()
		return nil, nil, nil, err
	}
	return sess, client, release, nil
}

// ── Idle watcher ────────────────────────────────────────────────────────────

// startIdleWatcher launches the goroutine that tears down the connection
// after IdleTimeout with no in-flight operations. Must be called with mu
// held. Only the first call spawns; the watcher survives teardown/redial
// cycles. No-op when IdleTimeout <= 0.
func (c *sshConn) startIdleWatcher() {
	if c.cfg.IdleTimeout <= 0 {
		return
	}
	c.watchOnce.Do(func() {
		ticker := time.NewTicker(max(c.cfg.IdleTimeout/2, time.Millisecond))
		go func() {
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					c.mu.Lock()
					if c.client != nil && c.active == 0 && time.Since(c.lastUsed) > c.cfg.IdleTimeout {
						c.teardownLocked()
					}
					c.mu.Unlock()
				case <-c.done:
					return
				}
			}
		}()
	})
}

// teardownLocked closes sftp then client, nils both. Must be called with mu
// held. Order matters: sftp rides on the ssh conn. Best-effort cleanup.
func (c *sshConn) teardownLocked() {
	// Close SSH first: it releases outstanding SFTP requests, including Close.
	if c.client != nil {
		_ = c.client.Close()
	}
	c.client = nil
	c.sftp = nil
}

// ── Shutdown ────────────────────────────────────────────────────────────────

// Close stops the idle watcher, unblocks session waiters, and tears down the
// connection. Safe to call multiple times.
func (c *sshConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.mu.Lock()
		if c.dialCancel != nil {
			c.dialCancel()
		}
		c.teardownLocked()
		c.mu.Unlock()
	})
	return nil
}

// CloseTransport cancels current I/O while allowing a later operation to reconnect.
func (c *sshConn) CloseTransport() {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client != nil {
		c.invalidate(client)
	}
}
