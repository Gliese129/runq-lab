package backend

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/consumer"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
)

func TestReconcileConsumerRestoresRestartAndConfirmedExit(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	target := config.TargetConfig{
		Name: "local", Workspace: t.TempDir(), SubmitTemplate: "true", MaxInflight: 1,
	}
	lane, err := NewSSHBackend(SSHBackendConfig{
		Target: target, Store: st, GlobalCfg: &config.GlobalConfig{}, FS: rfs.NewLocalFS(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.InsertJob(ctx, &store.JobRow{
		ID: "job", ProjectName: "project", ConfigJSON: "{}", Status: "done",
		TotalTasks: 1, Target: target.Name, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(target.Workspace, "job", "task")
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertTask(ctx, &store.TaskRow{
		ID: "task", JobID: "job", ProjectName: "project", Command: "true",
		ParamsJSON: "{}", Status: "success", Target: target.Name,
		TargetGeneration: lane.Generation(), TaskDir: taskDir, EnqueuedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(target.Workspace, "job", "consumers", "score-consumer")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	c := consumer.Consumer{
		ID: "consumer", JobID: "job", Name: "score", Command: "python score.py",
		Target: target.Name, Generation: lane.Generation(), Dir: dir,
		Status: "stopped", FinalIngest: true,
	}
	if err := st.InsertConsumer(ctx, &c); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateConsumer(ctx, &c); err != nil {
		t.Fatal(err)
	}
	writeStatus := func(state consumer.Consumer) {
		t.Helper()
		raw, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "status.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now().Unix()
	writeStatus(consumer.Consumer{Status: "running", PID: 101, PGID: 101, LastHeartbeatAt: &now})
	lane.reconcileConsumers(ctx)
	got, err := st.GetConsumer(ctx, c.ID)
	if err != nil || got == nil {
		t.Fatalf("GetConsumer: %v", err)
	}
	if got.Status != "running" || got.FinalIngest || got.ProcessExited {
		t.Fatalf("restart observation did not reopen consumer sync: %+v", got)
	}

	writeStatus(consumer.Consumer{Status: "lost", PID: 101, PGID: 101, ProcessExited: true})
	lane.reconcileConsumers(ctx)
	got, err = st.GetConsumer(ctx, c.ID)
	if err != nil || got == nil {
		t.Fatalf("GetConsumer after exit: %v", err)
	}
	if got.Status != "lost" || !got.ProcessExited || !got.FinalIngest {
		t.Fatalf("confirmed process exit did not finalize consumer sync: %+v", got)
	}
}
