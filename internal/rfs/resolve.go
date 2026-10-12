package rfs

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ResolveTargetSSH is shared by connect, lane operations, and forwarding.
// OpenSSH evaluates Include/Match and command-line precedence; unsupported
// transport options are rejected explicitly rather than silently bypassed.
func ResolveTargetSSH(host string, port int, user, key, jump string) (SSHConfig, error) {
	if host == "" || strings.HasPrefix(host, "-") {
		return SSHConfig{}, fmt.Errorf("invalid SSH host %q", host)
	}
	if h, p, err := net.SplitHostPort(host); err == nil {
		host = h
		if port == 0 {
			port, _ = strconv.Atoi(p)
		}
	}
	if jump != "" && jump != "none" {
		return SSHConfig{}, fmt.Errorf("ProxyJump is not supported by runq's SSH transport; configure a directly reachable endpoint or tunnel")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args := []string{"-G"}
	if port != 0 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	if user != "" {
		args = append(args, "-l", user)
	}
	if key != "" {
		args = append(args, "-i", key)
	}
	args = append(args, host)
	out, err := exec.CommandContext(ctx, "ssh", args...).Output()
	if err != nil {
		return SSHConfig{}, fmt.Errorf("resolve SSH configuration using ssh -G: %w", err)
	}
	values := map[string][]string{}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, " ")
		if ok {
			values[k] = append(values[k], strings.TrimSpace(v))
		}
	}
	first := func(k string) string {
		if len(values[k]) > 0 {
			return values[k][0]
		}
		return ""
	}
	for _, option := range []string{"proxyjump", "proxycommand"} {
		if v := first(option); v != "" && v != "none" {
			return SSHConfig{}, fmt.Errorf("SSH %s is not supported by runq's SSH transport; configure a directly reachable endpoint or tunnel", option)
		}
	}
	home, _ := os.UserHomeDir()
	host = first("hostname")
	user = first("user")
	p, err := strconv.Atoi(first("port"))
	if err != nil || p < 1 || p > 65535 {
		return SSHConfig{}, fmt.Errorf("invalid SSH port")
	}
	if key != "" {
		key = expandHome(key, home)
	} else {
		for _, file := range values["identityfile"] {
			file = expandHome(file, home)
			if _, err := os.Stat(file); err == nil {
				key = file
				break
			}
		}
	}
	sock := os.Getenv("SSH_AUTH_SOCK")
	if selected := first("identityagent"); selected != "" {
		if selected == "none" {
			sock = ""
		} else if selected != "SSH_AUTH_SOCK" {
			sock = expandHome(os.ExpandEnv(selected), home)
		}
	}
	auth, err := ResolveAuthMethods(key, sock)
	if err != nil {
		return SSHConfig{}, err
	}
	policy := HostKeyStrict
	switch first("stricthostkeychecking") {
	case "accept-new", "false", "no", "off":
		policy = HostKeyAcceptNew
	}
	// Custom host-key stores/aliases would otherwise change verification semantics.
	if alias := first("hostkeyalias"); alias != "" && alias != "none" {
		return SSHConfig{}, fmt.Errorf("SSH HostKeyAlias is not supported")
	}
	return SSHConfig{Host: net.JoinHostPort(host, strconv.Itoa(p)), User: user, AuthMethods: auth, HostKeyPolicy: policy}, nil
}
