package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/diskspace"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestPrintCheckpointPreviewListsJobsAndFreeSpace(t *testing.T) {
	r := &backend.CleanResult{
		CheckpointJobs: []backend.CheckpointJob{
			{JobID: "jb1", Project: "p", Target: "tsubame", Tasks: 4, Bytes: 3 << 30, Files: 12, Mount: "/gs/bs"},
			{JobID: "jb2", Project: "p", Target: "tsubame", Tasks: 1, Bytes: -1, Files: -1},
		},
		Filesystems: []backend.CleanFilesystem{{Target: "tsubame", Filesystem: diskspace.Filesystem{
			Mount: "/gs/bs", TotalBytes: 100 << 30, FreeBytes: 2 << 30, TotalInodes: 1000, FreeInodes: 10, InodesKnown: true}}},
	}
	var total int64
	out := captureStdout(t, func() { total = printCheckpointPreview(r) })
	if total != 3<<30 {
		t.Fatalf("total = %d", total)
	}
	for _, want := range []string{"jb1", "3.0 GB", "/gs/bs on tsubame: 2.0 GB free of 100.0 GB (2%)", "10 of 1000 inodes free (1%)", "unknown", "Total: 3.0 GB of checkpoints in 2 jobs"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}
}

// A run-phase failure reason (disk full) is not "failed before running".
func TestTaskFailureHeadlineForRunPhaseFailure(t *testing.T) {
	started := int64(1)
	out := captureStdout(t, func() {
		printTaskFailures([]backend.TaskView{
			{ID: "tk1", Status: "failed", StartedAt: &started, FailureDetail: "disk full (inodes): /gs/bs has no free inodes"},
			{ID: "tk2", Status: "failed", FailureDetail: "qsub: rejected"},
		})
	})
	if !strings.Contains(out, "failed:\n    disk full (inodes)") || !strings.Contains(out, "failed before running:\n    qsub: rejected") {
		t.Fatalf("headlines:\n%s", out)
	}
}
