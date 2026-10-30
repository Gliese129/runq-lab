package remote

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/store"
)

// An ssh alias that now resolves to another host keeps the generation but
// not the location: the same path on the new host is someone else's file,
// so the lane neither settles the old version nor ingests what it finds.
func TestReconcileSkipsVersionsOnAnotherFilesystem(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	dir := t.TempDir()
	jobID, taskID := seedTask(t, st, dir, "running", SourceScheduler)
	if _, err := st.DB().ExecContext(ctx, `UPDATE tasks SET target='hpc', target_generation='g1', location='fs-old' WHERE id=?`, taskID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, statusFileName),
		[]byte(`{"status":"success","exit_code":0,"finished_at":1730000000}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metrics.jsonl"), []byte(`{"step":1,"acc":0.9}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scope := store.NewLaneScope("hpc", "g1")
	scope.Location = "fs-new"
	b := &Backend{Cfg: &config.TargetConfig{Name: "hpc"}, Store: st, FS: newTestFSFromRunner(nopRunner), Scope: scope}

	if err := b.EnsureFresh(ctx, jobID, 0); err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if err := b.SchedulerProbe(ctx, 0); err != nil {
		t.Fatalf("SchedulerProbe: %v", err)
	}
	if silent, err := b.HeartbeatProbe(ctx, 0); err != nil || len(silent) != 0 {
		t.Fatalf("heartbeat = %v, %v", silent, err)
	}
	got, _ := st.GetTask(ctx, taskID)
	if got.Status != "running" || got.StatusSource != SourceScheduler {
		t.Fatalf("version on another filesystem settled: %s/%s", got.Status, got.StatusSource)
	}
	if keys, _ := st.MetricKeys(ctx, jobID); len(keys) != 0 {
		t.Fatalf("ingested another host's metrics: %v", keys)
	}
	if _, _, err := b.resolveTargets(ctx, taskID); err == nil {
		t.Fatal("kill resolved a version on another filesystem")
	}

	scope.Location = "fs-old" // back on its own filesystem: settled as usual
	if err := b.EnsureFresh(ctx, jobID, 0); err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if got, _ = st.GetTask(ctx, taskID); got.Status != "success" {
		t.Fatalf("own version not settled: %s", got.Status)
	}
}
