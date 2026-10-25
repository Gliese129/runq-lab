// env.go — the RUNQ_* environment half of the SDK contract (formerly
// package runqenv; merged: the env contract and the file contract are two
// halves of one SDK surface, and this package is that surface).
//
// BaseEnv returns only the variables both backends share. Backend-specific
// keys are layered on top by the caller:
//   - daemon adds RUNQ_SOCKET_PATH (so the SDK can reach the daemon)
//   - HPC adds RUNQ_NO_DAEMON=1 and sets no socket
//
// Path variables come from the layout helpers in workspace.go, so the
// on-disk file contract lives in exactly one place.

package workspace

import (
	"context"
	"fmt"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/utils"
	"strings"
)

// Identity is the task's stable identity plus its workspace directory.
type Identity struct {
	TaskID          string
	JobID           string
	Project         string
	TaskDir         string   // per-task workspace; when empty, path vars are omitted
	SweepKeys       []string // sweep parameter key names (for WANDB_TAGS + WANDB_RUN_NAME)
	WandbConfigured bool     // supplied by the plan, never inferred from the controller filesystem
	JobName         string   // job display name at submit (for WANDB_RUN_GROUP)
}

// BaseEnv returns the shared RUNQ_* variables. It does NOT set RUNQ_SOCKET_PATH or
// RUNQ_NO_DAEMON. RUNQ_WANDB_CONFIG_FILE follows the submitted plan, including remote workspaces.
func BaseEnv(id Identity) map[string]string {
	env := make(map[string]string, 10)
	env["RUNQ_TASK_ID"] = id.TaskID
	env["RUNQ_JOB_ID"] = id.JobID
	env["RUNQ_PROJECT_NAME"] = id.Project

	if id.TaskDir != "" {
		env["RUNQ_TASK_DIR"] = id.TaskDir
		env["RUNQ_PARAMS_FILE"] = ParamsPath(id.TaskDir)
		env["RUNQ_METRICS_FILE"] = MetricsPath(id.TaskDir)
		env["RUNQ_CHECKPOINT_DIR"] = CheckpointsDir(id.TaskDir)
		env["RUNQ_OUTPUT_DIR"] = OutputsDir(id.TaskDir)
		env["WANDB_DIR"] = id.TaskDir

		wandbCfg := WandbConfigPath(id.TaskDir)
		if id.WandbConfigured {
			env["RUNQ_WANDB_CONFIG_FILE"] = wandbCfg
		}
	}

	if len(id.SweepKeys) > 0 {
		env["RUNQ_SWEEP_KEYS"] = strings.Join(id.SweepKeys, ",")
		env["WANDB_TAGS"] = strings.Join(id.SweepKeys, ",")
	}
	if id.JobName != "" {
		env["RUNQ_JOB_NAME"] = id.JobName
		env["WANDB_RUN_GROUP"] = id.JobName
	}

	return env
}

// RunqDirVars are the task directories runq owns (storage contract);
// project/job env may not redirect them.
var RunqDirVars = []string{"RUNQ_OUTPUT_DIR", "RUNQ_CHECKPOINT_DIR", "WANDB_DIR"}

// TaskEnv is the single resolver for preview, persistence and run.sh: the
// caller's env plus runq's own variables, which always win.
func TaskEnv(id Identity, overrides map[string]string, workingDir string) map[string]string {
	env := make(map[string]string, len(overrides)+16)
	for key, value := range overrides {
		env[key] = value
	}
	delete(env, "RUNQ_WANDB_CONFIG_FILE")
	for key, value := range BaseEnv(id) {
		env[key] = value
	}
	return env
}

// EnsureTaskDirs operates on the actual target before external submission.
// W&B owns its child run directories; runq creates and checks the root only.
func EnsureTaskDirs(ctx context.Context, fsys rfs.FS, env map[string]string) error {
	if fsys == nil {
		fsys = rfs.NewLocalFS()
	}
	fsys = rfs.WithContext(fsys, ctx)
	seen := map[string]bool{}
	for _, key := range RunqDirVars {
		dir := env[key]
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		if err := fsys.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s %q on target: %w", key, dir, err)
		}
		// Check search/write permissions on the target using the same user as submit.
		_, stderr, code, err := fsys.Exec(ctx, "sh", "-c", "test -d "+utils.ShellQuote(dir)+" && test -w "+utils.ShellQuote(dir)+" && test -x "+utils.ShellQuote(dir))
		if err != nil || code != 0 {
			return fmt.Errorf("directory %s %q is not writable/searchable on target (exit %d): %v %s", key, dir, code, err, stderr)
		}
	}
	return nil
}
