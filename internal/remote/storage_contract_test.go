package remote

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
)

// Storage contract: <data_path>/<pj…>/<jb…>/<tk…>/, id-named directories
// with 0-<name> markers, and a <working_dir>/.runq/<pj…> convenience link.
func TestStorageContractLayout(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	root := t.TempDir()
	working := filepath.Join(root, "code")
	if err := os.MkdirAll(working, 0o755); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(root, "bs")
	cfg := &config.TargetConfig{Name: "fixture", Scheduler: "sge", DataPath: data, SubmitTemplate: "echo 55", SubmitIDRegex: "([0-9]+)", KillTemplate: "true"}
	b := &Backend{Cfg: cfg, Store: st, FS: rfs.NewLocalFS()}
	proj := &project.Config{ProjectName: "kl", WorkingDir: working, CmdTemplate: "true"}

	jobID, rows, err := b.Prepare(ctx, job.JobConfig{Project: "kl", Name: "A1 原始基线"}, proj, SubmitOpts{})
	if err != nil {
		t.Fatal(err)
	}
	projDir := filepath.Join(data, proj.ID)
	jobDir := filepath.Join(projDir, jobID)
	if filepath.Dir(rows[0].TaskDir) != jobDir {
		t.Fatalf("task dir %q not in %q", rows[0].TaskDir, jobDir)
	}
	for _, f := range []string{filepath.Join(projDir, "0-kl"), filepath.Join(jobDir, "0-A1 原始基线")} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("marker missing: %v", err)
		}
	}
	if got, err := os.Readlink(filepath.Join(working, ".runq", proj.ID)); err != nil || got != projDir {
		t.Errorf(".runq link = %q, %v; want %q", got, err, projDir)
	}

	// The global data_path is a local-target fallback only: an SSH target
	// without its own data_path keeps data next to the code on the target.
	ssh := &Backend{Cfg: &config.TargetConfig{Name: "c", SSH: &config.SSHTargetConfig{}}, StorageCfg: &config.GlobalConfig{DataPath: "/laptop/disk"}}
	if got, _ := ssh.WorkspaceRoot(ctx, proj, false); got != filepath.Join(working, ".runq", proj.ID) {
		t.Errorf("SSH target used the global data_path: %q", got)
	}
}
