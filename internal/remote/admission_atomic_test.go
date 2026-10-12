package remote

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/job"
)

func TestPrepareRollsBackWholeSweepWhenAnyTaskInsertFails(t *testing.T) {
	cfg := &config.TargetConfig{
		Name: "hpc", SubmitTemplate: "submit {{run_sh}}",
		SubmitIDRegex: `job ([0-9]+)`,
	}
	b, proj, jobCfg := newTestBackend(t, cfg, func(context.Context, string) (string, error) {
		return "", nil
	})
	jobCfg.Sweep = []job.SweepBlock{{
		Method: "grid",
		Parameters: map[string]job.ParameterSpec{
			"lr": {Values: []any{0.1, 0.2}},
		},
	}}
	if _, err := b.Store.DB().Exec(`
		CREATE TRIGGER reject_second_task
		BEFORE INSERT ON tasks
		WHEN (SELECT COUNT(*) FROM tasks WHERE job_id = NEW.job_id) > 0
		BEGIN SELECT RAISE(ABORT, 'injected second task failure'); END;
	`); err != nil {
		t.Fatal(err)
	}

	jobID, rows, err := b.Prepare(context.Background(), jobCfg, proj, SubmitOpts{SkipPreflight: true})
	if err == nil || !strings.Contains(err.Error(), "persist job and tasks atomically") {
		t.Fatalf("Prepare error = %v, want atomic persistence failure", err)
	}
	if jobID == "" {
		t.Fatal("Prepare did not preserve the rejected plan id for diagnostics")
	}
	if len(rows) != 0 {
		t.Fatalf("returned rows after rejected admission = %d, want 0", len(rows))
	}
	for table, want := range map[string]int{"jobs": 0, "tasks": 0} {
		var got int
		if err := b.Store.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s rows after rollback = %d, want %d", table, got, want)
		}
	}
}

func TestV2PreparePersistsEffectiveSnapshot(t *testing.T) {
	cfg := &config.TargetConfig{Name: "hpc", SubmitTemplate: "submit {{run_sh}}", SubmitIDRegex: `job ([0-9]+)`}
	b, proj, input := newTestBackend(t, cfg, func(context.Context, string) (string, error) { return "", nil })
	input.Command = "echo {{lr}}"
	input.Target = "hpc"
	input.TargetSource = "cli"
	jobID, rows, err := b.Prepare(context.Background(), input, proj, SubmitOpts{SkipPreflight: true})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := b.Store.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	var recorded job.JobConfig
	if err := json.Unmarshal([]byte(saved.ConfigJSON), &recorded); err != nil {
		t.Fatal(err)
	}
	if recorded.Command != input.Command || recorded.Target != "hpc" || recorded.TargetSource != "cli" || recorded.EffectiveConfig == nil || recorded.EffectiveConfig.CmdTemplate != input.Command || !strings.HasPrefix(rows[0].Command, "echo ") {
		t.Fatalf("snapshot mismatch: %+v", recorded)
	}
}
