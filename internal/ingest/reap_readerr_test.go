package ingest

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/workspace"
)

// flakyFS serves the first limit bytes of every opened file, then fails the
// read with a transient (non-EOF) transport error, like an SSH channel
// dropping mid-transfer.
type flakyFS struct {
	rfs.FS
	limit int64
}

type flakyFile struct {
	rfs.File
	left int64
}

var errTransport = errors.New("temporary transport read failure")

func (f flakyFS) Open(path string) (rfs.File, error) {
	file, err := f.FS.Open(path)
	if err != nil {
		return nil, err
	}
	return &flakyFile{File: file, left: f.limit}, nil
}

func (f *flakyFile) Read(p []byte) (int, error) {
	if f.left <= 0 {
		return 0, errTransport
	}
	if int64(len(p)) > f.left {
		p = p[:f.left]
	}
	n, err := f.File.Read(p)
	f.left -= int64(n)
	return n, err
}

const (
	metricLine1 = `{"type":"metric","key":"loss","value":0.2,"step":1,"ts":1}`
	metricLine2 = `{"type":"metric","key":"loss","value":0.1,"step":2,"ts":2}`
	resultLine1 = `{"ts":1,"axes":{"step":1},"metrics":{"acc":0.5}}`
	resultLine2 = `{"ts":2,"axes":{"step":2},"metrics":{"acc":0.6}}`
)

// A read that fails before EOF is not "the file was read to the end": the
// terminal pass must leave the marks open at what it really consumed, and a
// later healthy pass ingests the rest and only then freezes.
func TestReapReadErrorKeepsMarksOpen(t *testing.T) {
	for name, cut := range map[string]int64{
		"fails at the first byte":   0,
		"fails inside the 2nd line": int64(len(metricLine1)) + 5,
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			writeTaskFile(t, workspace.MetricsPath(dir), []string{metricLine1, metricLine2})
			writeTaskFile(t, workspace.ResultsPath(dir), []string{resultLine1, resultLine2})
			st := openMemoryStoreAndSeed(t, "t1", "j1", dir)

			// results line 1 is shorter than metrics line 1, so the same cut
			// lands inside results line 2 as well.
			wantLines := 0
			if cut > 0 {
				wantLines = 1
			}
			res, err := ReapIncremental(ctx, st, Target{TaskID: "t1", JobID: "j1", Dir: dir, FS: flakyFS{rfs.NewLocalFS(), cut}}, true)
			if !errors.Is(err, errTransport) {
				t.Fatalf("failed read reported as %v, want the transport error", err)
			}
			if res.MetricCount != wantLines || res.ResultCount != wantLines {
				t.Fatalf("partial pass counts = %+v, want %d complete line(s) each", res, wantLines)
			}
			mark, _ := st.GetIngestMark(ctx, "t1")
			wantOffset := int64(0)
			if wantLines == 1 {
				wantOffset = int64(len(metricLine1)) + 1
			}
			if mark.Final || mark.Offset != wantOffset || mark.Size != wantOffset {
				t.Fatalf("metrics mark after failed read = %+v, want open at offset %d", mark, wantOffset)
			}
			rmark, _ := st.GetFileIngestMark(ctx, "t1", store.IngestFileResults)
			wantROffset := int64(0)
			if wantLines == 1 {
				wantROffset = int64(len(resultLine1)) + 1
			}
			if rmark.Final || rmark.Offset != wantROffset || rmark.Size != wantROffset {
				t.Fatalf("results mark after failed read = %+v, want open at offset %d", rmark, wantROffset)
			}

			res, err = ReapIncremental(ctx, st, Target{TaskID: "t1", JobID: "j1", Dir: dir}, true)
			if err != nil {
				t.Fatalf("healthy retry: %v", err)
			}
			if res.MetricCount != 2-wantLines || res.ResultCount != 2-wantLines {
				t.Fatalf("retry counts = %+v, want the %d unread line(s)", res, 2-wantLines)
			}
			sums, err := st.ListMetricSummaries(ctx, "j1", "loss")
			if err != nil || len(sums) != 1 || sums[0].Count != 2 || sums[0].Last != 0.1 {
				t.Fatalf("summary = %+v, %v; want both points exactly once", sums, err)
			}
			mark, _ = st.GetIngestMark(ctx, "t1")
			rmark, _ = st.GetFileIngestMark(ctx, "t1", store.IngestFileResults)
			if !mark.Final || !rmark.Final {
				t.Fatalf("marks after a complete terminal read = %+v / %+v, want final", mark, rmark)
			}
		})
	}
}

// A non-final pass that fails must not leave a mark that claims the whole
// file: the next pass of an unchanged file has to read the rest instead of
// skipping it as "no new bytes".
func TestReapReadErrorRetriesUnchangedFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeTaskFile(t, workspace.MetricsPath(dir), []string{metricLine1, metricLine2})
	st := openMemoryStoreAndSeed(t, "t1", "j1", dir)
	cut := int64(len(metricLine1)) + 5

	if _, err := ReapIncremental(ctx, st, Target{TaskID: "t1", JobID: "j1", Dir: dir, FS: flakyFS{rfs.NewLocalFS(), cut}}, false); !errors.Is(err, errTransport) {
		t.Fatalf("failed read reported as %v", err)
	}
	res, err := ReapIncremental(ctx, st, Target{TaskID: "t1", JobID: "j1", Dir: dir}, false)
	if err != nil {
		t.Fatalf("healthy retry: %v", err)
	}
	if res.MetricCount != 1 {
		t.Fatalf("retry of an unchanged file read %d lines, want the 1 unread line", res.MetricCount)
	}
}

// Producer files share the scan: a failed read leaves the mark short of the
// listed size, so the next listing (same size) retries it.
func TestReapProducerReadErrorRetries(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := ProducerResultsPath(dir, "score")
	writeTaskFile(t, path, []string{resultLine1, resultLine2})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st := openMemoryStoreAndSeed(t, "t1", "j1", dir)
	cut := int64(len(resultLine1)) + 5

	n, err := ReapProducerResults(ctx, st, Target{TaskID: "t1", JobID: "j1", Dir: dir, FS: flakyFS{rfs.NewLocalFS(), cut}}, "score", info.Size())
	if !errors.Is(err, errTransport) || n != 1 {
		t.Fatalf("failed read = %d, %v; want 1 line and the transport error", n, err)
	}
	mark, _ := st.GetFileIngestMark(ctx, "t1", store.ProducerResultsFile("score"))
	if mark.Size == info.Size() || mark.Offset != int64(len(resultLine1))+1 {
		t.Fatalf("producer mark after failed read = %+v", mark)
	}
	n, err = ReapProducerResults(ctx, st, Target{TaskID: "t1", JobID: "j1", Dir: dir}, "score", info.Size())
	if err != nil || n != 1 {
		t.Fatalf("healthy retry = %d, %v; want the 1 unread line", n, err)
	}
}
