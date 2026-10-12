package remote

import (
	"context"
	"encoding/json"
	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/submitplan"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWrapperTimeSurvivesShortRunSetupFailureAndTERM(t *testing.T) {
	for _, tc := range []struct {
		name, setup, command, status string
		executes                     bool
	}{
		{"short", "sleep 1", "true", "success", true},
		{"setup-fail", "false", "true", "failed", false},
		{"TERM", "", "kill -TERM $$", "killed", true},
		{"hard-exit", "", "exec true", "running", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			b := &Backend{Cfg: &config.TargetConfig{EnvSetup: tc.setup}}
			script := b.buildRunScript(submitplan.PlannedTask{TaskID: "t", TaskDir: dir, LogPath: filepath.Join(dir, "task.log"), WorkingDir: dir, Command: tc.command}, submitplan.Plan{JobID: "j"})
			_ = exec.Command("sh", "-c", script).Run()
			raw, err := os.ReadFile(filepath.Join(dir, statusFileName))
			if err != nil {
				t.Fatal(err)
			}
			var sf statusFile
			if err = json.Unmarshal(raw, &sf); err != nil {
				t.Fatal(err)
			}
			if sf.Status != tc.status || sf.WrapperStartedAt <= 0 || sf.Attempt == nil || *sf.Attempt != 0 {
				t.Fatalf("bad status %s", raw)
			}
			if tc.executes && sf.ExecutionStartedAt <= 0 {
				t.Fatalf("lost short-task entrance %s", raw)
			}
			if !tc.executes && sf.ExecutionStartedAt != 0 {
				t.Fatalf("setup failure marked executing %s", raw)
			}
			if tc.status != "running" && tc.executes && sf.ExecutionFinishedAt < sf.ExecutionStartedAt {
				t.Fatalf("lost finish %s", raw)
			}
			if tc.name == "short" && sf.ExecutionStartedAt <= sf.WrapperStartedAt {
				t.Fatalf("setup time was mixed into command %s", raw)
			}
		})
	}
}

func TestLateTimeRepairsHardTerminalWithoutReopening(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	jobID, taskID := seedTask(t, st, dir, "success", SourceWrapper)
	raw := []byte(`{"time_version":1,"attempt":0,"status":"failed","wrapper_started_at":100,"execution_started_at":110,"execution_finished_at":110}`)
	if err = os.WriteFile(filepath.Join(dir, statusFileName), raw, 0644); err != nil {
		t.Fatal(err)
	}
	b := &Backend{Cfg: &config.TargetConfig{}, Store: st, FS: newTestFSFromRunner(nopRunner)}
	if err = b.EnsureFresh(context.Background(), jobID, 0); err != nil {
		t.Fatal(err)
	}
	row, _ := st.GetTask(context.Background(), taskID)
	if row.Status != "success" || store.CurrentTaskTimes(*row)["execution_started_at"].At != 110 {
		t.Fatalf("late evidence lost or terminal reopened: %+v", row)
	}
	before := row.TimingJSON
	if err = b.EnsureFresh(context.Background(), jobID, 0); err != nil {
		t.Fatal(err)
	}
	row, _ = st.GetTask(context.Background(), taskID)
	if row.TimingJSON != before {
		t.Fatal("repeat read changed receipt time")
	}
	row.RetryCount = 1
	var sf statusFile
	_ = json.Unmarshal(raw, &sf)
	if len(observationTimeFields(*row, sf, ProbeResult{})) != 0 || statusForAttempt(*row, sf).Status != "" {
		t.Fatal("old attempt reused")
	}
}

func TestProbeTimesRejectsProjectedStartsAndNaiveTimezones(t *testing.T) {
	p := ProbeResult{Signal: SchedPending}
	parseProbeTimes(&p, []string{"submitted_at=100", "started_at=200"})
	if p.SubmittedAt != 100 || p.StartedAt != 0 {
		t.Fatalf("projected start accepted %+v", p)
	}
	p.Signal = SchedRunning
	parseProbeTimes(&p, []string{"started_at=2026-10-12T12:00:00+09:00", "finished_at=300"})
	if p.StartedAt != 1791774000 || p.FinishedAt != 0 {
		t.Fatalf("bad zone/state parse %+v", p)
	}
	p = ProbeResult{Signal: SchedRunning}
	parseProbeTimes(&p, []string{"started_at=2026-10-12T12:00:00"})
	if p.StartedAt != 0 {
		t.Fatal("guessed remote timezone")
	}
}

func TestNativeProbeTimeFieldsInSingleAndBatchResults(t *testing.T) {
	b := &Backend{Cfg: &config.TargetConfig{StatusTemplate: "probe", StatusListTemplate: "list"}, FS: newTestFSFromRunner(func(_ context.Context, cmd string) (string, error) {
		if cmd == "list" {
			return "55 RUNNING gpu submitted_at=100 started_at=200", nil
		}
		return "COMPLETED submitted_at=100 started_at=200 finished_at=300", nil
	})}
	p, err := b.probeSchedulerObservedWith(context.Background(), b.shellRunClassified, "55")
	if err != nil || p.Signal != SchedSuccess || p.FinishedAt != 300 {
		t.Fatalf("single metadata lost %+v %v", p, err)
	}
	batch, err := b.probeBatch(context.Background())
	if err != nil || batch["55"].Queue != "gpu" || batch["55"].StartedAt != 200 {
		t.Fatalf("batch metadata lost %+v %v", batch, err)
	}
}
