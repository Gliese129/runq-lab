package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
)

func TestTaskLogGrepReturnsLastMatchesWithLineNumbers(t *testing.T) {
	log := filepath.Join(t.TempDir(), "tk1.log")
	var b strings.Builder
	for i := 1; i <= 10; i++ {
		b.WriteString("step " + string(rune('0'+i%10)) + " loss=x\n")
		b.WriteString("noise\n")
	}
	b.WriteString("DONE: a:b\n")
	if err := os.WriteFile(log, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	row := store.TaskRow{ID: "tk1", LogPath: log}

	got, err := taskLogGrepViaExec(context.Background(), rfs.NewLocalFS(), row, "loss|^DONE", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d matches, want the last 3: %+v", len(got), got)
	}
	last := got[2]
	if last.TaskID != "tk1" || last.Line != 21 || last.Text != "DONE: a:b" {
		t.Fatalf("last match = %+v (text may contain ':')", last)
	}
	if got[0].Line != 17 || got[1].Line != 19 {
		t.Fatalf("not the last matches in order: %+v", got)
	}
}

func TestTaskLogGrepMissingLogAndInvalidRegex(t *testing.T) {
	ctx := context.Background()
	missing := store.TaskRow{ID: "tk1", LogPath: filepath.Join(t.TempDir(), "absent.log")}
	got, err := taskLogGrepViaExec(ctx, rfs.NewLocalFS(), missing, "x", 5)
	if err != nil || len(got) != 0 {
		t.Fatalf("missing log (pending task) should be empty, got %+v, %v", got, err)
	}

	log := filepath.Join(t.TempDir(), "tk2.log")
	if err := os.WriteFile(log, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := taskLogGrepViaExec(ctx, rfs.NewLocalFS(), store.TaskRow{ID: "tk2", LogPath: log}, "(", 5); err == nil {
		t.Fatal("invalid regex should surface grep's error, not an empty result")
	}
	// Shell metacharacters are data, not shell.
	if _, err := taskLogGrepViaExec(ctx, rfs.NewLocalFS(), store.TaskRow{ID: "tk2", LogPath: log}, "a'; touch /tmp/runq-pwn; '", 5); err != nil {
		t.Fatalf("quoted pattern errored: %v", err)
	}
	if _, err := os.Stat("/tmp/runq-pwn"); err == nil {
		t.Fatal("pattern was executed as shell")
	}
}
