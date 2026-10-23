package rfs

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// ResolveTargetSSH retains the alias and explicit overrides. OpenSSH resolves
// configuration and authentication on every reconnect; resolving the alias in
// advance would lose Host/Match, ProxyJump and host-key store semantics.
func ResolveTargetSSH(host string, port int, user, key, jump string) (SSHConfig, error) {
	if host == "" || strings.HasPrefix(host, "-") || strings.ContainsAny(host, "\x00\r\n") {
		return SSHConfig{}, fmt.Errorf("invalid SSH host %q", host)
	}
	if h, p, err := net.SplitHostPort(host); err == nil {
		host = h
		if port == 0 {
			port, err = strconv.Atoi(p)
			if err != nil {
				return SSHConfig{}, fmt.Errorf("invalid SSH port %q", p)
			}
		}
	}
	if port < 0 || port > 65535 {
		return SSHConfig{}, fmt.Errorf("invalid SSH port %d", port)
	}
	binary, err := exec.LookPath("ssh")
	if err != nil {
		return SSHConfig{}, fmt.Errorf("system OpenSSH is required: %w", err)
	}
	var args []string
	if port != 0 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	if user != "" {
		args = append(args, "-l", user)
	}
	if key != "" {
		args = append(args, "-i", key)
	}
	if jump != "" {
		args = append(args, "-J", jump)
	}
	return SSHConfig{Host: host, User: user, OpenSSH: &OpenSSHConfig{Binary: binary, Args: args, Host: host}}, nil
}

// DebugCommand reproduces fresh noninteractive authentication using the alias
// and overrides, bypassing existing control masters. No credentials are printed.
func (c SSHConfig) DebugCommand() string {
	if c.OpenSSH == nil {
		return ""
	}
	args := append([]string{"-vvv", "-T", "-o", "BatchMode=yes", "-o", "ControlMaster=no", "-o", "ControlPersist=no", "-S", "none", "-o", "RemoteCommand=none", "-o", "ClearAllForwardings=yes"}, c.OpenSSH.Args...)
	args = append(args, c.OpenSSH.Host, "true")
	return buildCmdLine(c.OpenSSH.Binary, args)
}
