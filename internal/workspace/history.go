package workspace

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/gliese129/runq-lab/internal/rfs"
)

//go:embed history.py
var exactHistoryScript string

type ExactRecord struct {
	TS      int64          `json:"ts"`
	Section string         `json:"section"`
	Axes    map[string]any `json:"axes"`
	Metrics map[string]any `json:"metrics"`
}
type ExactFrame struct {
	At      float64       `json:"at"`
	Records []ExactRecord `json:"records"`
}
type ExactSeries struct {
	Key       string    `json:"key"`
	Step      []float64 `json:"step"`
	Value     []float64 `json:"value"`
	Signature string    `json:"signature"`
}
type ExactHistory struct {
	By     string        `json:"by"`
	Frames []ExactFrame  `json:"frames"`
	Series []ExactSeries `json:"series"`
}

func QueryExactHistory(ctx context.Context, fsys rfs.FS, dir, stream, by string, count int) (*ExactHistory, error) {
	if stream != "results" && stream != "metrics" {
		return nil, fmt.Errorf("invalid history stream")
	}
	if by != "step" && by != "time" {
		return nil, fmt.Errorf("by must be step or time")
	}
	if count < 2 || count > 2000 {
		return nil, fmt.Errorf("points must be 2..2000")
	}
	return queryExactHistory(ctx, fsys, dir, stream, by, count, nil)
}
func QueryExactFramesAt(ctx context.Context, fsys rfs.FS, dir, by string, ats []float64) (*ExactHistory, error) {
	return queryExactHistory(ctx, fsys, dir, "results", by, 2, ats)
}
func queryExactHistory(ctx context.Context, fsys rfs.FS, dir, stream, by string, count int, ats []float64) (*ExactHistory, error) {
	args := []string{"-c", exactHistoryScript, dir, stream, strconv.Itoa(count), by}
	if ats != nil {
		raw, _ := json.Marshal(ats)
		args = append(args, string(raw))
	}
	stdout, stderr, code, err := fsys.Exec(ctx, "python3", args...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("history index: %s", stderr)
	}
	var out ExactHistory
	if err = json.Unmarshal(stdout, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
