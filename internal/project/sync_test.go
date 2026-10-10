package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gliese129/runq-lab/internal/rfs"
)

func writeSyncTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMirrorAllowListAndBangExclusions(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	writeSyncTestFile(t, source, SyncFile, "src/\nsrc/**\n*.py\n!src/private/\n!secret.py\n")
	writeSyncTestFile(t, source, "src/main.go", "new-main")
	writeSyncTestFile(t, source, "src/private/key.txt", "secret")
	writeSyncTestFile(t, source, "tools/build.py", "build")
	writeSyncTestFile(t, source, "secret.py", "nope")
	writeSyncTestFile(t, source, "README.md", "not selected")

	writeSyncTestFile(t, destination, "src/main.go", "old-main")
	writeSyncTestFile(t, destination, "src/stale.go", "stale")
	writeSyncTestFile(t, destination, "src/private/keep.txt", "keep")
	writeSyncTestFile(t, destination, "README.md", "destination-owned")

	copied, deleted, err := Mirror(source, rfs.NewLocalFS(), destination, rfs.NewLocalFS())
	if err != nil {
		t.Fatal(err)
	}
	if copied != 2 || deleted != 1 {
		t.Fatalf("copied/deleted = %d/%d, want 2/1", copied, deleted)
	}
	assertFile := func(name, want string) {
		t.Helper()
		got, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(name)))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
	assertFile("src/main.go", "new-main")
	assertFile("tools/build.py", "build")
	assertFile(SyncFile, "src/\nsrc/**\n*.py\n!src/private/\n!secret.py\n")
	assertFile("src/private/keep.txt", "keep")
	assertFile("README.md", "destination-owned")
	if _, err := os.Stat(filepath.Join(destination, "src/stale.go")); !os.IsNotExist(err) {
		t.Fatalf("stale selected file still exists: %v", err)
	}
}

func TestMirrorRequiresSyncFile(t *testing.T) {
	_, _, err := Mirror(t.TempDir(), rfs.NewLocalFS(), t.TempDir(), rfs.NewLocalFS())
	if err == nil {
		t.Fatal("Mirror succeeded without .rsync")
	}
}

func TestParseSyncRulesRejectsParentTraversal(t *testing.T) {
	if _, err := parseSyncRules([]byte("../outside\n")); err == nil {
		t.Fatal("parent traversal pattern accepted")
	}
}

func TestMirrorRejectsDestinationSymlinkInsideSelectedPath(t *testing.T) {
	source, destination, outside := t.TempDir(), t.TempDir(), t.TempDir()
	writeSyncTestFile(t, source, SyncFile, "src/**\n")
	writeSyncTestFile(t, source, "src/main.go", "source")
	writeSyncTestFile(t, outside, "main.go", "outside")
	if err := os.Symlink(outside, filepath.Join(destination, "src")); err != nil {
		t.Fatal(err)
	}
	_, _, err := Mirror(source, rfs.NewLocalFS(), destination, rfs.NewLocalFS())
	if err == nil {
		t.Fatal("Mirror followed a destination symlink")
	}
	got, readErr := os.ReadFile(filepath.Join(outside, "main.go"))
	if readErr != nil || string(got) != "outside" {
		t.Fatalf("outside file changed to %q, %v", got, readErr)
	}
}

// A directory include only permits traversal (rsync include-file semantics):
// the hand-written whitelists list /data/ next to /data/*.jsonl and must not
// pull every checkpoint under data/ along with it.
func TestMirrorDirectoryPatternOnlyPermitsTraversal(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	writeSyncTestFile(t, source, SyncFile, "/data/\n/data/*.jsonl\n")
	writeSyncTestFile(t, source, "data/train.jsonl", "rows")
	writeSyncTestFile(t, source, "data/model.safetensors", "weights")

	copied, _, err := Mirror(source, rfs.NewLocalFS(), destination, rfs.NewLocalFS())
	if err != nil {
		t.Fatal(err)
	}
	if copied != 1 {
		t.Fatalf("copied = %d, want 1", copied)
	}
	if _, err := os.Stat(filepath.Join(destination, "data/model.safetensors")); !os.IsNotExist(err) {
		t.Fatalf("directory pattern selected unlisted file: %v", err)
	}
}
