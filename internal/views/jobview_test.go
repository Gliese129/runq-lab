package views

import (
	"strings"
	"testing"
)

// A job with no saved view: the task's own records and a scorer's are
// separate tables, rows are one per cell (varying params), and metric names
// that are not bare words still make columns.
func TestJobViews(t *testing.T) {
	rec := func(src, producer, section string, lr float64, metrics map[string]any) Record {
		return Record{SourceID: src, Producer: producer, JobID: "jb1", Section: section, TS: 1,
			Axes: map[string]any{"split": "test"}, Metrics: metrics, Params: map[string]any{"lr": lr, "seed": 1}}
	}
	in := Input{Records: []Record{
		rec("tk1", "", "auto-1a2b3c4d5", 0.1, map[string]any{"eval/acc": 0.5}),
		rec("tk2", "", "auto-1a2b3c4d5", 0.2, map[string]any{"eval/acc": 0.6}),
		rec("tk1@judge", "judge", "auto-1a2b3c4d5", 0.1, map[string]any{"eval/acc": 0.9}),
	}}
	out, err := JobViews("p", in, []string{"lr"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].Title != "results" || out[1].Title != "judge" {
		t.Fatalf("tables = %d, titles %q / %q", len(out), out[0].Title, out[len(out)-1].Title)
	}
	own := out[0]
	if strings.Join(own.RowKeys, ",") != "split,param.lr" || len(own.Rows) != 2 || len(own.Columns) != 1 || own.Columns[0].Name != "eval/acc" {
		t.Fatalf("own table: keys %v, %d rows, columns %+v", own.RowKeys, len(own.Rows), own.Columns)
	}
	if v := own.Rows[0].Cells[0].Value; v != 0.5 {
		t.Fatalf("first cell = %v, want 0.5", v)
	}
	if v := out[1].Rows[0].Cells[0].Value; v != 0.9 || len(out[1].Rows) != 1 {
		t.Fatalf("judge table = %+v", out[1].Rows)
	}
}

func TestBacktickField(t *testing.T) {
	e, err := Parse("`eval/acc` * 100")
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Eval(map[string]any{"eval/acc": 0.5}); got != 50.0 {
		t.Fatalf("eval = %v", got)
	}
	if f := e.Fields(); len(f) != 1 || f[0] != "eval/acc" {
		t.Fatalf("fields = %v", f)
	}
	if _, err := Parse("`unclosed"); err == nil {
		t.Fatal("an unclosed backtick must not parse")
	}
}
