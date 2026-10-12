package workspace

import (
	"path/filepath"
	"testing"
)

func TestBaseSharedKeys(t *testing.T) {
	dir := t.TempDir()
	env := BaseEnv(
		Identity{TaskID: "t1", JobID: "j1", Project: "p1", TaskDir: dir},
	)

	want := map[string]string{
		"RUNQ_TASK_ID":        "t1",
		"RUNQ_JOB_ID":         "j1",
		"RUNQ_PROJECT_NAME":   "p1",
		"RUNQ_TASK_DIR":       dir,
		"RUNQ_PARAMS_FILE":    filepath.Join(dir, "params.json"),
		"RUNQ_METRICS_FILE":   filepath.Join(dir, "metrics.jsonl"),
		"RUNQ_CHECKPOINT_DIR": filepath.Join(dir, "checkpoints"),
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("env[%q] = %q, want %q", k, env[k], v)
		}
	}

	// Backend-specific keys must NOT be set by Base.
	for _, k := range []string{"RUNQ_SOCKET_PATH", "RUNQ_NO_DAEMON"} {
		if _, ok := env[k]; ok {
			t.Errorf("Base must not set %q (backend layers it on)", k)
		}
	}
}

func TestBaseWandbFollowsPlanWithoutLocalFile(t *testing.T) {
	dir := t.TempDir()

	// No wandb file → key absent.
	if _, ok := BaseEnv(Identity{TaskDir: dir})["RUNQ_WANDB_CONFIG_FILE"]; ok {
		t.Fatal("RUNQ_WANDB_CONFIG_FILE should be absent when the file does not exist")
	}

	// A target path absent on this machine still receives its config signal.
	wandb := filepath.Join(dir, "wandb_config.json")
	if got := BaseEnv(Identity{TaskDir: dir, WandbConfigured: true})["RUNQ_WANDB_CONFIG_FILE"]; got != wandb {
		t.Fatalf("remote config signal lost: %q", got)
	}
}

func TestBaseNoTaskDirOmitsPaths(t *testing.T) {
	env := BaseEnv(Identity{TaskID: "t", JobID: "j", Project: "p"})
	for _, k := range []string{"RUNQ_TASK_DIR", "RUNQ_PARAMS_FILE", "RUNQ_METRICS_FILE", "RUNQ_CHECKPOINT_DIR"} {
		if _, ok := env[k]; ok {
			t.Errorf("path var %q should be omitted when TaskDir is empty", k)
		}
	}
	// Identity still present.
	if env["RUNQ_TASK_ID"] != "t" {
		t.Errorf("identity vars missing: %#v", env)
	}
}
