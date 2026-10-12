package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestControllerIdentityAndTargetIsolation(t *testing.T) {
	dir := t.TempDir()
	id, err := ControllerID(dir)
	if err != nil {
		t.Fatal(err)
	}
	same, err := ControllerID(dir)
	if err != nil || same != id {
		t.Fatal("identity changed")
	}
	info, _ := os.Stat(filepath.Join(dir, "controller-id"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("identity permissions")
	}
	other, err := ControllerID(t.TempDir())
	if err != nil || other == id {
		t.Fatal("controllers collide")
	}
	if RemoteSocketPath(id, "a") == RemoteSocketPath(id, "b") || RemoteSocketPath(id, "a") == RemoteSocketPath(other, "a") {
		t.Fatal("socket paths collide")
	}
}
