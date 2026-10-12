package rfs

import (
	"path/filepath"
	"strings"
)

// SSH settings for every connection site (lane, remote forward, connect) are
// resolved by ResolveTargetSSH via OpenSSH's own `ssh -G` (resolve.go), so
// aliases, Include and Match behave exactly as in the user's ssh. This file
// keeps the small shared pieces.

// Host key policy values consumed by sshConn/RemoteForward.
const (
	// HostKeyStrict is the default: unknown or changed keys refuse the
	// connection (typed errors naming the fix).
	HostKeyStrict = ""
	// HostKeyAcceptNew auto-trusts and records UNKNOWN hosts (no prompt);
	// a CHANGED key still hard-fails.
	HostKeyAcceptNew = "accept-new"
)

// expandHome resolves the "~/" prefix ssh_config values commonly carry.
func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
