package config

import "testing"

// An alias and an omitted user name the same filesystem once OpenSSH
// resolves them; without a resolver the configured values are used.
func TestFilesystemID(t *testing.T) {
	resolve := func(host string, port int, user, jump string) (string, string, int, bool) {
		if user == "" {
			user = "me"
		}
		return user, "login1.example.org", 22, true
	}
	a := TargetConfig{SSH: &SSHTargetConfig{Host: "hpc"}}
	b := TargetConfig{SSH: &SSHTargetConfig{Host: "hpc", User: "me", Port: 22}}
	if x, y := a.FilesystemID("c", resolve), b.FilesystemID("c", resolve); x != y || x != "ssh:me@login1.example.org:22" {
		t.Fatalf("%q vs %q", x, y)
	}
	if got := b.FilesystemID("c", nil); got != "ssh:me@hpc:22" {
		t.Fatalf("unresolved = %q", got)
	}
	if got := (TargetConfig{}).FilesystemID("c1", resolve); got != "local:c1" {
		t.Fatalf("local = %q", got)
	}
}
