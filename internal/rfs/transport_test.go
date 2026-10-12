package rfs

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
)

type limitedWriter struct {
	net.Conn
	bytes.Buffer
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if len(p) > 512 {
		return 0, errors.New("result too large")
	}
	return w.Buffer.Write(p)
}
func TestSSHTransportCapsWritesWithoutChangingBytes(t *testing.T) {
	raw := &limitedWriter{}
	conn := &chunkedWriteConn{Conn: raw}
	data := bytes.Repeat([]byte("packet"), 10000)
	n, err := conn.Write(data)
	if err != nil || n != len(data) || !bytes.Equal(raw.Bytes(), data) {
		t.Fatalf("packet stream changed: %d %v", n, err)
	}
}

func (w *limitedWriter) Read(p []byte) (int, error) { return 0, io.EOF }
