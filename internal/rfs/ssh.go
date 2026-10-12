package rfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// ErrExecInterrupted marks an Exec whose command STARTED on the remote side
// but whose verdict was lost (connection dropped mid-flight, session torn
// down). This is neither a clean transport failure (the command may have run
// to completion) nor a scheduler verdict (there is no exit code). Callers
// with side-effecting commands (job submission) must treat it as "outcome
// unknown" — never as retry-safe (RQ-74).
var ErrExecInterrupted = errors.New("remote command interrupted: outcome unknown")

// SSHFS implements FS over an SSH connection. File operations go through sftp;
// command execution opens SSH sessions. The underlying *sshConn is lazy — no
// network traffic until the first call — and self-healing: dead connections
// are detected and redialed transparently (see sshConn.getConn).
//
// Thread safety: safe for concurrent use. sshConn serializes dial/reconnect;
// sftp.Client is documented as safe for concurrent use; SSH sessions are
// bounded by the semaphore in sshConn.
//
// Every operation is bracketed by beginOp/endOp so the idle watcher never
// tears down a connection with work in flight; for handle-returning calls
// (Open, ExecStream) the bracket extends until the handle is closed.
type SSHFS struct {
	conn *sshConn
	ctx  context.Context
}

// NewSSHFS creates an SSHFS. The connection is lazy — no dial happens here.
func NewSSHFS(cfg SSHConfig) *SSHFS {
	return &SSHFS{conn: newSSHConn(cfg)}
}

// Close tears down the SSH connection. The SSHFS should not be used after Close.
func (s *SSHFS) Close() error {
	return s.conn.Close()
}

// WithContext scopes file operations without changing the shared connection.
func (s *SSHFS) WithContext(ctx context.Context) *SSHFS { return &SSHFS{conn: s.conn, ctx: ctx} }
func (s *SSHFS) operationContext() (context.Context, context.CancelFunc) {
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, s.conn.timeout())
}
func sftpOp[T any](s *SSHFS, fn func(*sftp.Client) (T, error)) (T, error) {
	ctx, cancel := s.operationContext()
	defer cancel()
	return sftpOpContext(s, ctx, fn)
}
func sftpOpContext[T any](s *SSHFS, ctx context.Context, fn func(*sftp.Client) (T, error)) (value T, err error) {
	s.conn.beginOp()
	defer func() { s.conn.endOpErr(err) }()
	client, sc, err := s.conn.getSFTPContext(ctx)
	if err != nil {
		return value, err
	}
	value, err = sshCall(ctx, client, func() (T, error) { return fn(sc) })
	if ctx.Err() != nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		s.conn.invalidate(client)
	}
	return value, err
}
func (s *SSHFS) Stat(path string) (fs.FileInfo, error) {
	return sftpOp(s, func(sc *sftp.Client) (fs.FileInfo, error) { return sc.Stat(path) })
}

// Only atomic replacement or a server's normal rename is permitted. A failed
// replacement must never delete the existing executable/configuration.
func (s *SSHFS) Rename(oldPath, newPath string) error {
	_, err := sftpOp(s, func(sc *sftp.Client) (bool, error) {
		if _, ok := sc.HasExtension("posix-rename@openssh.com"); ok {
			return true, sc.PosixRename(oldPath, newPath)
		}
		return true, sc.Rename(oldPath, newPath)
	})
	return err
}
func (s *SSHFS) Home() (string, error) {
	return sftpOp(s, func(sc *sftp.Client) (string, error) { return sc.Getwd() })
}
func (s *SSHFS) ReadFile(path string) ([]byte, error) {
	return sftpOp(s, func(sc *sftp.Client) ([]byte, error) {
		f, err := sc.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(f)
	})
}
func (s *SSHFS) Open(path string) (File, error) {
	s.conn.beginOp()
	ctx, cancel := s.operationContext()
	defer cancel()
	client, sc, err := s.conn.getSFTPContext(ctx)
	if err != nil {
		s.conn.endOpErr(err)
		return nil, err
	}
	f, err := sshCall(ctx, client, func() (*sftp.File, error) { return sc.Open(path) })
	if err != nil {
		if ctx.Err() != nil {
			s.conn.invalidate(client)
		}
		s.conn.endOpErr(err)
		return nil, err
	}
	return &sshFile{f: f, s: s, client: client, release: s.conn.endOp}, nil
}
func (s *SSHFS) ReadDir(path string) ([]fs.DirEntry, error) {
	return sftpOp(s, func(sc *sftp.Client) ([]fs.DirEntry, error) {
		fis, err := sc.ReadDir(path)
		if err != nil {
			return nil, err
		}
		entries := make([]fs.DirEntry, 0, len(fis))
		for _, fi := range fis {
			entries = append(entries, fs.FileInfoToDirEntry(fi))
		}
		return entries, nil
	})
}
func (s *SSHFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	ctx, cancel := s.operationContext()
	defer cancel()
	return s.WriteFileContext(ctx, path, data, perm)
}

// WriteFileContext uses an explicit transfer budget. Connection setup remains
// independently bounded; a large binary is not limited by a small-file timeout.
func (s *SSHFS) WriteFileContext(parent context.Context, path string, data []byte, perm os.FileMode) error {
	ctx := parent
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.conn.timeout())
		defer cancel()
	}
	_, err := sftpOpContext(s, ctx, func(sc *sftp.Client) (bool, error) {
		f, err := sc.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
		if err != nil {
			return false, err
		}
		if _, err = f.ReadFrom(bytes.NewReader(data)); err != nil {
			_ = f.Close()
			return false, err
		}
		if err = f.Close(); err != nil {
			return false, err
		}
		return true, sc.Chmod(path, perm)
	})
	return err
}
func (s *SSHFS) MkdirAll(path string, perm os.FileMode) error {
	_, err := sftpOp(s, func(sc *sftp.Client) (bool, error) {
		if err := sc.MkdirAll(path); err != nil {
			return false, err
		}
		return true, sc.Chmod(path, perm)
	})
	return err
}
func (s *SSHFS) Remove(path string) error {
	_, err := sftpOp(s, func(sc *sftp.Client) (bool, error) { return true, sc.Remove(path) })
	return err
}

// DialUnix opens a remote socket through SSH. Used to prove that a reverse
// forward actually reaches the expected controller/target before connect succeeds.
func (s *SSHFS) DialUnix(ctx context.Context, path string) (net.Conn, error) {
	client, err := s.conn.getConnContext(ctx)
	if err != nil {
		return nil, err
	}
	setup, cancel := context.WithTimeout(ctx, s.conn.timeout())
	defer cancel()
	conn, err := sshCall(setup, client, func() (net.Conn, error) { return client.Dial("unix", path) })
	if setup.Err() != nil {
		s.conn.invalidate(client)
	}
	return conn, err
}

// ── Command ─────────────────────────────────────────────────────────────────

// Exec runs a remote command via an SSH session and returns stdout, stderr,
// and the exit code. A non-zero exit is NOT a Go error; transport failures
// are. ctx cancellation closes the session (the remote command gets EOF/HUP).
func (s *SSHFS) Exec(parent context.Context, cmd string, args ...string) (stdout, stderr []byte, exitCode int, err error) {
	ctx := parent
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.conn.timeout())
		defer cancel()
	}
	s.conn.beginOp()
	// err carries transport failures only (non-zero exits return err == nil),
	// which is exactly the classification endOpErr wants.
	defer func() { s.conn.endOpErr(err) }()

	sess, client, release, err := s.conn.newSession(ctx)
	if err != nil {
		return nil, nil, -1, err
	}
	defer release()
	defer sess.Close()

	var sob, seb bytes.Buffer
	sess.Stdout = &sob
	sess.Stderr = &seb

	// ctx watcher with a guaranteed exit path: `finished` is closed when the
	// command settles, so this goroutine never outlives the call (a bare
	// <-ctx.Done() wait would leak one goroutine per call under a long-lived
	// ctx).
	finished := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			s.conn.invalidate(client)
		case <-finished:
		}
	}()

	// Losing Start acknowledgement can also hide an accepted command. A Wait failure that is not an ExitError means the command DID
	// start but its verdict was lost (connection dropped mid-flight, session
	// closed by ctx): callers that submit work must NOT retry on that — the
	// remote side effect may exist. ErrExecInterrupted marks this class.
	setup, stop := context.WithTimeout(ctx, s.conn.timeout())
	_, serr := sshCall(setup, client, func() (bool, error) { return true, sess.Start(buildCmdLine(cmd, args)) })
	stop()
	if serr != nil {
		close(finished)
		return sob.Bytes(), seb.Bytes(), -1, fmt.Errorf("%w: start acknowledgement lost: %v", ErrExecInterrupted, serr)
	}
	waitErr := sess.Wait()
	close(finished)

	so, se := sob.Bytes(), seb.Bytes()
	if waitErr != nil {
		if ee, ok := waitErr.(*ssh.ExitError); ok {
			return so, se, ee.ExitStatus(), nil
		}
		return so, se, -1, fmt.Errorf("%w: %v", ErrExecInterrupted, waitErr)
	}
	return so, se, 0, nil
}

// ExecStream runs a remote command and returns a streaming reader of its
// combined output. The session, semaphore slot, and busy-marker are released
// when the command exits (or ctx is cancelled); the pipe's read side reports
// the command's exit status via CloseWithError.
func (s *SSHFS) ExecStream(ctx context.Context, cmd string, args ...string) (io.ReadCloser, error) {
	s.conn.beginOp()

	sess, client, release, err := s.conn.newSession(ctx)
	if err != nil {
		s.conn.endOpErr(err)
		return nil, err
	}

	pr, pw := io.Pipe()
	sess.Stdout = pw
	sess.Stderr = pw

	setup, stop := context.WithTimeout(ctx, s.conn.timeout())
	_, startErr := sshCall(setup, client, func() (bool, error) { return true, sess.Start(buildCmdLine(cmd, args)) })
	stop()
	if err := startErr; err != nil {
		pr.Close()
		pw.Close()
		sess.Close()
		release()
		s.conn.endOpErr(err)
		return nil, err
	}

	// Same watcher pattern as Exec: exits when the stream finishes.
	finished := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			s.conn.invalidate(client)
		case <-finished:
		}
	}()
	go func() {
		pw.CloseWithError(sess.Wait())
		sess.Close()
		release()
		close(finished)
		s.conn.endOp()
	}()
	return &sshStream{ReadCloser: pr, closeSession: func() error {
		closeCtx, cancel := context.WithTimeout(context.Background(), s.conn.timeout())
		defer cancel()
		_, err := sshCall(closeCtx, client, func() (bool, error) { return true, sess.Close() })
		if closeCtx.Err() != nil {
			s.conn.invalidate(client)
		}
		return err
	}}, nil
}

// ── Transfer ────────────────────────────────────────────────────────────────

// CopyToLocal downloads a remote file via sftp. The busy-marker covers the
// whole transfer — large checkpoint downloads are never cut by idle teardown.
func (s *SSHFS) CopyToLocal(remotePath, localPath string, opts ...CopyOption) error {
	_, err := sftpOp(s, func(sc *sftp.Client) (bool, error) {
		src, err := sc.Open(remotePath)
		if err != nil {
			return false, err
		}
		defer src.Close()
		dst, err := os.Create(localPath)
		if err != nil {
			return false, err
		}
		defer dst.Close()
		_, err = io.Copy(dst, src)
		return true, err
	})
	return err
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// buildCmdLine builds a command string for ssh.Session.Run.
//
// With no args, cmd is sent as-is (it's a pre-built shell command like
// "squeue -j 12345" that the remote shell should parse directly).
//
// With args, each token is shell-quoted so the remote sshd's shell
// word-splitting reconstructs the original argv. Without this,
// Exec("sh", "-c", "sbatch run.sh") would arrive as three words
// (sh / -c / sbatch) instead of two (sh -c / "sbatch run.sh").
func buildCmdLine(cmd string, args []string) string {
	if len(args) == 0 {
		return cmd
	}
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, shellQuote(cmd))
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

// sshFile wraps *sftp.File to satisfy rfs.File. Closing it also releases the
// connection's busy-marker taken at Open (idempotent).
type sshFile struct {
	f        *sftp.File
	s        *SSHFS
	client   *ssh.Client
	release  func()
	once     sync.Once
	closeErr error
}

func fileOp[T any](sf *sshFile, fn func() (T, error)) (T, error) {
	ctx, cancel := sf.s.operationContext()
	defer cancel()
	v, err := sshCall(ctx, sf.client, fn)
	if ctx.Err() != nil {
		sf.s.conn.invalidate(sf.client)
	}
	return v, err
}
func (sf *sshFile) Read(p []byte) (int, error) {
	return fileOp(sf, func() (int, error) { return sf.f.Read(p) })
}
func (sf *sshFile) Seek(off int64, w int) (int64, error) {
	return fileOp(sf, func() (int64, error) { return sf.f.Seek(off, w) })
}
func (sf *sshFile) Stat() (fs.FileInfo, error) {
	return fileOp(sf, func() (fs.FileInfo, error) { return sf.f.Stat() })
}
func (sf *sshFile) Close() error {
	sf.once.Do(func() { _, sf.closeErr = fileOp(sf, func() (bool, error) { return true, sf.f.Close() }); sf.release() })
	return sf.closeErr
}

// compile-time interface check
var _ FS = (*SSHFS)(nil)

type sshStream struct {
	io.ReadCloser
	closeSession func() error
	once         sync.Once
	err          error
}

func (s *sshStream) Close() error {
	s.once.Do(func() { _ = s.ReadCloser.Close(); s.err = s.closeSession() })
	return s.err
}

// RemoveContext allows bounded cleanup with a context independent of a failed
// transfer while retaining the manager's connection/setup time bounds.
func (s *SSHFS) RemoveContext(ctx context.Context, path string) error {
	return s.WithContext(ctx).Remove(path)
}
