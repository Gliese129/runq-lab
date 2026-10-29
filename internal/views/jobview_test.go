package views

import (
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
)

// A job with no saved view: the task's own records and a scorer's are
// separate tables; without an index axis each is a metric / value list,
// one row per (metric, cell), and metric names need not be bare words.
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
	out, _, err := JobViews("p", in, []string{"lr"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].Title != "results" || out[1].Title != "judge" {
		t.Fatalf("tables = %d, titles %q / %q", len(out), out[0].Title, out[len(out)-1].Title)
	}
	own := out[0]
	if strings.Join(own.RowKeys, ",") != "metric,split,param.lr" || len(own.Rows) != 2 || len(own.Columns) != 1 || own.Columns[0].Name != "value" || own.Rows[0].Key["metric"] != "eval/acc" {
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

func est(v, se float64, n int) map[string]any {
	return map[string]any{"value": v, "stderr": se, "n": float64(n), "show": []any{"STDERR"}, "unit": "pct", "better": "higher"}
}

// The researcher's case: pass@k series for [base, A1] × [ja, en] — rows are
// (metric, cell), columns are k, one chart per metric coloured by cell, the
// best cell per (metric, k) ranked 1 and every value printed as a percentage.
func TestDefaultViewOneIndexSeveralCells(t *testing.T) {
	var records []Record
	cells := []struct {
		model, lang string
		acc         float64
	}{{"base", "ja", 0.3}, {"base", "en", 0.4}, {"A1", "ja", 0.5}, {"A1", "en", 0.45}}
	for _, c := range cells {
		for i, k := range []float64{1, 2, 4} {
			records = append(records, Record{SourceID: "tk-" + c.model + c.lang, JobID: "ex1", Section: "PolyMath high", TS: 1,
				Axes: map[string]any{"k": k}, Index: []string{"k"},
				Metrics: map[string]any{"准确率": est(c.acc+0.1*float64(i), 0.04, 125), "交付率": est(c.acc, 0.03, 125)},
				Params:  map[string]any{"model": c.model, "lang": c.lang}})
		}
	}
	out, specs, err := JobViews("p", Input{Records: records}, []string{"model", "lang"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	r := out[0]
	if strings.Join(r.RowKeys, ",") != "metric,param.model,param.lang" || len(r.Rows) != 8 {
		t.Fatalf("keys %v, %d rows", r.RowKeys, len(r.Rows))
	}
	if names := []string{r.Columns[0].Name, r.Columns[1].Name, r.Columns[2].Name}; strings.Join(names, ",") != "k=1,k=2,k=4" {
		t.Fatalf("columns = %v", names)
	}
	first := r.Rows[0]
	if first.Key["metric"] != "交付率" || first.Key["param.model"] != "A1" || first.Cells[0].Text != "45% ± 3.0%" {
		t.Fatalf("first row %v: %q", first.Key, first.Cells[0].Text)
	}
	acc := r.Rows[7] // 准确率, base-ja (rows sort by their keys in rows order: A1-en, A1-ja, base-en, base-ja)
	if acc.Key["metric"] != "准确率" || acc.Cells[2].Text != "50% ± 4.0%" || *acc.Cells[2].Lo != 0.46 || acc.Cells[2].Rank != 0 {
		t.Fatalf("准确率 base-ja k=4: %+v", acc.Cells[2])
	}
	if best := r.Rows[5]; best.Key["param.model"] != "A1" || best.Key["param.lang"] != "ja" || best.Cells[0].Rank != 1 {
		t.Fatalf("A1-ja should be best: %+v", best)
	}
	if len(specs[0].Charts) != 2 || specs[0].Charts[0]["y"] != "交付率" {
		t.Fatalf("charts = %v", specs[0].Charts)
	}
	if specs[0].Name != "polymath-high" {
		t.Fatalf("name = %q", specs[0].Name)
	}
}

func TestDefaultViewOneIndexOneCellAndTwoIndexes(t *testing.T) {
	one := []Record{}
	for _, k := range []float64{1, 2} {
		one = append(one, Record{SourceID: "tk1", JobID: "ex1", Section: "s", Axes: map[string]any{"k": k}, Index: []string{"k"},
			Metrics: map[string]any{"pass": est(0.5, 0.01, 10)}})
	}
	_, specs, err := JobViews("p", Input{Records: one}, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if c := specs[0].Charts; len(c) != 1 || c[0]["color"] != MetricKey {
		t.Fatalf("one cell: one chart, a line per metric: %v", c)
	}
	two := []Record{{SourceID: "tk1", JobID: "ex1", Section: "s", Axes: map[string]any{"k": 1.0, "t": 0.7}, Index: []string{"k", "t"},
		Metrics: map[string]any{"pass": est(0.5, 0.01, 10)}}}
	out, specs, err := JobViews("p", Input{Records: two}, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if specs[0].MetricRows || len(specs[0].Charts) != 0 || strings.Join(out[0].RowKeys, ",") != "k,t" {
		t.Fatalf("two indexes: plain table, no chart: %+v / %v", specs[0], out[0].RowKeys)
	}
}

// Without a column format an estimate's value prints in full and its error
// with two significant digits (a CI's bounds to the half-width's place);
// Exact keeps every digit for the hover.
func TestEstErrorTwoDigits(t *testing.T) {
	cases := []struct {
		est         map[string]any
		want, exact string
	}{
		{est(0.4305, 0.02298553191508421, 125), "43.05% ± 2.3%", "43.05% ± 2.298553191508421%"},
		{map[string]any{"value": 1234.567, "stderr": 23.456, "show": []any{"STDERR"}}, "1234.567 ± 23", "1234.567 ± 23.456"},
		{map[string]any{"value": 12345.0, "stderr": 234.0, "show": []any{"STDERR", "N"}, "n": 9.0}, "12345 ± 230 (n=9)", "12345 ± 234 (n=9)"},
		{map[string]any{"value": 0.5, "stderr": 0.0996, "show": []any{"STDERR"}}, "0.5 ± 0.10", "0.5 ± 0.0996"},
		{map[string]any{"value": 0.43, "stderr": 0.0228, "ci95": []any{0.3853, 0.4747}, "show": []any{"CI95"}, "unit": "pct"}, "43% [38.5%, 47.5%]", "43% [38.53%, 47.47%]"},
		{map[string]any{"value": 0.43, "std": 0.2547, "show": []any{"STD"}, "unit": "pct"}, "43% ± 25%", "43% ± 25.47%"},
		{map[string]any{"value": 0.4305, "show": []any{}, "unit": "pct"}, "43.05%", ""},
		{map[string]any{"value": -0.00001, "stderr": 0.01, "show": []any{"STDERR"}}, "-0.00001 ± 0.010", "-0.00001 ± 0.01"},
	}
	for _, c := range cases {
		var cell Cell
		if err := applyEst(&cell, c.est, "", 0); err != nil || cell.Text != c.want || cell.Exact != c.exact {
			t.Errorf("%v: %q / %q (%v), want %q / %q", c.est, cell.Text, cell.Exact, err, c.want, c.exact)
		}
	}
	var cell Cell
	if err := applyEst(&cell, est(0.4305, 0.023, 125), "pct2", 0); err != nil || cell.Text != "43.05% ± 2.30%" {
		t.Errorf("a column format wins: %q (%v)", cell.Text, err)
	}
}

func TestPlainPct(t *testing.T) {
	for in, want := range map[float64]string{0.5598: "55.98%", 0.5: "50%", 1: "100%", 0.000123: "0.0123%", -0.05: "-5%", 0: "0%", 12.5: "1250%"} {
		if got := PlainPct(in); got != want {
			t.Errorf("PlainPct(%v) = %q, want %q", in, got, want)
		}
	}
}

// Records with an axis named "metric" keep metrics as columns.
func TestDefaultViewAxisNamedMetric(t *testing.T) {
	in := Input{Records: []Record{{SourceID: "tk1", JobID: "ex1", Section: "s", Axes: map[string]any{"metric": "acc", "difficulty": "low"},
		Metrics: map[string]any{"pass@1": 0.8}}}}
	out, specs, err := JobViews("p", in, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if specs[0].MetricRows || out[0].Columns[0].Name != "pass@1" {
		t.Fatalf("spec %+v columns %+v", specs[0], out[0].Columns)
	}
}

// Two cells may share every param (a rerun under another name): the
// default view keeps both, keyed by the cell's name.
func TestDefaultViewCellsWithEqualParams(t *testing.T) {
	in := Input{Records: []Record{
		{SourceID: "tk1", Cell: "R1", JobID: "ex1", Section: "eval", TS: 1, Params: map[string]any{"lr": 0.1}, Metrics: map[string]any{"acc": 0.2}},
		{SourceID: "tk2", Cell: "R2", JobID: "ex1", Section: "eval", TS: 2, Params: map[string]any{"lr": 0.1}, Metrics: map[string]any{"acc": 0.9}},
	}}
	out, specs, err := JobViews("p", in, []string{"lr"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(specs[0].Rows, CellKey) || len(out[0].Rows) != 2 {
		t.Fatalf("rows %v: %d rows", specs[0].Rows, len(out[0].Rows))
	}
	got := map[any]any{}
	for _, r := range out[0].Rows {
		got[r.Key[CellKey]] = r.Cells[0].Value
	}
	if got["R1"] != 0.2 || got["R2"] != 0.9 {
		t.Fatalf("values by cell: %v", got)
	}
	// Params that do tell cells apart stay the only cell key.
	in.Records[1].Params = map[string]any{"lr": 0.2}
	_, specs, _ = JobViews("p", in, []string{"lr"}, 1)
	if contains(specs[0].Rows, CellKey) {
		t.Fatalf("rows %v: cell key not needed", specs[0].Rows)
	}
}

// A default view's spec names its producer, so saving it (through YAML)
// renders the same rows: the version's own results, not a scoring
// script's in the same section.
func TestDefaultSpecKeepsProducer(t *testing.T) {
	in := Input{Records: []Record{
		{SourceID: "tk1", Cell: "R1", JobID: "ex1", Section: "eval", TS: 1, Metrics: map[string]any{"acc": 0.2}},
		{SourceID: "tk1@judge", Cell: "R1", Producer: "judge", JobID: "ex1", Section: "eval", TS: 2, Metrics: map[string]any{"acc": 0.9}},
	}}
	out, specs, err := JobViews("p", in, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i, spec := range specs {
		raw, err := yaml.Marshal(spec)
		if err != nil {
			t.Fatal(err)
		}
		var saved Spec
		if err := yaml.Unmarshal(raw, &saved); err != nil {
			t.Fatal(err)
		}
		again, err := Render(saved, in, 1)
		if err != nil {
			t.Fatal(err)
		}
		if a, b := out[i].Rows[0].Cells[0].Value, again.Rows[0].Cells[0].Value; a != b {
			t.Fatalf("%s: default %v, saved %v\n%s", spec.Name, a, b, raw)
		}
	}
	if out[0].Rows[0].Cells[0].Value != 0.2 || out[1].Rows[0].Cells[0].Value != 0.9 {
		t.Fatalf("own then judge: %v, %v", out[0].Rows[0].Cells[0].Value, out[1].Rows[0].Cells[0].Value)
	}
}

// CSV keeps an estimate's parts, not just its value.
func TestCSVKeepsEstParts(t *testing.T) {
	in := Input{Records: []Record{{SourceID: "tk1", JobID: "ex1", Section: "eval",
		Metrics: map[string]any{"acc": map[string]any{"value": 0.5, "stderr": 0.04, "n": 125.0, "ci95": []any{0.42, 0.58}, "show": []any{"STDERR"}}}}}}
	out, _, err := JobViews("p", in, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	csv, err := Export(out[0], "csv", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(csv, "value.stderr,value.ci95_lo,value.ci95_hi") || !strings.Contains(csv, ",0.5,,,125,0.04,0.42,0.58") {
		t.Fatalf("csv:\n%s", csv)
	}
}

// Metrics list in the order the writer gave them, not by name.
func TestDefaultViewKeepsMetricOrder(t *testing.T) {
	var records []Record
	for _, c := range []string{"A1", "base"} {
		for _, k := range []float64{1, 2} {
			records = append(records, Record{SourceID: "tk-" + c, Cell: c, JobID: "ex1", Section: "PolyMath high", TS: 1,
				Axes: map[string]any{"k": k}, Index: []string{"k"}, Params: map[string]any{"model": c},
				Metrics:     map[string]any{"准确率": est(0.5, 0.04, 125), "交付率": est(0.4, 0.03, 125), "a": 1.0},
				MetricOrder: []string{"准确率", "交付率", "a"}})
		}
	}
	out, specs, err := JobViews("p", Input{Records: records}, []string{"model"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, r := range out[0].Rows {
		if m := r.Key["metric"].(string); len(got) == 0 || got[len(got)-1] != m {
			got = append(got, m)
		}
	}
	if strings.Join(got, ",") != "准确率,交付率,a" {
		t.Fatalf("row metrics %v", got)
	}
	if specs[0].Charts[0]["y"] != "准确率" || specs[0].Columns[0].Name != "准确率" {
		t.Fatalf("charts %v columns %v", specs[0].Charts, specs[0].Columns)
	}
}

// A record's "cell" axis (written before the SDK refused it) never takes
// the version's cell: two cells with the same params stay two rows.
func TestCellAxisNeverReplacesTheCell(t *testing.T) {
	rec := func(src, cell string, acc float64) Record {
		return Record{SourceID: src, JobID: "jb1", Cell: cell, Section: "eval", TS: 1,
			Axes: map[string]any{"cell": "test"}, Metrics: map[string]any{"acc": acc}, Params: map[string]any{"lr": 0.1}}
	}
	in := Input{Records: []Record{rec("tk1", "a", 0.5), rec("tk2", "b", 0.6)}}
	out, _, err := JobViews("p", in, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || len(out[0].Rows) != 2 {
		t.Fatalf("rows = %+v", out)
	}
	cells := map[any]bool{}
	for _, r := range out[0].Rows {
		cells[r.Key["cell"]] = true
	}
	if !cells["a"] || !cells["b"] {
		t.Fatalf("row cells = %v, want a and b", cells)
	}
}
