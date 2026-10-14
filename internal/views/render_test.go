package views

import (
	"strings"
	"testing"
)

func TestProgressLatestAndEmpty(t *testing.T) {
	spec := Spec{Name: "eval", Project: "p", Section: "eval", Rows: []string{"model"}, Expect: map[string][]any{"model": {"a", "b"}}, Columns: []Column{{Name: "Acc", Value: "correct / scored", Format: "pct1"}, {Name: "Conditional", Value: "correct", When: "not (missing == 1)"}}, State: []State{{Label: "DONE", When: "scored >= total"}, {Label: "PARTIAL"}}}
	record := func(id string, ts int64, step, correct float64) Record {
		return Record{SourceID: id, JobID: "j", Section: "eval", TS: ts, Axes: map[string]any{"model": "a", "step": step}, Metrics: map[string]any{"correct": correct, "scored": float64(10), "total": float64(10), "missing": nil}}
	}
	out, err := Render(spec, Input{Records: []Record{record("tk-old", 1, 1000, 1), record("tk-new", 2, 0, 8)}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != 2 || out.Rows[0].Cells[0].Text != "80.0%" || out.Rows[0].TaskID != "tk-new" || out.Rows[0].State != "DONE" {
		t.Fatalf("wrong latest: %+v", out.Rows)
	}
	if out.Rows[0].Cells[1].Text != "" || !out.Rows[1].Empty || out.Rows[1].State != "" || out.Rows[1].UpdatedAt != nil {
		t.Fatalf("null/empty semantics: %+v", out.Rows)
	}
	spec.Columns[0].Value = "corret / scrd"
	_, err = Render(spec, Input{Records: []Record{record("t", 1, 0, 1)}}, 10)
	if err == nil || !strings.Contains(err.Error(), "corret") || !strings.Contains(err.Error(), "scrd") {
		t.Fatalf("field errors not combined: %v", err)
	}
}
func TestSeedAggregationBeforePivot(t *testing.T) {
	spec := Spec{Name: "paper", Project: "p", Section: "eval", Rows: []string{"model", "split", "param.seed"}, Aggregate: &Aggregate{Over: []string{"param.seed"}, Stat: "mean_std"}, Pivot: &Pivot{Axis: "split", Order: []any{"test"}}, Columns: []Column{{Name: "Acc", Value: "correct / scored", Format: "pct1", Better: "higher"}}, Highlight: map[string]string{"best": "bold"}, Baseline: map[string]any{"model": "base"}}
	rs := []Record{}
	for _, model := range []string{"base", "new"} {
		for seed := 1; seed <= 2; seed++ {
			score := float64(seed)
			if model == "new" {
				score += 2
			}
			rs = append(rs, Record{SourceID: model + string(rune('0'+seed)), JobID: "j", Section: "eval", TS: 1, Axes: map[string]any{"model": model, "split": "test"}, Params: map[string]any{"seed": float64(seed)}, Metrics: map[string]any{"correct": score, "scored": float64(10)}})
		}
	}
	out, err := Render(spec, Input{Records: rs}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != 2 {
		t.Fatalf("rows: %+v", out.Rows)
	}
	c := out.Rows[1].Cells[0]
	if c.N == nil || *c.N != 2 || c.Mean == nil || *c.Mean != .35 || c.Rank != 1 || c.DeltaText != "+20.0pp" {
		t.Fatalf("aggregate lost seed or delta: %+v", c)
	}
	text, err := Export(out, "latex", false, false)
	if err != nil || !strings.Contains(text, "\\multicolumn") || !strings.Contains(text, "\\textbf") {
		t.Fatalf("latex: %s %v", text, err)
	}
	tpl, err := RenderTemplate("{{#each rows}}{{key.model}}: {{#each cells}}{{text}} {{/each}}{{/each}}", "markdown", out)
	if err != nil || !strings.Contains(tpl, "base:") {
		t.Fatalf("template: %s %v", tpl, err)
	}
}
func TestThreeValuedExpressions(t *testing.T) {
	for expr, want := range map[string]any{"false and missing": false, "true or missing": true, "not missing": nil, "1 / 0": nil, "1 == '1'": false, "not 2 > 1": false} {
		e, err := Parse(expr)
		if err != nil {
			t.Fatal(err)
		}
		if got := e.Eval(nil); got != want {
			t.Errorf("%s = %v want %v", expr, got, want)
		}
	}
	if _, err := Parse("open('/tmp/x')"); err == nil {
		t.Fatal("function calls accepted")
	}
}
