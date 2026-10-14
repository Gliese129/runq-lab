package consumer

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type runnerState struct {
	Consumer
	RunID string `json:"run_id"`
}

func writeRunnerFixture(t *testing.T, code string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"runner.py":       Runner,
		"python_entry.py": PythonEntry,
		"business.py":     []byte(code),
		"command.sh":      []byte("exec python3 '" + filepath.Join(dir, "python_entry.py") + "' '" + filepath.Join(dir, "business.py") + "'"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runnerCommand(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("python3", append([]string{filepath.Join(dir, "runner.py")}, args...)...)
	cmd.Env = append(os.Environ(), "RUNQ_CONSUMER_DIR="+dir)
	return cmd
}

func waitRunnerState(t *testing.T, dir, status string) runnerState {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var state runnerState
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(filepath.Join(dir, "status.json"))
		_ = json.Unmarshal(raw, &state)
		if state.Status == status {
			return state
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("consumer did not reach %q: %+v", status, state)
	return runnerState{}
}

func writeRunnerState(t *testing.T, dir string, state runnerState) {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "status.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPythonExitStatus(t *testing.T) {
	for _, tc := range []struct {
		name, code, status string
		exit               int
	}{{"normal", "pass", "stopped", 0}, {"exit", "import sys; sys.exit(3)", "failed", 3}, {"exception", "raise RuntimeError('test')", "failed", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeRunnerFixture(t, tc.code)
			_ = runnerCommand(dir).Run()
			state := waitRunnerState(t, dir, tc.status)
			if !state.ProcessExited || state.ExitCode == nil || *state.ExitCode != tc.exit {
				t.Fatalf("wrong exit: %+v", state)
			}
		})
	}
}

func TestRestartPreservesDirectoryAndRejectsOldWatcher(t *testing.T) {
	code := `
import os
from pathlib import Path
root = Path(os.environ["RUNQ_CONSUMER_DIR"])
with (root / "log.txt").open("a") as f: f.write("run\n")
with (root / "results.jsonl").open("a") as f: f.write('{"ts":1,"axes":{},"metrics":{"seen":1}}\n')
`
	dir := writeRunnerFixture(t, code)
	if err := os.Mkdir(filepath.Join(dir, "outputs"), 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dir, "outputs", "checkpoint")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runnerCommand(dir).Run(); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first := waitRunnerState(t, dir, "stopped")
	if err := runnerCommand(dir).Run(); err != nil {
		t.Fatalf("restart: %v", err)
	}
	second := waitRunnerState(t, dir, "stopped")
	if first.RunID == "" || first.RunID == second.RunID {
		t.Fatalf("run identity was not renewed: first=%q second=%q", first.RunID, second.RunID)
	}
	for _, name := range []string{"log.txt", "results.jsonl"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || strings.Count(string(raw), "\n") != 2 {
			t.Fatalf("%s was not appended across restart: %q, %v", name, raw, err)
		}
	}
	if raw, err := os.ReadFile(sentinel); err != nil || string(raw) != "keep" {
		t.Fatalf("consumer output was not preserved: %q, %v", raw, err)
	}

	before, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	if output, err := runnerCommand(dir, "watch", first.RunID).CombinedOutput(); err != nil {
		t.Fatalf("old watcher exit: %v: %s", err, output)
	}
	after, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("old watcher changed the new run state: before=%s after=%s err=%v", before, after, err)
	}
}

func TestFreshRunRejectsDuplicateThenRestartsAfterCancel(t *testing.T) {
	dir := writeRunnerFixture(t, "import time; time.sleep(30)")
	firstCmd := runnerCommand(dir)
	if err := firstCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if firstCmd.Process != nil {
			_ = firstCmd.Process.Kill()
		}
	})
	first := waitRunnerState(t, dir, "running")
	if output, err := runnerCommand(dir).CombinedOutput(); err == nil || !strings.Contains(string(output), "heartbeat is fresh") {
		t.Fatalf("duplicate start = %v, %q; want fresh-heartbeat rejection", err, output)
	}
	if current := waitRunnerState(t, dir, "running"); current.RunID != first.RunID {
		t.Fatalf("duplicate start replaced active run: first=%q current=%q", first.RunID, current.RunID)
	}
	stale := time.Now().Add(-4 * time.Minute).Unix()
	first.LastHeartbeatAt = &stale
	writeRunnerState(t, dir, first)
	if output, err := runnerCommand(dir, "--confirm-stopped").CombinedOutput(); err == nil || !strings.Contains(string(output), "process group is still alive") {
		t.Fatalf("stale local process start = %v, %q; want live-group rejection", err, output)
	}
	if output, err := runnerCommand(dir, "cancel").CombinedOutput(); err != nil {
		t.Fatalf("cancel: %v: %s", err, output)
	}
	_ = firstCmd.Wait()
	killed := waitRunnerState(t, dir, "killed")
	if !killed.ProcessExited {
		t.Fatalf("cancelled process group was not confirmed gone: %+v", killed)
	}
	if err := os.WriteFile(filepath.Join(dir, "business.py"), []byte("pass"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runnerCommand(dir).Run(); err != nil {
		t.Fatalf("restart after cancel: %v", err)
	}
	restarted := waitRunnerState(t, dir, "stopped")
	if restarted.RunID == killed.RunID {
		t.Fatalf("cancelled run identity was reused: %q", restarted.RunID)
	}
	if _, err := os.Stat(filepath.Join(dir, "cancel.request")); !os.IsNotExist(err) {
		t.Fatalf("stale cancellation survived restart: %v", err)
	}
}

func TestCrossHostRestartRequiresStaleHeartbeatAndConfirmation(t *testing.T) {
	dir := writeRunnerFixture(t, "pass")
	fresh := time.Now().Unix()
	previous := runnerState{
		Consumer: Consumer{
			Status: "lost", Host: "another-host", PID: 987654, PGID: 987654,
			PIDStart: "old-process", LastHeartbeatAt: &fresh,
		},
		RunID: "old-run",
	}
	writeRunnerState(t, dir, previous)
	if output, err := runnerCommand(dir, "--confirm-stopped").CombinedOutput(); err == nil || !strings.Contains(string(output), "heartbeat is fresh") {
		t.Fatalf("fresh cross-host confirmation = %v, %q; want rejection", err, output)
	}

	stale := time.Now().Add(-4 * time.Minute).Unix()
	previous.LastHeartbeatAt = &stale
	writeRunnerState(t, dir, previous)
	if output, err := runnerCommand(dir).CombinedOutput(); err == nil || !strings.Contains(string(output), "exit is unconfirmed") {
		t.Fatalf("unconfirmed cross-host restart = %v, %q; want rejection", err, output)
	}
	if output, err := runnerCommand(dir, "--confirm-stopped").CombinedOutput(); err != nil {
		t.Fatalf("confirmed cross-host restart: %v: %s", err, output)
	}
	restarted := waitRunnerState(t, dir, "stopped")
	if restarted.RunID == previous.RunID {
		t.Fatalf("confirmed restart reused run identity %q", restarted.RunID)
	}
}
