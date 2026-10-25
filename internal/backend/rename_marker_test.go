package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
)

type markerLane struct {
	*UnavailableBackend
	fs rfs.FS
}

func (l *markerLane) FS() rfs.FS { return l.fs }

// Renames rewrite the 0-<name> marker in the id-named directories that
// exist; ids and paths stay put.
func TestRenameRewritesMarkers(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m, err := NewMultiBackend(map[string]Backend{"a": &markerLane{NewUnavailableBackend(nil), rfs.NewLocalFS()}}, st, "a")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CreateProject(ctx, project.Config{ProjectName: "kl", Target: "a", WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	proj, err := m.GetProject(ctx, "kl")
	if err != nil {
		t.Fatal(err)
	}
	projDir := filepath.Join(t.TempDir(), proj.ID)
	jobDir := filepath.Join(projDir, "jb00000001")
	taskDir := filepath.Join(jobDir, "tk00000001")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(projDir, "0-kl"), filepath.Join(jobDir, "0-old")} {
		_ = os.WriteFile(f, nil, 0o644)
	}
	job := &store.JobRow{ID: "jb00000001", ProjectName: "kl", Name: "old", ConfigJSON: "{}", Status: "done", Target: "a", CreatedAt: time.Now()}
	task := store.TaskRow{ID: "tk00000001", JobID: job.ID, ProjectName: "kl", Status: "success", Target: "a", TaskDir: taskDir, ParamsJSON: "{}"}
	if err := st.InsertJobWithTasks(ctx, job, []store.TaskRow{task}); err != nil {
		t.Fatal(err)
	}

	if _, err := m.RenameJob(ctx, "jb0000", "A1 原始基线"); err != nil {
		t.Fatal(err)
	}
	if err := m.RenameProject(ctx, proj.ID, "kl-ablation"); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{filepath.Join(jobDir, "0-A1 原始基线"): true, filepath.Join(projDir, "0-kl-ablation"): true}
	gone := []string{filepath.Join(jobDir, "0-old"), filepath.Join(projDir, "0-kl")}
	for f := range want {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("marker missing: %v", err)
		}
	}
	for _, f := range gone {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("stale marker kept: %s", f)
		}
	}
}
