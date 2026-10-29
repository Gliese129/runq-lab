package workspace

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
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

// Curves are drawn honestly: a null value (NaN/Inf from the SDK) stays in the
// series as a gap all the way to the JSON the chart reads — 1 → gap → 3,
// never 1 → 3 — and a point logged without a step is placed at its
// write-order position instead of being dropped.
func TestExactHistoryCurvesKeepGapsAndSteplessPoints(t *testing.T) {
	dir := t.TempDir()
	data := `{"type":"metric","key":"gap","value":1,"step":1,"ts":1}
{"type":"metric","key":"gap","value":null,"step":2,"ts":2}
{"type":"metric","key":"gap","value":3,"step":3,"ts":3}
{"type":"metric","key":"nostep","value":0.5,"ts":1}
{"type":"metric","key":"nostep","value":0.6,"step":null,"ts":2}
{"type":"metric","key":"novalue","step":1,"ts":1}
`
	if err := os.WriteFile(filepath.Join(dir, "metrics.jsonl"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := QueryExactHistory(context.Background(), rfs.NewLocalFS(), dir, "metrics", "step", 400)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ExactSeries{}
	for _, s := range h.Series {
		got[s.Key] = s
	}
	if _, ok := got["novalue"]; ok {
		t.Errorf("a metric line without a value became a point: %+v", got["novalue"])
	}
	gap := got["gap"]
	if len(gap.Step) != 3 || gap.Step[1] != 2 || len(gap.Value) != 3 || !math.IsNaN(gap.Value[1]) || gap.Value[2] != 3 {
		t.Fatalf("null point removed or filled: %+v", gap)
	}
	raw, err := json.Marshal(gap)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"value":[1,null,3]`) {
		t.Fatalf("gap not null on the wire: %s", raw)
	}
	var back ExactSeries
	if err := json.Unmarshal(raw, &back); err != nil || !math.IsNaN(back.Value[1]) {
		t.Fatalf("round trip lost the gap: %+v %v", back, err)
	}
	nostep := got["nostep"]
	if len(nostep.Step) != 2 || nostep.Step[0] != 0 || nostep.Step[1] != 1 || nostep.Value[1] != 0.6 {
		t.Fatalf("stepless points not placed in write order: %+v", nostep)
	}
}
