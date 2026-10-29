package rfs

// SSH settings for every connection site (lane, remote forward, connect) are
// passed to the system OpenSSH executable by ResolveTargetSSH (resolve.go).
// The native transport helpers below remain for injected/test connections.

// Host key policy values consumed by sshConn/RemoteForward.
const (
	// HostKeyStrict is the default: unknown or changed keys refuse the
	// connection (typed errors naming the fix).
	HostKeyStrict = ""
	// HostKeyAcceptNew auto-trusts and records UNKNOWN hosts (no prompt);
	// a CHANGED key still hard-fails.
	HostKeyAcceptNew = "accept-new"
)
