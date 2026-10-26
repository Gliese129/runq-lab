package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gliese129/runq-lab/internal/rfs"
)

func TestExactHistoryPrefixAndTail(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "results.jsonl")
	data := `{"ts":1,"section":"eval","axes":{"model":"a","step":0},"metrics":{"score":1}}
{"ts":2,"section":"eval","axes":{"model":"a","step":10},"metrics":{"score":9}}
`
	if err := os.WriteFile(file, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := QueryExactFramesAt(context.Background(), rfs.NewLocalFS(), dir, "results", "step", []float64{0, 5, 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Frames) != 3 || h.Frames[1].Records[0].Metrics["score"] != float64(1) {
		t.Fatalf("frame used an average: %+v", h)
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("{\"ts\":3,\"section\":\"eval\",\"axes\":{\"model\":\"a\",\"step\":20},\"metrics\":{\"score\":7}}\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	h, err = QueryExactFramesAt(context.Background(), rfs.NewLocalFS(), dir, "results", "step", []float64{15, 20})
	if err != nil {
		t.Fatal(err)
	}
	if h.Frames[0].Records[0].Metrics["score"] != float64(9) || h.Frames[1].Records[0].Metrics["score"] != float64(7) {
		t.Fatalf("tail not indexed: %+v", h)
	}
}

// A scoring script's results.<producer>.jsonl animates like results.jsonl.
func TestExactHistoryProducerStream(t *testing.T) {
	dir := t.TempDir()
	data := `{"ts":1,"section":"math","axes":{"split":"test","step":100},"metrics":{"acc":0.5}}
{"ts":2,"section":"math","axes":{"split":"test","step":200},"metrics":{"acc":0.75}}
`
	if err := os.WriteFile(filepath.Join(dir, "results.score.jsonl"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := QueryExactFramesAt(context.Background(), rfs.NewLocalFS(), dir, ProducerResultsStream("score"), "step", []float64{100, 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Frames) != 2 || h.Frames[1].Records[0].Metrics["acc"] != 0.75 {
		t.Fatalf("producer frames: %+v", h)
	}
	for _, bad := range []string{"metrics", "results.bad name", "../results"} {
		if _, err := QueryExactFramesAt(context.Background(), rfs.NewLocalFS(), dir, bad, "step", []float64{1}); err == nil {
			t.Errorf("stream %q must be refused for frames", bad)
		}
	}
}
