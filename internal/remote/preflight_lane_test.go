package remote

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/preflight"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/store"
)

// The lane path: preflight warns about a missing entry script and a broken
// run.sh line, the submit goes through anyway, and the report (with
// shell_syntax) reaches the caller and the preview.
func TestPreflightWarnsNeverBlocks(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	t.Setenv("RUNQ_DATA_DIR", t.TempDir())

	// The fake cluster runs the preflight script for real (locally) and
	// answers everything else with nothing.
	execs := 0
	runner := func(ctx context.Context, command string) (string, error) {
		if !strings.Contains(command, "##RUNQ_PF##") {
			return "", nil
		}
		execs++
		out, _ := exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput()
		return string(out), nil
	}
	b := &Backend{Cfg: &config.TargetConfig{Name: "cl", SubmitTemplate: "submit {{run_sh}} 'oops", SubmitIDRegex: `job ([0-9]+)`},
		Store: st, FS: newTestFSFromRunner(runner)}
	proj := &project.Config{ProjectName: "p", WorkingDir: t.TempDir(), CmdTemplate: "sh missing.sh --lr {{lr}}"}
	jobCfg := job.JobConfig{Project: "p", FixedParams: map[string]any{"lr": 0.1}}
	ctx := context.Background()

	text, plan, err := b.CompilePreview(ctx, jobCfg, proj)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	for _, s := range []string{"! entry", "missing.sh does not exist", "! shell_syntax", "submit line:"} {
		if !strings.Contains(text, s) {
			t.Errorf("preview missing %q:\n%s", s, text)
		}
	}
	if n := len(plan.Preflight.Results); n != len(preflight.CheckNames) {
		t.Errorf("preview report has %d checks, want %d", n, len(preflight.CheckNames))
	}

	var report preflight.Report
	_, rows, err := b.Prepare(ctx, jobCfg, proj, SubmitOpts{Preflight: &report})
	if err != nil {
		t.Fatalf("warnings must not block the submit: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	var names []string
	for _, c := range report.Warnings() {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "entry,shell_syntax" {
		t.Fatalf("submit warnings = %v, want entry,shell_syntax:\n%s", names, preflight.Format(report.Results))
	}
	if execs != 2 {
		t.Fatalf("preflight execs = %d, want one per preview/submit", execs)
	}
}
