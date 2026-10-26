package ingest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/workspace"
)

func TestReapLinksIngestsReplacesAndFreezes(t *testing.T) {
	ctx := context.Background()
	taskDir := t.TempDir()
	st := openMemoryStoreAndSeed(t, "t1", "j1", taskDir)
	target := Target{TaskID: "t1", JobID: "j1", Dir: taskDir}
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(workspace.LinksPath(taskDir), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// No file yet (task still starting): nothing, no error.
	if _, err := ReapIncremental(ctx, st, target, false); err != nil {
		t.Fatal(err)
	}

	write(`{"wandb":"https://wandb.ai/e/p/runs/abc","bad":3}`)
	if _, err := ReapIncremental(ctx, st, target, false); err != nil {
		t.Fatal(err)
	}
	got, _ := st.TaskLinks(ctx, []string{"t1"})
	if got["t1"]["wandb"] != "https://wandb.ai/e/p/runs/abc" || len(got["t1"]) != 1 {
		t.Fatalf("links = %+v, want only the string-valued wandb entry", got)
	}

	// A rewrite (different size) replaces the set wholesale.
	write(`{"tensorboard":"http://tb.example/run1"}`)
	if _, err := ReapIncremental(ctx, st, target, true); err != nil {
		t.Fatal(err)
	}
	got, _ = st.TaskLinks(ctx, []string{"t1"})
	if len(got["t1"]) != 1 || got["t1"]["tensorboard"] == "" {
		t.Fatalf("links after rewrite = %+v", got)
	}

	// Final pass froze the mark: later edits are ignored.
	write(`{"wandb":"https://later.example/x/y/z/longer-than-before"}`)
	if _, err := ReapIncremental(ctx, st, target, false); err != nil {
		t.Fatal(err)
	}
	got, _ = st.TaskLinks(ctx, []string{"t1"})
	if _, ok := got["t1"]["wandb"]; ok {
		t.Fatalf("frozen task picked up a new link: %+v", got)
	}
}

// runq.link rewrites links.json atomically; a same-size change is still
// picked up (the mark tracks mtime, not only size).
func TestReapLinksSeesSameSizeRewrite(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st := openMemoryStoreAndSeed(t, "t1", "j1", dir)
	target := Target{TaskID: "t1", JobID: "j1", Dir: dir}
	path := workspace.LinksPath(dir)
	for i, url := range []string{"https://a.test/one", "https://a.test/two"} {
		if err := os.WriteFile(path, []byte(`{"wandb":"`+url+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Unix(1_700_000_000+int64(i), 0) // distinct mtimes, as two writes a second apart
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
		if _, err := ReapIncremental(ctx, st, target, false); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := st.TaskLinks(ctx, []string{"t1"})
	if got["t1"]["wandb"] != "https://a.test/two" {
		t.Fatalf("link = %q, want the rewritten /two", got["t1"]["wandb"])
	}
}
