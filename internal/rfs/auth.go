package rfs

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// ResolveAuthMethods builds the ordered auth chain (RQ-45): ssh-agent
// first when SSH_AUTH_SOCK is present — signing stays in the agent, the
// daemon never reads key plaintext — then the configured key file. An
// encrypted key file without an agent is a clear, actionable error, not
// a parse failure.
func ResolveAuthMethods(keyPath string, agentSocket ...string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	sock := os.Getenv("SSH_AUTH_SOCK")
	if len(agentSocket) > 0 {
		sock = agentSocket[0]
	}
	if sock != "" {
		methods = append(methods, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			conn, err := dialAgent(sock)
			if err != nil {
				return nil, err
			}
			defer conn.Close()
			keys, err := agent.NewClient(conn).List()
			if err != nil {
				return nil, err
			}
			var signers []ssh.Signer
			for _, key := range keys {
				pub, err := ssh.ParsePublicKey(key.Blob)
				if err != nil {
					return nil, err
				}
				signers = append(signers, &reconnectingAgentSigner{sock: sock, pub: pub})
			}
			return signers, nil
		}))
	}

	if keyPath != "" {
		if fi, err := os.Stat(keyPath); err == nil && fi.Mode().Perm()&0o077 != 0 {
			slog.Warn("ssh key file permissions too open (ssh convention is 0600)", "key", keyPath, "mode", fi.Mode().Perm().String())
		}
		keyBytes, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("read key %q: %w", keyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(keyBytes)
		if err != nil {
			var passErr *ssh.PassphraseMissingError
			if errors.As(err, &passErr) {
				if len(methods) > 0 {
					// Agent is available — the encrypted file is fine to skip.
					slog.Debug("key file is passphrase-protected; relying on ssh-agent", "key", keyPath)
					return methods, nil
				}
				return nil, fmt.Errorf("key %q is passphrase-protected — add it to ssh-agent (`ssh-add %s`); the daemon does not prompt for passphrases", keyPath, keyPath)
			}
			return nil, fmt.Errorf("parse key %q: %w", keyPath, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("no usable ssh auth: SSH_AUTH_SOCK not set and no key file configured")
	}
	return methods, nil
}

func dialAgent(sock string) (net.Conn, error) {
	conn, err := net.DialTimeout("unix", sock, 5*time.Second)
	if err == nil {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	}
	return conn, err
}

type reconnectingAgentSigner struct {
	sock string
	pub  ssh.PublicKey
}

func (s *reconnectingAgentSigner) PublicKey() ssh.PublicKey { return s.pub }
func (s *reconnectingAgentSigner) Sign(_ io.Reader, data []byte) (*ssh.Signature, error) {
	return s.SignWithAlgorithm(nil, data, "")
}
func (s *reconnectingAgentSigner) SignWithAlgorithm(_ io.Reader, data []byte, algorithm string) (*ssh.Signature, error) {
	conn, err := dialAgent(s.sock)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var flags agent.SignatureFlags
	switch algorithm {
	case ssh.KeyAlgoRSASHA256:
		flags = agent.SignatureFlagRsaSha256
	case ssh.KeyAlgoRSASHA512:
		flags = agent.SignatureFlagRsaSha512
	}
	return agent.NewClient(conn).SignWithFlags(s.pub, data, flags)
}
