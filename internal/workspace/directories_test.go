package workspace

import (
	"context"
	"errors"
	"github.com/gliese129/runq-lab/internal/rfs"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskDirectoriesRespectOverridesAndKeepCachesOutsideContract(t *testing.T) {
	root := t.TempDir()
	working := filepath.Join(root, "code")
	dir := filepath.Join(root, "task")
	env := TaskEnv(Identity{TaskID: "task", TaskDir: dir, WandbConfigured: true}, map[string]string{"RUNQ_OUTPUT_DIR": "results", "RUNQ_CHECKPOINT_DIR": filepath.Join(root, "ckpts"), "WANDB_DIR": "tracking", "WANDB_CACHE_DIR": filepath.Join(root, "cache")}, working)
	if env["RUNQ_OUTPUT_DIR"] != filepath.Join(working, "results") || env["WANDB_DIR"] != filepath.Join(working, "tracking") {
		t.Fatalf("bad path resolution %+v", env)
	}
	if err := EnsureTaskDirs(context.Background(), rfs.NewLocalFS(), env); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"RUNQ_OUTPUT_DIR", "RUNQ_CHECKPOINT_DIR", "WANDB_DIR"} {
		if _, err := os.Stat(env[key]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(env["WANDB_CACHE_DIR"]); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("shared W&B cache was treated as task directory")
	}
	if _, err := os.Stat(WandbConfigPath(dir)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("directory resolution wrote a config")
	}
	// mkdir must fail before external submit when a selected path is a file.
	bad := filepath.Join(root, "file")
	_ = os.WriteFile(bad, []byte("x"), 0600)
	if err := EnsureTaskDirs(context.Background(), nil, map[string]string{"WANDB_DIR": bad}); err == nil {
		t.Fatal("invalid W&B root silently accepted")
	}
}
