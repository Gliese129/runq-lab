package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The SSH alias now points at another host, the generation is unchanged:
// the version's files are on the old filesystem, which no lane reads. Every
// file access refuses it — curves and view history as well as logs, and a
// checkpoints clean must not delete what is at the same path on the new host.
func TestFileAccessFollowsLocation(t *testing.T) {
	ctx := context.Background()
	f := newJobFilesFixture(t)
	if _, err := f.st.DB().ExecContext(ctx, `UPDATE tasks SET location = 'ssh:me@old-host:22'`); err != nil {
		t.Fatal(err)
	}
	ckpt := filepath.Join(f.taskDir, "checkpoints", "step100.pt")
	if err := os.MkdirAll(filepath.Dir(ckpt), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ckpt, []byte("weights"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.taskDir, "metrics.jsonl"),
		[]byte(`{"type":"metric","key":"loss","value":0.4,"step":1,"ts":1}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := NewMultiBackend(map[string]Backend{"local": f.lane}, f.st, "local")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.TaskLogRead(ctx, "task", 0, 10); !errors.Is(err, ErrFilesElsewhere) {
		t.Fatalf("log read: %v", err)
	}
	if _, err := m.SourceHistory(ctx, "task", "metrics", "step", 10); !errors.Is(err, ErrFilesElsewhere) {
		t.Fatalf("curve read: %v", err)
	}
	if _, err := m.TaskLogGrep(ctx, "task", "x", 10); !errors.Is(err, ErrFilesElsewhere) {
		t.Fatalf("log grep: %v", err)
	}
	if _, err := m.Clean(ctx, CleanOptions{Checkpoints: true, TaskIDs: []string{"task"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ckpt); err != nil {
		t.Fatalf("clean --checkpoints deleted another filesystem's path: %v", err)
	}
	refs, err := f.st.TaskDirs(ctx, "", "job")
	if err != nil || len(refs) != 2 || refs[0].Location != "ssh:me@old-host:22" {
		t.Fatalf("task dirs: %+v, %v", refs, err)
	}
}
