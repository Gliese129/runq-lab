package remote

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/gliese129/runq-lab/internal/rfs"
)

// testFS wraps a real LocalFS for file operations but intercepts command
// execution, letting tests inject fake cluster responses (sbatch, qstat, etc.)
// without implementing the full rfs.FS interface from scratch.
type testFS struct {
	*rfs.LocalFS
	run func(ctx context.Context, command string) (string, error)
}

// newTestFSFromRunner creates a testFS that delegates file ops to the real OS
// and routes shell commands through the provided runner function.
func newTestFSFromRunner(run func(ctx context.Context, command string) (string, error)) *testFS {
	return &testFS{LocalFS: rfs.NewLocalFS(), run: run}
}

func (f *testFS) Exec(ctx context.Context, cmd string, args ...string) (stdout, stderr []byte, exitCode int, err error) {
	// shellRun always calls Exec("sh", "-c", command). Route those through the
	// fake runner; everything else falls through to real execution.
	if cmd == "sh" && len(args) >= 2 && args[0] == "-c" {
		if strings.HasPrefix(args[1], receiptScriptHeader) {
			return f.execReceipt(ctx, args[1])
		}
		out, rerr := f.run(ctx, args[1])
		if rerr != nil {
			return []byte(out), nil, 1, nil
		}
		return []byte(out), nil, 0, nil
	}
	return f.LocalFS.Exec(ctx, cmd, args...)
}

// schedCmd returns the LAST line of a command routed through the fake
// runner — the scheduler invocation itself. Submit commands now carry
// the shared env prelude (HOME/env_setup/exports, RQ-76 r2 #2) above
// the actual qsub/sbatch line, and the runner should match on the
// command, not the prelude.
func schedCmd(command string) string {
	lines := strings.Split(strings.TrimSpace(command), "\n")
	return lines[len(lines)-1]
}

// execReceipt plays a receipt script the way the target would: the fake runner
// sees the same submit command as before receipts existed (handle export plus
// submit.cmd), and the receipt files land in the task directory.
func (f *testFS) execReceipt(ctx context.Context, script string) (stdout, stderr []byte, exitCode int, err error) {
	vars := map[string]string{}
	for _, line := range strings.Split(script, "\n") {
		for _, key := range []string{"r=", "c=", "export RUNQ_SUBMIT_HANDLE="} {
			if strings.HasPrefix(line, key) {
				vars[key] = line[len(key):]
			}
		}
	}
	unquote := func(s string) string {
		return strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(s, "'"), "'"), `'\''`, "'")
	}
	base, cmdFile := unquote(vars["r="]), unquote(vars["c="])
	body, rerr := os.ReadFile(cmdFile)
	if rerr != nil {
		return nil, nil, 255, nil
	}
	_ = os.WriteFile(base+".started", nil, 0o644)
	out, rerr := f.run(ctx, "export RUNQ_SUBMIT_HANDLE="+vars["export RUNQ_SUBMIT_HANDLE="]+"\n"+string(body))
	code := 0
	if rerr != nil {
		code = 1
	}
	_ = os.WriteFile(base+".out", []byte(out), 0o644)
	_ = os.WriteFile(base+".rc", []byte(strconv.Itoa(code)+"\n"), 0o644)
	return []byte(out), nil, code, nil
}
