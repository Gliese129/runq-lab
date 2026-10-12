package remote

import (
	"context"
	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/submitplan"
	"github.com/gliese129/runq-lab/internal/workspace"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteWandbConfigDoesNotDependOnControllerStat(t *testing.T) {
	b := &Backend{Cfg: &config.TargetConfig{Scheduler: "sge"}}
	task := submitplan.PlannedTask{TaskID: "t", TaskDir: "/remote-only/rc3/t", LogPath: "/remote-only/rc3/t.log", WorkingDir: "/remote/code", Command: "true", Env: map[string]string{"WANDB_DIR": "/remote/explicit"}}
	plan := submitplan.Plan{JobID: "j", Wandb: &project.WandbConfig{Project: "experiments", Entity: "lab", Mode: "offline"}}
	script := b.buildRunScript(task, plan)
	for _, line := range []string{"export RUNQ_WANDB_CONFIG_FILE='/remote-only/rc3/t/wandb_config.json'", "export WANDB_DIR='/remote/explicit'", "export RUNQ_OUTPUT_DIR='/remote-only/rc3/t/outputs'"} {
		if !strings.Contains(script, line) {
			t.Fatalf("missing remote contract %q", line)
		}
	}
}
func TestWandbPreviewPrepareAndRetryDirectoryContract(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	root := t.TempDir()
	working := filepath.Join(root, "code")
	if err = os.MkdirAll(working, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.TargetConfig{Name: "fixture", Scheduler: "sge", Workspace: filepath.Join(root, "workspace"), SubmitTemplate: "echo 55", SubmitIDRegex: "([0-9]+)", KillTemplate: "true"}
	b := &Backend{Cfg: cfg, Store: st, FS: rfs.NewLocalFS()}
	proj := &project.Config{ProjectName: "p", WorkingDir: working, CmdTemplate: "true", Wandb: &project.WandbConfig{Project: "wp", Mode: "offline"}}
	jcfg := job.JobConfig{Project: "p", Overrides: &job.Overrides{Env: map[string]string{"WANDB_DIR": "tracking", "RUNQ_OUTPUT_DIR": "results"}}}
	_, plan, err := b.CompilePreview(ctx, jcfg, proj, true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tasks[0].WandbDir != filepath.Join(working, "tracking") || plan.Tasks[0].OutputDir != filepath.Join(working, "results") {
		t.Fatalf("preview wrong %+v", plan.Tasks[0])
	}
	if _, err = os.Stat(cfg.Workspace); !os.IsNotExist(err) {
		t.Fatal("preview materialized task dirs")
	}
	_, rows, err := b.Prepare(ctx, jcfg, proj, SubmitOpts{SkipPreflight: true})
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	script, err := os.ReadFile(filepath.Join(row.TaskDir, "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), "export WANDB_DIR='"+plan.Tasks[0].WandbDir+"'") {
		t.Fatal("prepare disagrees with preview override")
	}
	for _, dir := range []string{filepath.Join(working, "tracking"), filepath.Join(working, "results"), workspace.CheckpointsDir(row.TaskDir)} {
		if _, err = os.Stat(dir); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{filepath.Join(working, "tracking", "offline.bin"), filepath.Join(working, "results", "result.json"), filepath.Join(workspace.CheckpointsDir(row.TaskDir), "model.pt")} {
		if err = os.WriteFile(file, []byte("retain"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = b.ResetWrapperState(ctx, row.TaskDir, row.ID); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{filepath.Join(working, "tracking", "offline.bin"), filepath.Join(working, "results", "result.json"), filepath.Join(workspace.CheckpointsDir(row.TaskDir), "model.pt")} {
		if _, err = os.Stat(file); err != nil {
			t.Fatalf("retry deleted %s", file)
		}
	}
}
