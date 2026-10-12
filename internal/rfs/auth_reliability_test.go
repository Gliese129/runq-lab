package rfs

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func startTestAgent(t *testing.T, socket string, key ed25519.PrivateKey) func() {
	t.Helper()
	ring := agent.NewKeyring()
	if err := ring.Add(agent.AddedKey{PrivateKey: key}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var clients []net.Conn
	var wg sync.WaitGroup
	var once sync.Once
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			clients = append(clients, conn)
			mu.Unlock()
			wg.Add(1)
			go func() { defer wg.Done(); defer conn.Close(); _ = agent.ServeAgent(ring, conn) }()
		}
	}()
	stop := func() {
		once.Do(func() {
			_ = listener.Close()
			mu.Lock()
			for _, conn := range clients {
				_ = conn.Close()
			}
			mu.Unlock()
			wg.Wait()
		})
	}
	t.Cleanup(stop)
	return stop
}

func TestAgentRestartAndExplicitSocketBinding(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "rq-agent-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketA, socketB := filepath.Join(dir, "a.sock"), filepath.Join(dir, "b.sock")
	_, keyA, _ := ed25519.GenerateKey(rand.Reader)
	_, keyB, _ := ed25519.GenerateKey(rand.Reader)
	pubA, _ := ssh.NewPublicKey(keyA.Public())
	var authCount atomic.Int32
	f := newFaultSSH(t, func(cfg *ssh.ServerConfig) {
		cfg.NoClientAuth = false
		cfg.PublicKeyCallback = func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) != string(pubA.Marshal()) {
				return nil, errors.New("wrong agent identity")
			}
			authCount.Add(1)
			return nil, nil
		}
	})
	stopA := startTestAgent(t, socketA, keyA)
	t.Setenv("SSH_AUTH_SOCK", socketA)
	methods, err := ResolveAuthMethods("")
	if err != nil {
		t.Fatal(err)
	}
	cfg := f.cfg
	cfg.AuthMethods = methods
	fs := NewSSHFS(cfg)
	defer fs.Close()
	if _, err := fs.Home(); err != nil {
		t.Fatalf("initial agent auth: %v", err)
	}
	stopA()
	fs.conn.CloseTransport()
	stopA = startTestAgent(t, socketA, keyA)
	if _, err := fs.Home(); err != nil {
		t.Fatalf("agent restart poisoned auth callback: %v", err)
	}
	// A later shell chooses another working agent. The previously resolved
	// daemon config must still use A rather than silently selecting B.
	startTestAgent(t, socketB, keyB)
	t.Setenv("SSH_AUTH_SOCK", socketB)
	fs.conn.CloseTransport()
	if _, err := fs.Home(); err != nil {
		t.Fatalf("auth unexpectedly followed changed shell environment: %v", err)
	}
	stopA()
	fs.conn.CloseTransport()
	if _, err := fs.Home(); err == nil {
		t.Fatal("missing explicitly selected agent silently fell back to another socket")
	}
	if authCount.Load() < 3 {
		t.Fatalf("fresh SSH authentication not exercised: %d", authCount.Load())
	}
	// Cancellation with no agent remains bounded and cannot start a session.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := fs.Exec(ctx, "must-not-run"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquisition: %v", err)
	}
	if f.execRequests.Load() != 0 {
		t.Fatal("authentication failure/cancel executed a command")
	}
}
