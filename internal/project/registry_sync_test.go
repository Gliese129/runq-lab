package project

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/gliese129/runq-lab/internal/store"
	_ "modernc.org/sqlite"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE projects (name TEXT PRIMARY KEY, id TEXT, config_json TEXT)`); err != nil {
		t.Fatal(err)
	}
	return NewRegistry(db)
}

// Archive guard: a project with live work must not be hidden — its jobs
// would cascade out of the global lists while still holding queue slots.
// paused counts as active (resumable = forgettable).
func TestProjectArchiveRefusesActiveJobs(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := NewRegistry(st.DB())
	cfg := Config{ProjectName: "p", WorkingDir: dir, CmdTemplate: "echo {{x}}"}
	ctx := context.Background()
	if err := reg.Add(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(
		`INSERT INTO jobs (id, project_name, description, config_json, status, created_at) VALUES ('jb1','p','','{}','paused',1)`); err != nil {
		t.Fatal(err)
	}
	if err := reg.Archive(ctx, "p"); err == nil {
		t.Fatal("archiving a project with a paused job must be refused")
	}
	if _, err := st.DB().Exec(`UPDATE jobs SET status = 'done' WHERE id = 'jb1'`); err != nil {
		t.Fatal(err)
	}
	if err := reg.Archive(ctx, "p"); err != nil {
		t.Fatalf("archive with only terminal jobs: %v", err)
	}
	names, _ := reg.ArchivedNames(ctx)
	if !names["p"] {
		t.Fatal("ArchivedNames must report p")
	}
	if err := reg.Unarchive(ctx, "p"); err != nil {
		t.Fatal(err)
	}
}

// rc6 explicit upsert: the registration is the truth; a hand-edited
// project.yaml changes nothing until `runq project <file>` registers it.
func TestGetIgnoresProjectYAMLOnDisk(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()
	dir := t.TempDir()
	if err := r.Add(ctx, Config{ProjectName: "p", Target: "local", WorkingDir: dir, CmdTemplate: "echo old"}); err != nil {
		t.Fatal(err)
	}
	data := []byte("project_name: p\ntarget: local\nworking_dir: " + dir + "\ncommand: echo new\n")
	if err := os.WriteFile(filepath.Join(dir, "project.yaml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := r.Get(ctx, "p")
	if err != nil || cfg.CmdTemplate != "echo old" {
		t.Fatalf("Get must not read project.yaml: %+v %v", cfg, err)
	}
}
