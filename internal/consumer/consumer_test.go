package consumer

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSingleExecutionAndPythonExit(t *testing.T) {
	for _, tc := range []struct {
		name, code, status string
		exit               int
	}{{"normal", "pass", "stopped", 0}, {"exit", "import sys; sys.exit(3)", "failed", 3}, {"exception", "raise RuntimeError('test')", "failed", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, data := range map[string][]byte{"runner.py": Runner, "python_entry.py": PythonEntry, "business.py": []byte(tc.code), "command.sh": []byte("exec python3 '" + filepath.Join(dir, "python_entry.py") + "' '" + filepath.Join(dir, "business.py") + "'")} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("python3", filepath.Join(dir, "runner.py"))
			cmd.Env = append(os.Environ(), "RUNQ_CONSUMER_DIR="+dir)
			_ = cmd.Run()
			deadline := time.Now().Add(5 * time.Second)
			var c Consumer
			for time.Now().Before(deadline) {
				raw, _ := os.ReadFile(filepath.Join(dir, "status.json"))
				_ = json.Unmarshal(raw, &c)
				if c.Status == tc.status {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if c.Status != tc.status || c.ExitCode == nil || *c.ExitCode != tc.exit {
				t.Fatalf("wrong exit: %+v", c)
			}
			cmd = exec.Command("python3", filepath.Join(dir, "runner.py"))
			cmd.Env = append(os.Environ(), "RUNQ_CONSUMER_DIR="+dir)
			if err := cmd.Run(); err == nil {
				t.Fatal("same consumer ID started twice")
			}
		})
	}
}
