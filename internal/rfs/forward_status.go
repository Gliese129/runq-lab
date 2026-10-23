package rfs

import (
	"strings"
	"time"
)

func sshTransportName(cfg SSHConfig) string {
	if cfg.OpenSSH != nil {
		return "openssh"
	}
	return "native"
}
func (f *RemoteForward) setPhase(phase string) {
	f.stMu.Lock()
	defer f.stMu.Unlock()
	if f.st.State == ForwardClosed {
		return
	}
	f.st.Phase = phase
	f.st.NextRetryAt = 0
	if phase == "ssh" {
		f.st.LastAttemptAt = time.Now().Unix()
	}
}
func forwardFailure(phase, message string) (string, string) {
	m := strings.ToLower(message)
	switch {
	case strings.Contains(m, "permission denied ("), strings.Contains(m, "unable to authenticate"), strings.Contains(m, "agent refused"), strings.Contains(m, "sign_and_send_pubkey"):
		return "authentication", "Run the debug command in the service's environment. Unlock the key in ssh-agent; if SSH_AUTH_SOCK changed, restart runq from that shell. Password/MFA prompts require an interactive terminal."
	case strings.Contains(m, "host key verification failed"), strings.Contains(m, "host identification has changed"), strings.Contains(m, "unknown host key"):
		return "host_key", "Verify the host fingerprint using system ssh. runq uses the same known-hosts policy and files."
	case strings.Contains(m, "could not resolve hostname"), strings.Contains(m, "no such host"), strings.Contains(m, "name or service not known"):
		return "dns", "Check DNS and VPN connectivity; run the debug command after the network has recovered."
	case strings.Contains(m, "empty socket owner record"):
		return "socket", "The remote socket owner file is empty, possibly from an interrupted older runq write. Verify the controller identity and socket liveness before repairing the record."
	case strings.Contains(m, "already serving"), strings.Contains(m, "another session"), strings.Contains(m, "ownership"), strings.Contains(m, "unrecognized socket"):
		return "socket_conflict", "SSH connected, but the remote CLI endpoint is owned or still responding. Check for another controller; do not delete an unverified socket."
	case strings.Contains(m, "bad configuration"), strings.Contains(m, "bad configuration option"), strings.Contains(m, "no such identity"), strings.Contains(m, "unsupported option"):
		return "configuration", "Run the debug command and inspect ssh -G for this host alias."
	case strings.Contains(m, "timeout"), strings.Contains(m, "timed out"), strings.Contains(m, "deadline exceeded"):
		return "timeout", "Check network/VPN and the reported phase. A socket probe timeout differs from an SSH authentication timeout."
	case phase == "forward_bind":
		return "forward", "SSH connected; check Unix socket forwarding permission, the remote directory and the raw OpenSSH error."
	case phase == "socket_probe":
		return "socket", "SSH connected; check SFTP access, remote socket ownership and directory permissions."
	default:
		return "network", "Check the raw error and debug command. The service retries automatically; no submitted command is replayed."
	}
}
