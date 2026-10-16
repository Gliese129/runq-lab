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

// ResolveAuthMethods builds the auth chain (RQ-45): the configured key
// file and the ssh-agent's identities, offered through ONE publickey
// method — golang.org/x/crypto/ssh tries each method name once, so a
// second PublicKeys method after an agent that holds no matching key was
// never attempted ("attempted methods [none publickey]") even though
// plain ssh logged in with the same file. Signing via the agent stays in
// the agent; an encrypted key file without an agent is a clear,
// actionable error, not a parse failure.
func ResolveAuthMethods(keyPath string, agentSocket ...string) ([]ssh.AuthMethod, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if len(agentSocket) > 0 {
		sock = agentSocket[0]
	}

	var fileSigner ssh.Signer
	if keyPath != "" {
		if fi, err := os.Stat(keyPath); err == nil && fi.Mode().Perm()&0o077 != 0 {
			slog.Warn("ssh key file permissions too open (ssh convention is 0600)", "key", keyPath, "mode", fi.Mode().Perm().String())
		}
		keyBytes, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("read key %q: %w", keyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(keyBytes)
		var passErr *ssh.PassphraseMissingError
		switch {
		case err == nil:
			fileSigner = signer
		case errors.As(err, &passErr) && sock != "":
			// Agent is available — the encrypted file is fine to skip.
			slog.Debug("key file is passphrase-protected; relying on ssh-agent", "key", keyPath)
		case errors.As(err, &passErr):
			return nil, fmt.Errorf("key %q is passphrase-protected — add it to ssh-agent (`ssh-add %s`); the daemon does not prompt for passphrases", keyPath, keyPath)
		default:
			return nil, fmt.Errorf("parse key %q: %w", keyPath, err)
		}
	}

	if sock == "" && fileSigner == nil {
		return nil, fmt.Errorf("no usable ssh auth: SSH_AUTH_SOCK not set and no key file configured")
	}
	if sock == "" {
		return []ssh.AuthMethod{ssh.PublicKeys(fileSigner)}, nil
	}
	return []ssh.AuthMethod{ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
		// The configured file first (what IdentityFile names), then the
		// agent's other identities. An unreachable or empty agent is not
		// fatal while the file can still sign.
		var signers []ssh.Signer
		if fileSigner != nil {
			signers = append(signers, fileSigner)
		}
		agentSigners, err := listAgent(sock)
		if err != nil {
			if fileSigner == nil {
				return nil, err
			}
			slog.Debug("ssh-agent unavailable; using key file", "sock", sock, "error", err)
		}
		for _, sg := range agentSigners {
			if fileSigner != nil && string(sg.PublicKey().Marshal()) == string(fileSigner.PublicKey().Marshal()) {
				continue
			}
			signers = append(signers, sg)
		}
		return signers, nil
	})}, nil
}

func listAgent(sock string) ([]ssh.Signer, error) {
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
