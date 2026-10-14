package backend

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// The runner executes on the target; run it locally with the same argv.
func runViewScriptRunner(t *testing.T, command, timeout string) (value any, failure, stderr string) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	out, err := exec.Command("python3", "-c", viewScriptRunner, command, timeout).Output()
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
	var r struct {
		Value  any    `json:"value"`
		Error  string `json:"error"`
		Stderr string `json:"stderr"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("runner output %q: %v", out, err)
	}
	return r.Value, r.Error, r.Stderr
}

func TestViewScriptRunner(t *testing.T) {
	if v, fail, _ := runViewScriptRunner(t, `echo '{"scored": 512}'`, "5"); fail != "" || v.(map[string]any)["scored"] != 512.0 {
		t.Fatalf("json: value=%v failure=%q", v, fail)
	}
	if _, fail, _ := runViewScriptRunner(t, `echo not json`, "5"); fail != "stdout is not one JSON value" {
		t.Fatalf("non-json failure = %q", fail)
	}
	if _, fail, stderr := runViewScriptRunner(t, `echo boom >&2; exit 3`, "5"); fail != "script exit 3" || !strings.Contains(stderr, "boom") {
		t.Fatalf("exit: failure=%q stderr=%q", fail, stderr)
	}
	if _, fail, _ := runViewScriptRunner(t, `sleep 5`, "0.5"); fail != "script timed out" {
		t.Fatalf("timeout failure = %q", fail)
	}
	if _, fail, _ := runViewScriptRunner(t, `head -c 3000000 /dev/zero | tr '\0' 'a'`, "10"); fail != "stdout exceeds 2 MB" {
		t.Fatalf("size failure = %q", fail)
	}
}
