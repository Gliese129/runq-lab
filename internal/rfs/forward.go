package rfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// ── Remote socket forward (RQ remote CLI) ───────────────────────────────────
//
// RemoteForward keeps a unix socket listening on a remote host (the HPC
// login node) and hands every accepted connection to a local server — the
// reverse of `ssh -R`, in-process. A `runq` CLI on the login node dials
// that socket and transparently talks to THIS machine's client daemon.
//
// It deliberately does NOT ride on sshConn: sshConn's contract is "look
// like a normal SSH user" (idle teardown after quiet periods), while a
// forward listener is a persistent service — holding the lane's connection
// open forever would defeat that etiquette. So the forward owns a second,
// dedicated connection with keepalives instead of idle teardown, and its
// own reconnect supervision.
//
// Security: the remote socket grants full control of the local daemon
// (which executes submit templates). The login node is multi-user, so the
// socket lives in a 0700 dir and is chmod'd 0600 after bind.

// RemoteForwardConfig configures a RemoteForward.
type RemoteForwardConfig struct {
	// SSH is the connection config. IdleTimeout is ignored — a forward
	// listener is persistent by definition (see package comment above).
	SSH SSHConfig

	// SocketPath is the remote unix socket path. Relative paths resolve
	// against the remote user's home (sftp session start dir).
	// Default: ".runq/runq.sock".
	SocketPath string
	Owner      string

	// Serve consumes the remote listener and blocks until the listener
	// dies (the daemon passes http.Serve with the dashboard mux). It is
	// called once per established forward; a non-nil return is logged and
	// triggers a reconnect cycle.
	Serve func(net.Listener) error

	// OnEstablished, when set, is called once per established forward
	// session, right before Serve — the moment the remote socket is bound
	// and listening. The daemon uses it to record target reachability
	// (RQ-74): a forward coming up is transport-level proof of contact, so
	// doctor's "no contact yet" clears without waiting for the next
	// scheduled sensor pass. Must be fast and non-blocking.
	OnEstablished func()

	Logger *slog.Logger
}

// Forward reconnect/keepalive tuning. Backoff max and keepalive interval
// were tightened from 60s/30s (RQ-74): after a laptop wakes from sleep the
// forward should be back within seconds, not "keepalive lag + a minute of
// backoff". Keepalive detection is bounded by interval + liveness timeout;
// recovery additionally includes backoff, authentication, and bind. Measure
// end-to-end recovery rather than deriving a guarantee from these constants.
const (
	forwardBackoffMin   = 2 * time.Second
	forwardBackoffMax   = 10 * time.Second
	forwardHealthyAfter = 2 * time.Minute // session older than this resets backoff
	forwardKeepalive    = 15 * time.Second
)

// Forward state labels (ForwardStatus.State).
const (
	ForwardUp           = "up"           // session established, socket bound
	ForwardReconnecting = "reconnecting" // between sessions (includes pre-first-dial)
	ForwardClosed       = "closed"       // Close() called — no further attempts
)

// ForwardStatus is one forward's observable state (RQ-74): what /health
// exposes and doctor renders ("↻ reconnecting · attempt 3 · last online
// 12m ago"). All timestamps unix seconds.
type ForwardStatus struct {
	SocketPath string `json:"socket_path,omitempty"`
	Owner      string `json:"owner,omitempty"`
	State      string `json:"state"` // up | reconnecting | closed
	Since      int64  `json:"since"` // when the current state was entered
	// LastOnline is the last moment the forward was known to be serving:
	// set on establish and refreshed when a live session drops. 0 = never
	// online ("no contact yet" ≠ "went down" — doctor renders them apart).
	LastOnline int64 `json:"last_online,omitempty"`
	// Attempts counts consecutive failed sessions since the last up.
	Attempts  int    `json:"attempts,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

// RemoteForward supervises one remote socket forward. Create with
// NewRemoteForward, drive with Run (blocking; run it in a goroutine),
// stop with Close.
type RemoteForward struct {
	cfg    RemoteForwardConfig
	logger *slog.Logger

	mu     sync.Mutex
	client *ssh.Client // current connection; nil between sessions

	// stMu guards st — the observable status snapshot (RQ-74).
	stMu sync.Mutex
	st   ForwardStatus

	closeOnce sync.Once
	done      chan struct{}
	runMu     sync.Mutex
	runCancel context.CancelFunc
	runDone   chan struct{}
	running   bool
}

// Status returns a snapshot of the forward's observable state.
func (f *RemoteForward) Status() ForwardStatus {
	f.stMu.Lock()
	defer f.stMu.Unlock()
	return f.st
}

// markUp records an established session: attempts reset, error cleared.
func (f *RemoteForward) markUp() {
	f.stMu.Lock()
	defer f.stMu.Unlock()
	if f.st.State == ForwardClosed {
		return
	}
	now := time.Now().Unix()
	f.st = ForwardStatus{State: ForwardUp, Since: now, LastOnline: now, SocketPath: f.cfg.SocketPath, Owner: f.cfg.Owner}
}

// markDown records a session ending. err may be nil (orderly serve exit);
// the attempt counter still advances — the forward is not serving either
// way, and the next successful session resets it.
func (f *RemoteForward) markDown(err error) {
	f.stMu.Lock()
	defer f.stMu.Unlock()
	if f.st.State == ForwardClosed {
		return
	}
	if f.st.State == ForwardUp {
		// It WAS online until this very moment — that fact is the honest
		// "last online" reference for doctor.
		f.st.LastOnline = time.Now().Unix()
	}
	f.st.State = ForwardReconnecting
	f.st.Since = time.Now().Unix()
	f.st.Attempts++
	if err != nil {
		f.st.LastError = err.Error()
	}
}

// markClosed pins the terminal state (Close called).
func (f *RemoteForward) markClosed() {
	f.stMu.Lock()
	defer f.stMu.Unlock()
	f.st.State = ForwardClosed
	f.st.Since = time.Now().Unix()
}

// NewRemoteForward creates a RemoteForward. No network traffic until Run.
func NewRemoteForward(cfg RemoteForwardConfig) *RemoteForward {
	if cfg.SocketPath == "" {
		cfg.SocketPath = ".runq/runq.sock"
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &RemoteForward{cfg: cfg, logger: logger, done: make(chan struct{}), runDone: make(chan struct{}), st: ForwardStatus{State: ForwardReconnecting, Since: time.Now().Unix(), SocketPath: cfg.SocketPath, Owner: cfg.Owner}}
}

// Run supervises the forward until ctx is cancelled or Close is called:
// dial → bind remote socket → Serve, then backoff and repeat. Failures are
// logged, never fatal — the HPC lane works with or without the forward.
func (f *RemoteForward) Run(parent context.Context) {
	f.runMu.Lock()
	if f.running {
		f.runMu.Unlock()
		return
	}
	select {
	case <-f.done:
		f.runMu.Unlock()
		return
	default:
	}
	ctx, cancel := context.WithCancel(parent)
	f.runCancel = cancel
	f.running = true
	f.runMu.Unlock()
	defer func() { cancel(); close(f.runDone) }()

	backoff := forwardBackoffMin
	for {
		select {
		case <-ctx.Done():
			return
		case <-f.done:
			return
		default:
		}

		started := time.Now()
		err := f.session(ctx)
		if err != nil {
			f.logger.Warn("remote socket forward down", "error", err)
		}
		f.markDown(err)
		if time.Since(started) > forwardHealthyAfter {
			backoff = forwardBackoffMin // it worked for a while; fail fast next time
		}

		select {
		case <-ctx.Done():
			return
		case <-f.done:
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, forwardBackoffMax)
	}
}

// session runs one full forward lifetime: dial, prepare the socket dir,
// bind, serve until the connection or listener dies.
func (f *RemoteForward) session(ctx context.Context) error {
	client, err := dialSSH(ctx, f.cfg.SSH)
	if err != nil {
		return fmt.Errorf("dial/handshake: %w", err)
	}
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()
	f.mu.Lock()
	select {
	case <-f.done: // Close raced the dial; don't leak the connection
		f.mu.Unlock()
		_ = client.Close()
		return nil
	default:
	}
	f.client = client
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.client = nil
		f.mu.Unlock()
		_ = client.Close()
	}()

	setup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	sockPath, err := sshCall(setup, client, func() (string, error) { return f.prepareSocket(client) })
	if err != nil {
		return err
	}

	ln, err := sshCall(setup, client, func() (net.Listener, error) { return client.ListenUnix(sockPath) })
	if err != nil {
		return fmt.Errorf("remote listen %s: %w", sockPath, err)
	}
	defer ln.Close()

	// Permission clamping is part of readiness, not a best-effort promise.
	_, err = sshCall(setup, client, func() (bool, error) {
		sc, e := sftp.NewClient(client)
		if e != nil {
			return false, e
		}
		defer sc.Close()
		return true, sc.Chmod(sockPath, 0600)
	})
	if err != nil {
		return fmt.Errorf("socket permissions: %w", err)
	}
	cancel()

	// No exec traffic proves this connection alive, so probe it ourselves.
	// A dead transport makes SendRequest fail → close → Accept unblocks →
	// Serve returns → the Run loop reconnects.
	stopKeepalive := make(chan struct{})
	defer close(stopKeepalive)
	go func() {
		ticker := time.NewTicker(forwardKeepalive)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				probe, cancel := context.WithTimeout(ctx, livenessTimeout)
				_, err := sshCall(probe, client, func() (bool, error) { _, _, e := client.SendRequest("keepalive@runq", true, nil); return true, e })
				cancel()
				if err != nil {
					_ = client.Close()
					return
				}
			case <-stopKeepalive:
				return
			case <-ctx.Done():
				_ = client.Close()
				return
			}
		}
	}()

	f.logger.Info("remote socket forward up", "host", f.cfg.SSH.Host, "socket", sockPath)
	f.markUp()
	if f.cfg.OnEstablished != nil {
		f.cfg.OnEstablished()
	}
	if err := f.cfg.Serve(deadlineListener{ln}); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// prepareSocket resolves the socket path against the remote home, ensures
// a 0700 parent dir, and removes a stale socket file — sshd refuses to
// bind over an existing one (OpenSSH StreamLocalBindUnlink defaults to no).
func (f *RemoteForward) prepareSocket(client *ssh.Client) (string, error) {
	sc, err := sftp.NewClient(client)
	if err != nil {
		return "", fmt.Errorf("sftp: %w", err)
	}
	defer sc.Close()

	sockPath := f.cfg.SocketPath
	if !path.IsAbs(sockPath) {
		home, err := sc.Getwd() // sftp session starts in the user's home
		if err != nil {
			return "", fmt.Errorf("resolve remote home: %w", err)
		}
		sockPath = path.Join(home, sockPath)
	}

	dir := path.Dir(sockPath)
	if err := sc.MkdirAll(dir); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	// The socket grants control of the LOCAL daemon; on a multi-user login
	// node the parent dir permissions are the access boundary.
	if err := sc.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("chmod %s: %w", dir, err)
	}
	if len([]byte(sockPath)) > 103 {
		return "", fmt.Errorf("remote socket path too long (%d bytes; maximum 103): %s", len([]byte(sockPath)), sockPath)
	}
	ownerPath := sockPath + ".owner"
	owner := f.cfg.Owner
	if owner == "" {
		return "", fmt.Errorf("socket owner identity is required")
	}
	if fi, err := sc.Lstat(sockPath); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return "", fmt.Errorf("refusing to replace non-socket %s", sockPath)
		}
		file, err := sc.Open(ownerPath)
		if err != nil {
			return "", fmt.Errorf("socket has no ownership proof: %w", err)
		}
		data, err := io.ReadAll(io.LimitReader(file, 1024))
		_ = file.Close()
		if err != nil || string(data) != owner+"\n" {
			return "", fmt.Errorf("socket belongs to another session: %s", sockPath)
		}
		conn, err := client.Dial("unix", sockPath)
		if err == nil {
			_ = conn.Close()
			return "", fmt.Errorf("socket is already serving; stop its owning controller before reconnecting: %s", sockPath)
		}
		// ConnectFailed alone includes permission errors. Only an explicit refusal
		// proves a dead listener. A transport failure never authorizes unlink.
		var openErr *ssh.OpenChannelError
		if !errors.As(err, &openErr) {
			return "", fmt.Errorf("cannot prove socket stale: %w", err)
		}
		if !strings.Contains(strings.ToLower(openErr.Message), "connection refused") {
			// OpenSSH may deliberately hide errno behind "open failed". Query
			// the remote kernel without unlinking; only ECONNREFUSED is proof.
			if probeErr := proveSocketRefused(client, sockPath); probeErr != nil {
				return "", fmt.Errorf("cannot prove socket stale: %w", probeErr)
			}
		}
		if err := sc.Remove(sockPath); err != nil {
			return "", err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	file, err := sc.OpenFile(ownerPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return "", err
	}
	_, err = file.Write([]byte(owner + "\n"))
	ce := file.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return "", err
	}
	if err := sc.Chmod(ownerPath, 0600); err != nil {
		return "", err
	}
	return sockPath, nil
}

// Close stops the supervisor and tears down the current connection (which
// unblocks Serve). Safe to call multiple times.
func (f *RemoteForward) Close() error {
	f.closeOnce.Do(func() {
		close(f.done)
		f.runMu.Lock()
		if f.runCancel != nil {
			f.runCancel()
		}
		f.runMu.Unlock()
		f.mu.Lock()
		client := f.client
		f.mu.Unlock()
		if client != nil {
			_ = client.Close()
		}
		f.markClosed()
	})
	f.runMu.Lock()
	running := f.running
	f.runMu.Unlock()
	if running {
		<-f.runDone
	}
	// No pathname cleanup here: a delayed old supervisor must never unlink
	// the socket subsequently installed by its replacement.
	return nil
}

// proveSocketRefused performs a read-only Unix connect on the login node.
// Python is required only for servers that mask the SSH channel-open errno.
func proveSocketRefused(client *ssh.Client, socketPath string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	script := `# RUNQ_SOCKET_PROBE
import errno, socket, sys
s=socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.settimeout(2)
try:
    s.connect(sys.argv[1])
except OSError as e:
    sys.exit(3 if e.errno == errno.ECONNREFUSED else 2)
else:
    s.close()
    sys.exit(0)
`
	err = session.Run(buildCmdLine("python3", []string{"-c", script, socketPath}))
	var exit *ssh.ExitError
	if errors.As(err, &exit) && exit.ExitStatus() == 3 {
		return nil
	}
	if err == nil {
		return fmt.Errorf("socket is still serving: %s", socketPath)
	}
	return fmt.Errorf("remote socket errno probe failed (python3 required): %w", err)
}
