package rfs

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh/knownhosts"
)

func systemTestConfig(t *testing.T, f *faultSSH) SSHConfig {
	t.Helper()
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("OpenSSH not installed")
	}
	host, port, _ := net.SplitHostPort(f.cfg.Host)
	known := filepath.Join(f.root, "known_hosts")
	if err := os.WriteFile(known, []byte(knownhosts.Line([]string{"test-host-key"}, f.hostKey)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(f.root, "ssh_config")
	// HostKeyAlias and an independent known-hosts file must be honored by real
	// OpenSSH, not approximated by runq's old subset of ssh -G output.
	config := "Host fixture\n HostName " + host + "\n Port " + port + "\n User researcher\n HostKeyAlias test-host-key\n UserKnownHostsFile " + known + "\n StrictHostKeyChecking yes\n IdentityAgent none\n IdentitiesOnly yes\n"
	if err := os.WriteFile(file, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := ResolveTargetSSH("fixture", 0, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	cfg.OpenSSH.Args = []string{"-F", file}
	cfg.OperationTimeout = 3 * time.Second
	return cfg
}

func TestSystemSSHConfigFilesTransfersAndReconnect(t *testing.T) {
	f := newFaultSSH(t)
	cfg := systemTestConfig(t, f)
	fs := NewSSHFS(cfg)
	defer fs.Close()
	file := filepath.Join(f.root, "payload")
	if err := fs.WriteFile(file, []byte("research data"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(file)
	if err != nil || string(data) != "research data" {
		t.Fatalf("read: %q %v", data, err)
	}
	_, _, code, err := fs.Exec(context.Background(), "exit 7")
	if err != nil || code != 7 {
		t.Fatalf("remote exit: %d %v", code, err)
	}
	// A changed config is read by the next master. No daemon restart is needed.
	fs.conn.CloseTransport()
	configFile := cfg.OpenSSH.Args[1]
	original, _ := os.ReadFile(configFile)
	_ = os.WriteFile(configFile, []byte(strings.ReplaceAll(string(original), "HostKeyAlias test-host-key", "HostKeyAlias wrong-host-key")), 0600)
	if _, err = fs.Home(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "host key verification failed") {
		t.Fatalf("host-key config ignored: %v", err)
	}
	_ = os.WriteFile(configFile, original, 0600)
	if _, err = fs.Home(); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	f.mode.Store(faultExec)
	_, _, _, err = fs.Exec(context.Background(), "submit-once")
	if !errors.Is(err, ErrExecInterrupted) {
		t.Fatalf("interrupted submission must remain unknown: %v", err)
	}
}

func TestSystemSSHForwardAndMasterShutdown(t *testing.T) {
	f := newFaultSSH(t)
	cfg := systemTestConfig(t, f)
	fw := NewRemoteForward(RemoteForwardConfig{SSH: cfg, SocketPath: filepath.Join(f.root, "forward/rq.sock"), Owner: "controller:target", Serve: func(ln net.Listener) error { return http.Serve(ln, http.NotFoundHandler()) }})
	go fw.Run(context.Background())
	defer fw.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && fw.Status().State != ForwardUp {
		time.Sleep(20 * time.Millisecond)
	}
	if fw.Status().State != ForwardUp {
		t.Fatalf("forward: %+v", fw.Status())
	}
	fs := NewSSHFS(cfg)
	defer fs.Close()
	conn, err := fs.DialUnix(context.Background(), fw.cfg.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	if !forwardResponds(conn) {
		t.Fatal("OpenSSH reverse forward did not carry HTTP")
	}
	fw.mu.Lock()
	master := fw.client.system
	fw.mu.Unlock()
	if err = fw.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-master.done:
	default:
		t.Fatal("master not joined")
	}
	if _, err = os.Stat(master.dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("control directory leaked: %v", err)
	}
}

func TestForwardRecoversOwnedUnresponsiveListener(t *testing.T) {
	f := newFaultSSH(t)
	public := filepath.Join(f.root, "recover/rq.sock")
	_ = os.MkdirAll(filepath.Dir(public), 0700)
	old, err := net.Listen("unix", public)
	if err != nil {
		t.Fatal(err)
	}
	old.(*net.UnixListener).SetUnlinkOnClose(false)
	defer old.Close()
	_ = os.WriteFile(public+".owner", []byte("c:t\n"), 0600)
	// Accepting but never forwarding a request models the pre-sleep sshd.
	accepted := make(chan net.Conn, 1)
	go func() {
		c, e := old.Accept()
		if e == nil {
			accepted <- c
		}
	}()
	cfg := f.cfg
	cfg.OperationTimeout = 4 * time.Second
	fw := NewRemoteForward(RemoteForwardConfig{SSH: cfg, SocketPath: public, Owner: "c:t", Serve: func(ln net.Listener) error { return http.Serve(ln, http.NotFoundHandler()) }})
	go fw.Run(context.Background())
	defer fw.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && fw.Status().State != ForwardUp {
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case c := <-accepted:
		_ = c.Close()
	default:
	}
	if fw.Status().State != ForwardUp {
		t.Fatalf("stuck behind zombie: %+v", fw.Status())
	}
	link, err := os.Readlink(public)
	if err != nil || !validForwardLink(public, link) {
		t.Fatalf("not a published generation: %q %v", link, err)
	}
	_ = old.Close()
	fs := NewSSHFS(cfg)
	defer fs.Close()
	conn, err := fs.DialUnix(context.Background(), public)
	if err != nil {
		t.Fatal(err)
	}
	if !forwardResponds(conn) {
		t.Fatal("old listener teardown broke replacement")
	}
}

func TestForwardFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct{ phase, message, reason string }{
		{"ssh", "Permission denied (publickey).", "authentication"},
		{"ssh", "Host key verification failed.", "host_key"},
		{"ssh", "Could not resolve hostname h", "dns"},
		{"socket_probe", "socket is already serving", "socket_conflict"},
		{"socket_probe", "empty socket owner record: /remote/rq.sock.owner", "socket"},
		{"socket_probe", "read socket owner record /remote/rq.sock.owner: connection lost", "socket"},
		{"forward_bind", "remote port forwarding failed", "forward"},
	} {
		reason, hint := forwardFailure(tc.phase, tc.message)
		if reason != tc.reason || hint == "" {
			t.Fatalf("diagnostic: %s %s", reason, hint)
		}
	}
	f := NewRemoteForward(RemoteForwardConfig{Owner: "c:t"})
	f.markUp()
	first := f.Status().LastOnline
	f.setPhase("ssh")
	f.markDown(errors.New("network offline"))
	since := f.Status().Since
	f.markDown(errors.New("still offline"))
	if st := f.Status(); st.Since != since || st.LastOnline != first || st.Attempts != 2 {
		t.Fatalf("outage state: %+v", st)
	}
	f.markUp()
	if st := f.Status(); st.LastError != "" || st.Attempts != 0 || st.NextRetryAt != 0 {
		t.Fatalf("recovery state: %+v", st)
	}
}

func TestSystemSSHProxyJump(t *testing.T) {
	target, jump := newFaultSSH(t), newFaultSSH(t)
	cfg := systemTestConfig(t, target)
	host, port, _ := net.SplitHostPort(jump.cfg.Host)
	configFile := cfg.OpenSSH.Args[1]
	data, _ := os.ReadFile(configFile)
	data = append(data, []byte(" ProxyJump jump\nHost jump\n HostName "+host+"\n Port "+port+"\n User researcher\n HostKeyAlias jump-key\n UserKnownHostsFile "+filepath.Join(target.root, "known_hosts")+"\n StrictHostKeyChecking yes\n IdentityAgent none\n")...)
	_ = os.WriteFile(configFile, data, 0600)
	known, _ := os.OpenFile(filepath.Join(target.root, "known_hosts"), os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = known.WriteString(knownhosts.Line([]string{"jump-key"}, jump.hostKey) + "\n")
	_ = known.Close()
	fs := NewSSHFS(cfg)
	defer fs.Close()
	if _, err := fs.Home(); err != nil {
		t.Fatalf("ProxyJump via system configuration: %v", err)
	}
}

func TestSystemSSHDeadMasterNeverFallsBack(t *testing.T) {
	f := newFaultSSH(t)
	cfg := systemTestConfig(t, f)
	transport, err := dialSSH(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	session, err := transport.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	// Lose the master between acquiring a session and starting its process.
	// Its lifecycle context is deliberately still live: ProxyCommand=false,
	// rather than cancellation alone, must prevent OpenSSH's fallback dial.
	_ = transport.system.cmd.Process.Kill()
	<-transport.system.done
	before := f.execRequests.Load()
	if err = session.Run("submit-once"); err == nil {
		t.Fatal("dead master accepted a command")
	}

	if f.execRequests.Load() != before {
		t.Fatal("session fell back to an independent connection")
	}
}

func TestSystemSSHFileTimeoutClosesMaster(t *testing.T) {
	f := newFaultSSH(t)
	cfg := systemTestConfig(t, f)
	f.mode.Store(faultFile)
	fs := NewSSHFS(cfg)
	defer fs.Close()
	if _, err := fs.conn.getConn(); err != nil {
		t.Fatal(err)
	}
	master := fs.conn.client.system
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := fs.WithContext(ctx).ReadFile(filepath.Join(f.root, "payload"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("file timeout: %v", err)
	}
	select {
	case <-master.done:
	case <-time.After(time.Second):
		t.Fatal("timeout did not terminate OpenSSH")
	}
	f.mode.Store(0)
	if _, err = fs.Home(); err != nil {
		t.Fatalf("timeout poisoned reconnect: %v", err)
	}
}

func TestForwardRecoversMissingGeneration(t *testing.T) {
	f := newFaultSSH(t)
	public := filepath.Join(f.root, "missing/rq.sock")
	_ = os.MkdirAll(filepath.Dir(public), 0700)
	_ = os.WriteFile(public+".owner", []byte("c:t\n"), 0600)
	_ = os.Symlink(filepath.Join(filepath.Dir(public), ".f-0123456789ab"), public)
	fw := NewRemoteForward(RemoteForwardConfig{SSH: f.cfg, SocketPath: public, Owner: "c:t", Serve: func(ln net.Listener) error { return http.Serve(ln, http.NotFoundHandler()) }})
	go fw.Run(context.Background())
	defer fw.Close()
	waitUp(t, fw)
}
