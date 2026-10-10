package backend

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
)

type mappedProjectFS struct {
	rfs.FS
	root string
}

func (f *mappedProjectFS) mapPath(name string) string {
	return filepath.Join(f.root, filepath.FromSlash(strings.TrimPrefix(name, "/project")))
}
func (f *mappedProjectFS) Stat(name string) (fs.FileInfo, error) { return os.Stat(f.mapPath(name)) }
func (f *mappedProjectFS) ReadFile(name string) ([]byte, error)  { return os.ReadFile(f.mapPath(name)) }
func (f *mappedProjectFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return os.ReadDir(f.mapPath(name))
}
func (f *mappedProjectFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(f.mapPath(name), data, perm)
}
func (f *mappedProjectFS) MkdirAll(name string, perm os.FileMode) error {
	return os.MkdirAll(f.mapPath(name), perm)
}
func (f *mappedProjectFS) Remove(name string) error { return os.Remove(f.mapPath(name)) }

type projectSyncLane struct {
	*UnavailableBackend
	fs        rfs.FS
	submitted int
}

func (l *projectSyncLane) FS() rfs.FS { return l.fs }
func (l *projectSyncLane) SubmitJob(context.Context, job.JobConfig, SubmitOptions) (string, int, error) {
	l.submitted++
	return "job-sync", 1, nil
}

func TestCrossTargetSubmitMirrorsProjectBeforeDelegating(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	sourceRoot, destinationRoot := t.TempDir(), t.TempDir()
	sourceFS := &mappedProjectFS{FS: rfs.NewLocalFS(), root: sourceRoot}
	destinationFS := &mappedProjectFS{FS: rfs.NewLocalFS(), root: destinationRoot}
	sourceLane := &projectSyncLane{UnavailableBackend: NewUnavailableBackend(nil), fs: sourceFS}
	destinationLane := &projectSyncLane{UnavailableBackend: NewUnavailableBackend(nil), fs: destinationFS}
	m, err := NewMultiBackend(map[string]Backend{"server-a": sourceLane, "server-b": destinationLane}, st, "server-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, project.SyncFile), []byte("src/**\n!src/private/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sourceRoot, "src", "private"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "src", "main.go"), []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "src", "private", "key"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := project.Config{
		ProjectName: "p", Target: "server-a", Targets: []string{"server-a", "server-b"},
		WorkingDir: "/project", CmdTemplate: "true",
	}
	if err := m.registry.Add(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	jobID, total, err := m.SubmitJob(ctx, job.JobConfig{Project: "p"}, SubmitOptions{Target: "server-b"})
	if err != nil {
		t.Fatal(err)
	}
	if jobID != "job-sync" || total != 1 || destinationLane.submitted != 1 {
		t.Fatalf("submit result = %q/%d, calls=%d", jobID, total, destinationLane.submitted)
	}
	got, err := os.ReadFile(filepath.Join(destinationRoot, "src", "main.go"))
	if err != nil || string(got) != "source" {
		t.Fatalf("mirrored file = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(destinationRoot, "src", "private", "key")); !os.IsNotExist(err) {
		t.Fatalf("! exclusion was copied: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destinationRoot, "src", "main.go"), []byte("destination-truth"), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := m.SyncProject(ctx, "p", "server-b", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].DestinationTarget != "server-a" {
		t.Fatalf("force-sync results = %+v", results)
	}
	got, err = os.ReadFile(filepath.Join(sourceRoot, "src", "main.go"))
	if err != nil || string(got) != "destination-truth" {
		t.Fatalf("one-shot truth override = %q, %v", got, err)
	}
}

func TestProjectRejectsUndeclaredSubmitTarget(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := t.TempDir()
	fsys := &mappedProjectFS{FS: rfs.NewLocalFS(), root: root}
	lane := &projectSyncLane{UnavailableBackend: NewUnavailableBackend(nil), fs: fsys}
	m, err := NewMultiBackend(map[string]Backend{"server-a": lane, "server-b": lane}, st, "server-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.registry.Add(ctx, project.Config{
		ProjectName: "p", Target: "server-a", Targets: []string{"server-a"},
		WorkingDir: "/project", CmdTemplate: "true",
	}); err != nil {
		t.Fatal(err)
	}
	_, _, err = m.SubmitJob(ctx, job.JobConfig{Project: "p"}, SubmitOptions{Target: "server-b"})
	if err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("undeclared target error = %v", err)
	}
	if lane.submitted != 0 {
		t.Fatal("submit delegated despite target violation")
	}
}
