package remote

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/store"
)

// sgeQstatHeader is the two header lines SGE/UGE qstat prints before job rows.
const sgeQstatHeader = "job-ID     prior   name       user         state submit/start at     queue                          jclass                         slots ja-task-ID\n" +
	"------------------------------------------------------------------------------------------------------------------------------------------------\n"

// A qstat that fails mid-query (TSUBAME, 2026-10-23: gdi request timeout)
// is a failed observation, not a "gone" verdict: the task keeps the
// wrapper's running state and the pass stays unfresh. Only a qstat that
// SUCCEEDED and omits the job may say gone.
func TestSGEQstatErrorIsNotGone(t *testing.T) {
	gdi := "error: failed receiving gdi request response for mid=1 (got syncron message receive timeout error).\n"
	other := "9100000 0.55500 other      user         r     10/23/2026 05:00:00 node_f.q@r1n1                                                     1\n"
	cases := []struct {
		name     string
		out      string
		fail     bool // qstat exits non-zero
		wantGone bool
	}{
		{name: "gdi timeout", out: gdi, fail: true},
		{name: "non-zero exit with partial output", out: sgeQstatHeader + other + gdi, fail: true},
		{name: "successful qstat without the job", out: sgeQstatHeader + other, wantGone: true},
	}
	entries := []struct {
		name string
		run  func(b *Backend, jobID string) error
	}{
		{"SchedulerProbe", func(b *Backend, _ string) error { return b.SchedulerProbe(context.Background(), 0) }},
		{"EnsureFresh", func(b *Backend, jobID string) error { return b.EnsureFresh(context.Background(), jobID, 0) }},
	}
	for _, tc := range cases {
		for _, entry := range entries {
			t.Run(tc.name+"/"+entry.name, func(t *testing.T) {
				st, err := store.Open(":memory:")
				if err != nil {
					t.Fatal(err)
				}
				defer st.Close()
				dir := t.TempDir()
				jobID, taskID := seedTask(t, st, dir, "running", SourceWrapper)
				if _, err := st.DB().Exec(`UPDATE jobs SET target = 'tsubame'`); err != nil {
					t.Fatal(err)
				}
				if _, err := st.DB().Exec(`UPDATE tasks SET target = 'tsubame'`); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, statusFileName), []byte(`{"status":"running"}`), 0o644); err != nil {
					t.Fatal(err)
				}
				cfg, err := config.HPCPreset("tsubame")
				if err != nil {
					t.Fatal(err)
				}
				cfg.Name = "tsubame"
				b := &Backend{Cfg: cfg, Store: st, FS: newTestFSFromRunner(func(ctx context.Context, command string) (string, error) {
					if command == "qstat" {
						if tc.fail {
							return tc.out, errors.New("exit 1")
						}
						return tc.out, nil
					}
					// Parser pipelines run for real, as on a login node.
					out, err := exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput()
					return string(out), err
				})}

				runErr := entry.run(b, jobID)
				tk, err := st.GetTask(context.Background(), taskID)
				if err != nil {
					t.Fatal(err)
				}
				if tc.wantGone {
					if runErr != nil {
						t.Fatalf("probe: %v", runErr)
					}
					if tk.Status != "failed" || tk.StatusSource != SourceInferred {
						t.Fatalf("got %s/%s, want failed/inferred", tk.Status, tk.StatusSource)
					}
					return
				}
				if tk.Status != "running" || tk.StatusSource != SourceWrapper || tk.NativeState == "gone" {
					t.Fatalf("got %s/%s native=%q, want running/wrapper", tk.Status, tk.StatusSource, tk.NativeState)
				}
				if runErr == nil {
					t.Fatal("failed qstat reported a complete pass")
				}
				if b.probeIsFresh(jobID, 1<<62) {
					t.Fatal("failed qstat marked the job probed")
				}
			})
		}
	}
}
