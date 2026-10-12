package rfs

import (
	"io"
	"net"
	"sync"
)

// SSH channel connections do not implement deadlines. net/http needs a read
// deadline to interrupt its background read when finishing a response. A pipe
// supplies those semantics; closing it releases both pumps and the SSH channel.
type deadlineListener struct{ net.Listener }

func (l deadlineListener) Accept() (net.Conn, error) {
	remote, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	front, pump := net.Pipe()
	conn := &deadlineConn{Conn: front, remote: remote, pump: pump}
	go func() { _, _ = io.Copy(pump, remote); _ = conn.Close() }()
	go func() { _, _ = io.Copy(remote, pump); _ = conn.Close() }()
	return conn, nil
}

type deadlineConn struct {
	net.Conn
	remote net.Conn
	pump   net.Conn
	once   sync.Once
}

func (c *deadlineConn) Close() error {
	c.once.Do(func() { _ = c.Conn.Close(); _ = c.pump.Close(); _ = c.remote.Close() })
	return nil
}
func (c *deadlineConn) LocalAddr() net.Addr  { return c.remote.LocalAddr() }
func (c *deadlineConn) RemoteAddr() net.Addr { return c.remote.RemoteAddr() }
