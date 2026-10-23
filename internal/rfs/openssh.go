package rfs

// OpenSSH owns user authentication, configuration and the network connection.
// Each transport gets a private control socket: closing runq cannot terminate a
// user's terminal, and a dead master cannot silently start an untracked session.
import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type OpenSSHConfig struct {
	Binary string
	Args   []string
	Host   string
}

type sshTransport struct {
	*ssh.Client
	system *openSSHClient
}

func (c *sshTransport) NewSession() (*sshSession, error) {
	if c.system != nil {
		select {
		case <-c.system.done:
			return nil, fmt.Errorf("OpenSSH master ended: %v: %s", c.system.waitErr, c.system.stderr.String())
		default:
		}
		return &sshSession{system: c.system}, nil
	}
	s, err := c.Client.NewSession()
	if err != nil {
		return nil, err
	}
	return &sshSession{native: s}, nil
}
func (c *sshTransport) NewSFTP() (*sftp.Client, error) {
	if c.system == nil {
		return sftp.NewClient(c.Client, sftp.UseConcurrentWrites(false))
	}
	cmd := c.system.command("-s", c.system.cfg.Host, "sftp")
	stderr := &errorTail{}
	cmd.Stderr = stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		_ = out.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		_ = out.Close()
		_ = in.Close()
		return nil, err
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	pipes := &sftpProcess{WriteCloser: in, cmd: cmd, done: done}
	client, err := sftp.NewClientPipe(out, pipes, sftp.UseConcurrentWrites(false))
	if err != nil {
		_ = pipes.Close()
		return nil, fmt.Errorf("OpenSSH SFTP: %w: %s", err, stderr.String())
	}
	return client, nil
}
func (c *sshTransport) Close() error {
	if c.system != nil {
		return c.system.Close()
	}
	return c.Client.Close()
}
func (c *sshTransport) Wait() error {
	if c.system != nil {
		<-c.system.done
		return c.system.waitErr
	}
	return c.Client.Wait()
}
func (c *sshTransport) SendRequest(name string, want bool, data []byte) (bool, []byte, error) {
	if c.system == nil {
		return c.Client.SendRequest(name, want, data)
	}
	// -O check is only a local process check. A remote command proves a real
	// round trip; the caller's deadline closes the master if it blackholes.
	s, err := c.NewSession()
	if err != nil {
		return false, nil, err
	}
	defer s.Close()
	err = s.Run("true")
	return err == nil, nil, err
}
func (c *sshTransport) Dial(network, address string) (net.Conn, error) {
	if c.system == nil {
		return c.Client.Dial(network, address)
	}
	if network != "unix" {
		return nil, fmt.Errorf("unsupported SSH dial network %q", network)
	}
	local := c.system.socket("d")
	spec := local + ":" + address
	if err := c.system.control("forward", "-L", spec); err != nil {
		return nil, err
	}
	conn, err := net.Dial("unix", local)
	if err != nil {
		_ = c.system.control("cancel", "-L", spec)
		return nil, err
	}
	return &forwardConn{Conn: conn, cleanup: func() { _ = c.system.control("cancel", "-L", spec); _ = os.Remove(local) }}, nil
}
func (c *sshTransport) ListenUnix(address string) (net.Listener, error) {
	if c.system == nil {
		return c.Client.ListenUnix(address)
	}
	local := c.system.socket("r")
	ln, err := net.Listen("unix", local)
	if err != nil {
		return nil, err
	}
	spec := address + ":" + local
	if err = c.system.control("forward", "-R", spec); err != nil {
		_ = ln.Close()
		return nil, err
	}
	listener := &forwardListener{Listener: ln, cleanup: func() { _ = c.system.control("cancel", "-R", spec) }, done: make(chan struct{})}
	go func() {
		select {
		case <-c.system.done:
			_ = listener.Close()
		case <-listener.done:
		}
	}()
	return listener, nil
}

type sshSession struct {
	native         *ssh.Session
	system         *openSSHClient
	Stdout, Stderr io.Writer
	mu             sync.Mutex
	cmd            *exec.Cmd
	closed         bool
	tail           errorTail
}

func (s *sshSession) Start(command string) error {
	if s.native != nil {
		s.native.Stdout = s.Stdout
		s.native.Stderr = s.Stderr
		return s.native.Start(command)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return net.ErrClosed
	}
	s.cmd = s.system.command(s.system.cfg.Host, command)
	s.cmd.Stdout = s.Stdout
	if s.Stderr != nil {
		s.cmd.Stderr = io.MultiWriter(s.Stderr, &s.tail)
	} else {
		s.cmd.Stderr = &s.tail
	}
	return s.cmd.Start()
}
func (s *sshSession) Wait() error {
	if s.native != nil {
		return s.native.Wait()
	}
	err := s.cmd.Wait()
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() >= 0 && exit.ExitCode() != 255 {
		return remoteExit(exit.ExitCode())
	}
	// OpenSSH reserves 255 for transport errors, but a remote command may also
	// return it. Conservatively keep the submission outcome unknown; never retry.
	return fmt.Errorf("OpenSSH session: %w: %s", err, s.tail.String())
}
func (s *sshSession) Run(command string) error {
	if err := s.Start(command); err != nil {
		return err
	}
	return s.Wait()
}
func (s *sshSession) Close() error {
	if s.native != nil {
		return s.native.Close()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return nil
}

type remoteExit int

func (e remoteExit) Error() string   { return fmt.Sprintf("remote command exited with status %d", e) }
func (e remoteExit) ExitStatus() int { return int(e) }

type openSSHClient struct {
	cfg              OpenSSHConfig
	dir, controlPath string
	ctx              context.Context
	cancel           context.CancelFunc
	cmd              *exec.Cmd
	stderr           errorTail
	done             chan struct{}
	waitErr          error
	closeOnce        sync.Once
	serial           atomic.Uint64
}

func dialOpenSSH(parent context.Context, cfg SSHConfig) (*sshTransport, error) {
	timeout := cfg.OperationTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	setup, stop := context.WithTimeout(parent, timeout)
	defer stop()
	// /tmp keeps Unix socket paths below the macOS 104-byte limit. MkdirTemp
	// creates a private 0700 directory even when the user's TMPDIR is very long.
	dir, err := os.MkdirTemp("/tmp", "runq-ssh-")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &openSSHClient{cfg: *cfg.OpenSSH, dir: dir, controlPath: filepath.Join(dir, "ctl"), ctx: ctx, cancel: cancel, done: make(chan struct{})}
	// Without -f the master stays in the foreground. ForkAfterAuthentication=no
	// is deliberately omitted: OpenSSH < 8.7 rejects the option outright.
	args := []string{"-M", "-N", "-T", "-S", c.controlPath,
		"-o", "ControlMaster=yes", "-o", "ControlPersist=no", "-o", "BatchMode=yes",
		"-o", "RemoteCommand=none", "-o", "ClearAllForwardings=yes",
		"-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=1", "-o", "ExitOnForwardFailure=yes"}
	args = append(args, c.cfg.Args...)
	args = append(args, c.cfg.Host)
	c.cmd = exec.CommandContext(ctx, c.cfg.Binary, args...)
	c.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.cmd.Cancel = func() error { return syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL) }
	c.cmd.WaitDelay = time.Second
	c.cmd.Stderr = &c.stderr
	if err = c.cmd.Start(); err != nil {
		cancel()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("start OpenSSH: %w", err)
	}
	go func() { c.waitErr = c.cmd.Wait(); close(c.done) }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-setup.Done():
			_ = c.Close()
			return nil, fmt.Errorf("OpenSSH connection: %w: %s", setup.Err(), c.stderr.String())
		case <-c.done:
			_ = c.Close()
			return nil, fmt.Errorf("OpenSSH connection: %v: %s", c.waitErr, c.stderr.String())
		case <-ticker.C:
			if _, err = os.Stat(c.controlPath); err != nil {
				continue
			}
			if err = c.controlContext(setup, "check"); err == nil {
				return &sshTransport{system: c}, nil
			}
		}
	}
}

// Multiplex clients use no config: authentication has already happened in the
// master. ProxyCommand=false prevents fallback to an independent connection if
// the master disappears between the readiness check and session startup.
func (c *openSSHClient) command(args ...string) *exec.Cmd {
	base := []string{"-F", "none", "-T", "-S", c.controlPath, "-o", "ControlMaster=no", "-o", "BatchMode=yes", "-o", "ProxyCommand=false"}
	cmd := exec.CommandContext(c.ctx, c.cfg.Binary, append(base, args...)...)
	cmd.WaitDelay = time.Second
	return cmd
}
func (c *openSSHClient) controlContext(ctx context.Context, op string, args ...string) error {
	argv := append([]string{"-O", op}, args...)
	argv = append(argv, c.cfg.Host)
	cmd := c.command(argv...)
	cmd = exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("OpenSSH %s: %w: %s", op, err, strings.TrimSpace(string(out)))
	}
	return nil
}
func (c *openSSHClient) control(op string, args ...string) error {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	return c.controlContext(ctx, op, args...)
}
func (c *openSSHClient) socket(prefix string) string {
	return filepath.Join(c.dir, fmt.Sprintf("%s%d", prefix, c.serial.Add(1)))
}
func (c *openSSHClient) Close() error {
	c.closeOnce.Do(func() { c.cancel(); <-c.done; _ = os.RemoveAll(c.dir) })
	return nil
}

type sftpProcess struct {
	io.WriteCloser
	cmd  *exec.Cmd
	done chan struct{}
	once sync.Once
}

func (p *sftpProcess) Close() error {
	p.once.Do(func() { _ = p.WriteCloser.Close(); _ = p.cmd.Process.Kill(); <-p.done })
	return nil
}

type forwardConn struct {
	net.Conn
	once    sync.Once
	cleanup func()
}

func (c *forwardConn) Close() error { err := c.Conn.Close(); c.once.Do(c.cleanup); return err }

type forwardListener struct {
	net.Listener
	once    sync.Once
	cleanup func()
	done    chan struct{}
}

func (l *forwardListener) Close() error {
	var err error
	l.once.Do(func() { err = l.Listener.Close(); l.cleanup(); close(l.done) })
	return err
}

// Bounded, synchronized stderr tail: errors remain useful after a long-lived
// master exits without retaining an unlimited amount of diagnostic output.
type errorTail struct {
	mu   sync.Mutex
	data []byte
}

func (b *errorTail) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > 8192 {
		b.data = append([]byte(nil), b.data[len(b.data)-8192:]...)
	}
	return len(p), nil
}
func (b *errorTail) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}

// VerifySSHInteractive delegates first-use trust and authentication to the
// user's terminal. It never stores credentials or bypasses host verification.
func VerifySSHInteractive(ctx context.Context, cfg SSHConfig) error {
	if cfg.OpenSSH == nil {
		return nil
	}
	args := []string{"-T", "-S", "none", "-o", "ControlMaster=no", "-o", "ControlPersist=no", "-o", "RemoteCommand=none", "-o", "ClearAllForwardings=yes"}
	args = append(args, cfg.OpenSSH.Args...)
	args = append(args, cfg.OpenSSH.Host, "true")
	cmd := exec.CommandContext(ctx, cfg.OpenSSH.Binary, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("system SSH verification: %w", err)
	}
	return nil
}
