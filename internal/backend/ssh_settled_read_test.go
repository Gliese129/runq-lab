package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
)

// Detail reads of a settled job must not pay the SSH freshness pass (it made
// every dashboard task page wait ~2s); live jobs still probe.
func TestSSHDetailReadsSkipProbeOnlyForSettledJobs(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "probed")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	be, err := NewSSHBackend(SSHBackendConfig{
		Target: config.TargetConfig{
			Name: "hpc", SubmitTemplate: "submit {{run_sh}}", SubmitIDRegex: `([0-9]+)`,
			StatusTemplate: "touch " + marker + "; echo running", MaxInflight: 1,
		},
		Store: st,
		FS:    rfs.NewLocalFS(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = be.Close() })
	ctx := context.Background()
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO projects (name, config_json) VALUES ('p', '{}')`); err != nil {
		t.Fatal(err)
	}
	seed := func(jobID, taskID, jobStatus, taskStatus string) {
		t.Helper()
		if err := st.InsertJob(ctx, &store.JobRow{
			ID: jobID, ProjectName: "p", Status: jobStatus, TotalTasks: 1, Target: "hpc", CreatedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
		if err := st.InsertTask(ctx, &store.TaskRow{
			ID: taskID, JobID: jobID, ProjectName: "p", Command: "true", ParamsJSON: "{}",
			Status: taskStatus, ExternalID: "42", Target: "hpc", TargetGeneration: be.Generation(),
			TaskDir: t.TempDir(), EnqueuedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	probed := func() bool { _, err := os.Stat(marker); return err == nil }

	seed("job-done", "task-done", "done", "success")
	if _, err := be.GetTask(ctx, "task-done"); err != nil {
		t.Fatal(err)
	}
	if _, err := be.GetJob(ctx, "job-done"); err != nil {
		t.Fatal(err)
	}
	if probed() {
		t.Fatal("settled job detail read probed the scheduler")
	}

	seed("job-live", "task-live", "running", "running")
	if _, err := be.GetTask(ctx, "task-live"); err != nil {
		t.Fatal(err)
	}
	if !probed() {
		t.Fatal("live job detail read skipped the freshness probe")
	}
}
