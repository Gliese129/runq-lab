package rfs

import (
	"context"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"time"
)

// dialSSH bounds TCP and the entire SSH handshake, not just TCP connect.
func dialSSH(ctx context.Context, cfg SSHConfig) (*ssh.Client, error) {
	hostKeys := cfg.HostKeyCallback
	if hostKeys == nil {
		var err error
		hostKeys, err = policyHostKeyCallback(cfg.HostKeyPolicy)
		if err != nil {
			return nil, err
		}
	}
	timeout := cfg.OperationTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	base, err := (&net.Dialer{}).DialContext(ctx, "tcp", cfg.addr())
	if err != nil {
		return nil, err
	}
	raw := &chunkedWriteConn{Conn: base}
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	_ = raw.SetDeadline(deadline)
	conn, chans, reqs, err := ssh.NewClientConn(raw, cfg.addr(), &ssh.ClientConfig{User: cfg.User, Auth: cfg.AuthMethods, HostKeyCallback: hostKeys})
	if err != nil {
		_ = raw.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	if ctx.Err() != nil {
		_ = conn.Close()
		return nil, ctx.Err()
	}
	_ = raw.SetDeadline(time.Time{})
	return ssh.NewClient(conn, chans, reqs), nil
}

// Cancellation closes the real connection and joins the operation. SSH/SFTP
// waits are released by transport shutdown; no orphaned blackhole goroutine.
func sshCall[T any](ctx context.Context, client *ssh.Client, fn func() (T, error)) (T, error) {
	if err := ctx.Err(); err != nil {
		var zero T
		return zero, err
	}
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1)
	go func() { v, e := fn(); done <- result{v, e} }()
	select {
	case r := <-done:
		return r.value, r.err
	case <-ctx.Done():
		if client != nil {
			_ = client.Close()
		}
		<-done
		var zero T
		return zero, ctx.Err()
	}
}

// Some VPN/user-space TCP bridges reject large Write calls instead of
// returning a partial write. SSH packets retain their byte stream framing;
// only the underlying syscall buffers are capped.
type chunkedWriteConn struct{ net.Conn }

func (c *chunkedWriteConn) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n, err := c.Conn.Write(p[:min(len(p), 512)])
		total += n
		p = p[n:]
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}
