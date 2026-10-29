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

// Storage contract: runq's task directories always point into the task dir
// (submitplan rejects env that tries to redirect them); unrelated caches pass
// through untouched.
func TestTaskDirectoriesStayInTaskDirAndKeepCachesOutsideContract(t *testing.T) {
	root := t.TempDir()
	working := filepath.Join(root, "code")
	dir := filepath.Join(root, "task")
	env := TaskEnv(Identity{TaskID: "task", TaskDir: dir, WandbConfigured: true}, map[string]string{"RUNQ_OUTPUT_DIR": "results", "WANDB_DIR": "tracking", "WANDB_CACHE_DIR": filepath.Join(root, "cache")}, working)
	if env["RUNQ_OUTPUT_DIR"] != OutputsDir(dir) || env["WANDB_DIR"] != dir || env["RUNQ_CHECKPOINT_DIR"] != CheckpointsDir(dir) {
		t.Fatalf("runq directories must stay in the task dir %+v", env)
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

// runq's W&B group and tags are defaults: environment: sets them if it
// wants; runq's own directories still always win.
func TestTaskEnvKeepsConfiguredWandbGroup(t *testing.T) {
	id := Identity{TaskID: "t", TaskDir: "/d", JobName: "ablation", SweepKeys: []string{"lr"}}
	env := TaskEnv(id, map[string]string{"WANDB_RUN_GROUP": "paper", "WANDB_TAGS": "x", "WANDB_DIR": "elsewhere"}, "/w")
	if env["WANDB_RUN_GROUP"] != "paper" || env["WANDB_TAGS"] != "x" || env["WANDB_DIR"] != "/d" {
		t.Fatalf("env = %v", env)
	}
	if env := TaskEnv(id, nil, "/w"); env["WANDB_RUN_GROUP"] != "ablation" || env["WANDB_TAGS"] != "lr" {
		t.Fatalf("defaults = %v", env)
	}
}
