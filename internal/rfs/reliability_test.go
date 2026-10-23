package rfs

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type faultSSH struct {
	hostKey           ssh.PublicKey
	listener          net.Listener
	cfg               SSHConfig
	root              string
	mode              atomic.Int32
	mu                sync.Mutex
	conns             []net.Conn
	sockets           map[string]net.Listener
	wg                sync.WaitGroup
	keepaliveRequests atomic.Int32
	execRequests      atomic.Int32
}

const (
	faultHandshake    = 1
	faultSFTP         = 2
	faultBind         = 3
	faultExec         = 4
	faultFile         = 5
	faultMaskedSocket = 6
	faultKeepalive    = 7
	faultOwnerWrite   = 8
)

func newFaultSSH(t *testing.T, configure ...func(*ssh.ServerConfig)) *faultSSH {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rq-")
	if err != nil {
		t.Fatal(err)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	for _, fn := range configure {
		fn(config)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &faultSSH{hostKey: signer.PublicKey(), listener: ln, root: dir, sockets: map[string]net.Listener{}}
	f.cfg = SSHConfig{Host: ln.Addr().String(), User: "test", OperationTimeout: 500 * time.Millisecond, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if string(key.Marshal()) != string(signer.PublicKey().Marshal()) {
			return errors.New("wrong test host")
		}
		return nil
	}}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			f.mu.Lock()
			f.conns = append(f.conns, raw)
			f.mu.Unlock()
			f.wg.Add(1)
			go func() {
				defer f.wg.Done()
				if f.mode.Load() == faultHandshake {
					_, _ = io.Copy(io.Discard, raw)
					return
				}
				server, chans, reqs, err := ssh.NewServerConn(raw, config)
				if err != nil {
					return
				}
				defer server.Close()
				go f.requests(server, reqs)
				for ch := range chans {
					if ch.ChannelType() == "direct-tcpip" {
						var p struct {
							Host       string
							Port       uint32
							Origin     string
							OriginPort uint32
						}
						_ = ssh.Unmarshal(ch.ExtraData(), &p)
						conn, err := net.Dial("tcp", net.JoinHostPort(p.Host, fmt.Sprint(p.Port)))
						if err != nil {
							_ = ch.Reject(ssh.ConnectionFailed, err.Error())
							continue
						}
						channel, req, err := ch.Accept()
						if err != nil {
							_ = conn.Close()
							continue
						}
						go ssh.DiscardRequests(req)
						go bridge(channel, conn)
						continue
					}
					if ch.ChannelType() == "direct-streamlocal@openssh.com" {
						var payload struct {
							Path, Reserved string
							ReservedPort   uint32
						}
						_ = ssh.Unmarshal(ch.ExtraData(), &payload)
						conn, err := net.Dial("unix", payload.Path)
						if err != nil {
							message := err.Error()
							if f.mode.Load() == faultMaskedSocket {
								message = "open failed"
							}
							_ = ch.Reject(ssh.ConnectionFailed, message)
							continue
						}
						channel, req, err := ch.Accept()
						if err != nil {
							_ = conn.Close()
							continue
						}
						go ssh.DiscardRequests(req)
						go bridge(channel, conn)
						continue
					}
					if ch.ChannelType() != "session" {
						_ = ch.Reject(ssh.UnknownChannelType, "unsupported")
						continue
					}
					channel, req, err := ch.Accept()
					if err != nil {
						continue
					}
					go func() {
						defer channel.Close()
						for r := range req {
							switch r.Type {
							case "subsystem":
								_ = r.Reply(true, nil)
								if f.mode.Load() == faultSFTP {
									_, _ = io.Copy(io.Discard, channel)
									return
								}
								var stream io.ReadWriteCloser = channel
								if f.mode.Load() == faultOwnerWrite {
									stream = &dropSFTPWrite{ReadWriteCloser: channel}
								}
								if f.mode.Load() == faultFile {
									stream = &freezeSFTP{ReadWriteCloser: channel}
								}
								srv, err := sftp.NewServer(stream, sftp.WithServerWorkingDirectory(f.root))
								if err != nil {
									return
								}
								_ = srv.Serve()
								_ = srv.Close()
								return
							case "exec":
								f.execRequests.Add(1)
								_ = r.Reply(true, nil)
								if f.mode.Load() == faultExec {
									_ = server.Close()
									return
								}
								var code uint32
								var command struct{ Command string }
								_ = ssh.Unmarshal(r.Payload, &command)
								if command.Command == "exit 7" {
									code = 7
								}
								if strings.Contains(command.Command, "RUNQ_SOCKET_PROBE") {
									fields := strings.Fields(command.Command)
									p := strings.Trim(fields[len(fields)-1], "'")
									conn, e := net.Dial("unix", p)
									if e == nil {
										_ = conn.Close()
									} else {
										code = 3
									}
								}
								_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{code}))
								return
							default:
								_ = r.Reply(false, nil)
							}
						}
					}()
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		f.mu.Lock()
		for _, c := range f.conns {
			_ = c.Close()
		}
		for _, l := range f.sockets {
			_ = l.Close()
		}
		f.mu.Unlock()
		f.wg.Wait()
		_ = os.RemoveAll(dir)
	})
	return f
}
func bridge(a io.ReadWriteCloser, b io.ReadWriteCloser) {
	defer a.Close()
	defer b.Close()
	done := make(chan struct{})
	go func() { _, _ = io.Copy(a, b); _ = a.Close(); _ = b.Close(); close(done) }()
	_, _ = io.Copy(b, a)
	_ = b.Close()
	_ = a.Close()
	<-done
}
func (f *faultSSH) requests(server *ssh.ServerConn, reqs <-chan *ssh.Request) {
	owned := map[string]net.Listener{}
	defer func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		for path, ln := range owned {
			_ = ln.Close()
			if f.sockets[path] == ln {
				delete(f.sockets, path)
			}
		}
	}()
	for r := range reqs {
		var payload struct{ Path string }
		_ = ssh.Unmarshal(r.Payload, &payload)
		switch r.Type {
		case "keepalive@runq":
			f.keepaliveRequests.Add(1)
			if f.mode.Load() != faultKeepalive {
				_ = r.Reply(false, nil)
			}
		case "streamlocal-forward@openssh.com":
			if f.mode.Load() == faultBind {
				continue
			}
			ln, err := net.Listen("unix", payload.Path)
			if err != nil {
				_ = r.Reply(false, nil)
				continue
			}
			ln.(*net.UnixListener).SetUnlinkOnClose(false)
			f.mu.Lock()
			f.sockets[payload.Path] = ln
			owned[payload.Path] = ln
			f.mu.Unlock()
			_ = r.Reply(true, nil)
			go func() {
				for {
					c, err := ln.Accept()
					if err != nil {
						return
					}
					ch, req, err := server.OpenChannel("forwarded-streamlocal@openssh.com", ssh.Marshal(struct{ Path, Reserved string }{payload.Path, ""}))
					if err != nil {
						_ = c.Close()
						continue
					}
					go ssh.DiscardRequests(req)
					go bridge(ch, c)
				}
			}()
		case "cancel-streamlocal-forward@openssh.com":
			f.mu.Lock()
			if l := f.sockets[payload.Path]; l != nil {
				_ = l.Close()
				delete(f.sockets, payload.Path)
			}
			f.mu.Unlock()
			_ = r.Reply(true, nil)
		default:
			_ = r.Reply(false, nil)
		}
	}
}
func TestHandshakeAndSFTPTimeoutAndClose(t *testing.T) {
	for _, mode := range []int32{faultHandshake, faultSFTP} {
		t.Run(map[int32]string{faultHandshake: "handshake", faultSFTP: "sftp"}[mode], func(t *testing.T) {
			f := newFaultSSH(t)
			f.mode.Store(mode)
			fs := NewSSHFS(f.cfg)
			defer fs.Close()
			started := time.Now()
			_, err := fs.Home()
			if err == nil || time.Since(started) > 2*time.Second {
				t.Fatalf("unbounded setup: %v (%v)", err, time.Since(started))
			}
			f.mode.Store(0)
			if _, err := fs.Home(); err != nil {
				t.Fatalf("failed to reconnect: %v", err)
			}
		})
	}
	f := newFaultSSH(t)
	f.mode.Store(faultHandshake)
	cfg := f.cfg
	cfg.OperationTimeout = time.Minute
	fs := NewSSHFS(cfg)
	done := make(chan error, 1)
	go func() { _, e := fs.Home(); done <- e }()
	time.Sleep(20 * time.Millisecond)
	_ = fs.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("closed setup succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel handshake")
	}
}
func TestForwardCancellationDuringBind(t *testing.T) {
	f := newFaultSSH(t)
	f.mode.Store(faultBind)
	forward := NewRemoteForward(RemoteForwardConfig{SSH: f.cfg, SocketPath: filepath.Join(f.root, "bind/rq.sock"), Owner: "controller:target", Serve: func(ln net.Listener) error { _, e := ln.Accept(); return e }})
	go forward.Run(context.Background())
	time.Sleep(40 * time.Millisecond)
	started := time.Now()
	_ = forward.Close()
	if time.Since(started) > 2*time.Second {
		t.Fatal("close hung during bind")
	}
	if forward.Status().State != ForwardClosed {
		t.Fatal("close state lost")
	}
}
func TestForwardIsolationConflictAndReplacement(t *testing.T) {
	f := newFaultSSH(t)
	makeForward := func(controller, target string) *RemoteForward {
		return NewRemoteForward(RemoteForwardConfig{SSH: f.cfg, SocketPath: filepath.Join(f.root, controller, target, "rq.sock"), Owner: controller + ":" + target, Serve: func(ln net.Listener) error {
			return http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, controller+":"+target) }))
		}})
	}
	a, b, c := makeForward("c1", "t1"), makeForward("c1", "t2"), makeForward("c2", "t1")
	// The fixture's request lifetime cleanup is per connection in production;
	// use sequential readiness, and keep all three connections open here.
	for _, fw := range []*RemoteForward{a, b, c} {
		go fw.Run(context.Background())
		t.Cleanup(func() { _ = fw.Close() })
		waitUp(t, fw)
	}
	fs := NewSSHFS(f.cfg)
	defer fs.Close()
	for _, fw := range []*RemoteForward{a, b, c} {
		conn, err := fs.DialUnix(context.Background(), fw.cfg.SocketPath)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(conn, "GET / HTTP/1.0\r\n\r\n")
		buf, _ := io.ReadAll(conn)
		_ = conn.Close()
		if !containsBytes(buf, fw.cfg.Owner) {
			t.Fatalf("wrong forward: %s", buf)
		}
	}
	competitor := makeForward("c1", "t1")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := competitor.session(ctx)
	if err == nil {
		t.Fatal("active socket overwritten")
	}
	// Closing an old session must not erase an unrelated target's listener.
	f.mode.Store(faultMaskedSocket)
	_ = a.Close()
	if _, err := os.Stat(b.cfg.SocketPath); err != nil {
		t.Fatal(err)
	}
	replacement := makeForward("c1", "t1")
	go replacement.Run(context.Background())
	t.Cleanup(func() { _ = replacement.Close() })
	waitUp(t, replacement)
	_ = a.Close()
	conn, err := fs.DialUnix(context.Background(), replacement.cfg.SocketPath)
	if err != nil {
		t.Fatalf("late cleanup unlinked replacement: %v", err)
	}
	_ = conn.Close()
}
func containsBytes(data []byte, s string) bool {
	for i := 0; i+len(s) <= len(data); i++ {
		if string(data[i:i+len(s)]) == s {
			return true
		}
	}
	return false
}
func waitUp(t *testing.T, f *RemoteForward) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if f.Status().State == ForwardUp {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("forward not up: %+v", f.Status())
}
func TestExecDisconnectIsOutcomeUnknown(t *testing.T) {
	f := newFaultSSH(t)
	f.mode.Store(faultExec)
	fs := NewSSHFS(f.cfg)
	defer fs.Close()
	_, _, _, err := fs.Exec(context.Background(), "submit-once")
	if !errors.Is(err, ErrExecInterrupted) {
		t.Fatalf("accepted submission must not be replayable: %v", err)
	}
}

type freezeSFTP struct {
	io.ReadWriteCloser
	first bool
}

func (s *freezeSFTP) Write(p []byte) (int, error) {
	if !s.first {
		s.first = true
		return s.ReadWriteCloser.Write(p)
	}
	return len(p), nil
}
func TestFileOperationsCancelActualIO(t *testing.T) {
	f := newFaultSSH(t)
	f.mode.Store(faultFile)
	fs := NewSSHFS(f.cfg)
	defer fs.Close()
	if _, err := fs.conn.getConn(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(30*time.Millisecond, cancel)
	started := time.Now()
	_, err := fs.WithContext(ctx).ReadFile(filepath.Join(f.root, "file"))
	if !errors.Is(err, context.Canceled) || time.Since(started) > 2*time.Second {
		t.Fatalf("file read did not cancel: %v", err)
	}
	f.mode.Store(0)
	if _, err := fs.Home(); err != nil {
		t.Fatalf("file timeout poisoned reconnect: %v", err)
	}
}
func TestForwardReconnectAndBackoffCancellation(t *testing.T) {
	f := newFaultSSH(t)
	fw := NewRemoteForward(RemoteForwardConfig{SSH: f.cfg, SocketPath: filepath.Join(f.root, "reconnect/rq.sock"), Owner: "c:t", Serve: func(ln net.Listener) error { return http.Serve(ln, http.NotFoundHandler()) }})
	go fw.Run(context.Background())
	defer fw.Close()
	waitUp(t, fw)
	fw.mu.Lock()
	client := fw.client
	fw.mu.Unlock()
	_ = client.Close()
	recovered := false
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		fw.mu.Lock()
		changed := fw.client != nil && fw.client != client
		fw.mu.Unlock()
		if changed && fw.Status().State == ForwardUp {
			recovered = true
			break
		}
	}
	if !recovered {
		t.Fatalf("forward did not recover: %+v", fw.Status())
	}
	fw.mu.Lock()
	client = fw.client
	fw.mu.Unlock()
	_ = client.Close()
	time.Sleep(20 * time.Millisecond)
	started := time.Now()
	_ = fw.Close()
	if time.Since(started) > 2*time.Second {
		t.Fatal("Close waited for backoff")
	}
}

func TestKeepaliveBlackholeRecoveryAndClose(t *testing.T) {
	f := newFaultSSH(t)
	fw := NewRemoteForward(RemoteForwardConfig{SSH: f.cfg, SocketPath: filepath.Join(f.root, "blackhole/rq.sock"), Owner: "c:t", Serve: func(ln net.Listener) error { return http.Serve(ln, http.NotFoundHandler()) }})
	go fw.Run(context.Background())
	defer fw.Close()
	waitUp(t, fw)
	fw.mu.Lock()
	old := fw.client
	fw.mu.Unlock()
	f.mode.Store(faultKeepalive)
	started := time.Now()
	// Exercise the production 15s keepalive + 5s deadline, not a second mock
	// watchdog. Leave TCP open while dropping keepalive replies.
	deadline := time.Now().Add(forwardKeepalive + livenessTimeout + 5*time.Second)
	for time.Now().Before(deadline) {
		if fw.Status().State == ForwardReconnecting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if fw.Status().State != ForwardReconnecting {
		t.Fatalf("blackhole was never detected: %+v", fw.Status())
	}
	transportDone := make(chan error, 1)
	go func() { transportDone <- old.Wait() }()
	select {
	case <-transportDone:
	case <-time.After(time.Second):
		t.Fatal("timeout did not close the real SSH transport")
	}
	f.mode.Store(0)
	dropped := time.Since(started)
	deadline = time.Now().Add(forwardBackoffMin + 4*time.Second)
	for time.Now().Before(deadline) {
		fw.mu.Lock()
		changed := fw.client != nil && fw.client != old
		fw.mu.Unlock()
		if changed && fw.Status().State == ForwardUp {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if fw.Status().State != ForwardUp {
		t.Fatalf("blackhole did not recover: %+v", fw.Status())
	}
	t.Logf("production cadence: detected in %v, recovered in %v", dropped, time.Since(started))
	// Once another keepalive is actually waiting, Close must join the blocked
	// request immediately and never create another connection in backoff.
	f.mode.Store(faultKeepalive)
	requests := f.keepaliveRequests.Load()
	deadline = time.Now().Add(forwardKeepalive + 3*time.Second)
	for f.keepaliveRequests.Load() == requests && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if f.keepaliveRequests.Load() == requests {
		t.Fatal("second keepalive did not start")
	}
	started = time.Now()
	_ = fw.Close()
	if time.Since(started) > time.Second {
		t.Fatalf("Close hung on keepalive: %v", time.Since(started))
	}
	select {
	case <-fw.runDone:
	default:
		t.Fatal("supervisor not joined")
	}
	f.mu.Lock()
	connections := len(f.conns)
	f.mu.Unlock()
	time.Sleep(forwardBackoffMin + 100*time.Millisecond)
	f.mu.Lock()
	after := len(f.conns)
	f.mu.Unlock()
	if after != connections || fw.Status().State != ForwardClosed {
		t.Fatal("closed forward kept reconnecting")
	}
	if f.execRequests.Load() != 0 {
		t.Fatal("forward recovery replayed a command")
	}
}

func TestFileBrowsingReportsContactAndRecovery(t *testing.T) {
	f := newFaultSSH(t)
	remote := NewSSHFS(f.cfg)
	defer remote.Close()
	var calls int
	var contactErr error
	remote.SetContactObserver(func(err error) { calls++; contactErr = err })
	f.mode.Store(faultSFTP)
	if _, err := remote.Home(); err == nil || calls != 1 || contactErr == nil {
		t.Fatalf("failed SFTP contact not reported: calls=%d operation=%v contact=%v", calls, err, contactErr)
	}
	f.mode.Store(0)
	if _, err := remote.WithContext(context.Background()).ReadDir(f.root); err != nil || calls != 2 || contactErr != nil {
		t.Fatalf("file browsing did not restore contact: calls=%d operation=%v contact=%v", calls, err, contactErr)
	}
	if _, err := remote.Stat(filepath.Join(f.root, "missing")); !errors.Is(err, os.ErrNotExist) || calls != 3 || contactErr != nil {
		t.Fatalf("remote missing file must still prove contact: calls=%d operation=%v contact=%v", calls, err, contactErr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = remote.WithContext(ctx).Home()
	if calls != 3 {
		t.Fatalf("cancellation changed reachability: calls=%d", calls)
	}
}

// Drop the transport immediately before the server receives a file write.
// This models sleep after OPEN/TRUNCATE but before the owner bytes arrive.
type dropSFTPWrite struct {
	io.ReadWriteCloser
	pending bytes.Buffer
}

func (s *dropSFTPWrite) Read(p []byte) (int, error) {
	if s.pending.Len() == 0 {
		var header [4]byte
		if _, err := io.ReadFull(s.ReadWriteCloser, header[:]); err != nil {
			return 0, err
		}
		packet := make([]byte, binary.BigEndian.Uint32(header[:]))
		if _, err := io.ReadFull(s.ReadWriteCloser, packet); err != nil {
			return 0, err
		}
		if packet[0] == 6 {
			_ = s.ReadWriteCloser.Close()
			return 0, io.EOF
		} // SSH_FXP_WRITE
		s.pending.Write(header[:])
		s.pending.Write(packet)
	}
	return s.pending.Read(p)
}

func TestForwardOwnerSurvivesInterruptedCreation(t *testing.T) {
	f := newFaultSSH(t)
	public := filepath.Join(f.root, "owner-interrupt/rq.sock")
	fw := NewRemoteForward(RemoteForwardConfig{SSH: f.cfg, SocketPath: public, Owner: "c:t"})
	f.mode.Store(faultOwnerWrite)
	client, err := dialSSH(context.Background(), f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fw.prepareSocket(client)
	_ = client.Close()
	if err == nil {
		t.Fatal("expected interrupted owner write")
	}
	if data, err := os.ReadFile(public + ".owner"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted write published ownership: %q, %v", data, err)
	}
	f.mode.Store(0)
	client, err = dialSSH(context.Background(), f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := fw.prepareSocket(client); err != nil {
		t.Fatalf("creation did not recover: %v", err)
	}
	data, err := os.ReadFile(public + ".owner")
	if err != nil || string(data) != "c:t\n" {
		t.Fatalf("owner=%q err=%v", data, err)
	}
}

func TestForwardReconnectNeverRewritesOwner(t *testing.T) {
	f := newFaultSSH(t)
	public := filepath.Join(f.root, "owner-reconnect/rq.sock")
	_ = os.MkdirAll(filepath.Dir(public), 0700)
	_ = os.WriteFile(public+".owner", []byte("c:t\n"), 0600)
	timestamp := time.Unix(1000000, 0)
	_ = os.Chtimes(public+".owner", timestamp, timestamp)
	_ = os.Symlink(filepath.Join(filepath.Dir(public), ".f-0123456789ab"), public)
	// Any write would drop this connection. A stale generation must recover
	// using the existing owner proof, without opening it for writing at all.
	f.mode.Store(faultOwnerWrite)
	client, err := dialSSH(context.Background(), f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	fw := NewRemoteForward(RemoteForwardConfig{SSH: f.cfg, SocketPath: public, Owner: "c:t"})
	if _, err := fw.prepareSocket(client); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(public + ".owner")
	if err != nil || !info.ModTime().Equal(timestamp) {
		t.Fatalf("owner was rewritten: %v %v", info, err)
	}
}

func TestForwardRejectsEmptyAndForeignOwner(t *testing.T) {
	for _, owner := range []string{"", "another:controller\n"} {
		t.Run(fmt.Sprintf("owner=%q", owner), func(t *testing.T) {
			f := newFaultSSH(t)
			public := filepath.Join(f.root, "rq.sock")
			_ = os.WriteFile(public+".owner", []byte(owner), 0600)
			client, err := dialSSH(context.Background(), f.cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			fw := NewRemoteForward(RemoteForwardConfig{SSH: f.cfg, SocketPath: public, Owner: "c:t"})
			if _, err := fw.prepareSocket(client); err == nil {
				t.Fatal("unverified owner accepted")
			}
			data, _ := os.ReadFile(public + ".owner")
			if string(data) != owner {
				t.Fatal("unverified owner overwritten")
			}
		})
	}
}
