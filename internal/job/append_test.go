package job

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestClassifyCells(t *testing.T) {
	existing := []ExistingCell{
		{TaskID: "t1", RunName: "base", Status: "success", Params: TaskParams{"lr": 1.0, "h_rt": "1:00"}},
		{TaskID: "t2", RunName: "lr=2", Status: "failed", Params: TaskParams{"lr": 2.0}},
		{TaskID: "t3", RunName: "lr=3", Status: "killed", Params: TaskParams{"lr": 3.0}},
		{TaskID: "t4", RunName: "lr=3", Status: "running", Params: TaskParams{"lr": 3.0}}, // a live run wins over its failure
	}
	rows := []Row{
		{Run: "base", Params: TaskParams{"lr": 1, "h_rt": "4:00"}}, // 1 == 1.0; h_rt is the scheduler's
		{Run: "lr=2", Params: TaskParams{"lr": 2}},
		{Run: "lr=3", Params: TaskParams{"lr": 3}},
		{Run: "lr=4", Params: TaskParams{"lr": 4}},
	}
	exclude := map[string]bool{"h_rt": true}
	got := ClassifyCells(rows, existing, exclude, false)
	want := []string{CellSkippedExisting, CellSkippedFailed, CellSkippedExisting, CellNew}
	for i := range want {
		if got[i].Class != want[i] {
			t.Errorf("row %s: %s, want %s", rows[i].Run, got[i].Class, want[i])
		}
	}
	if got[2].TaskID != "t4" {
		t.Errorf("lr=3 matched %s, want the running t4", got[2].TaskID)
	}
	rerun := ClassifyCells(rows, existing, exclude, true)
	if rerun[1].Class != CellRerunFailed || rerun[1].TaskID != "t2" {
		t.Errorf("rerun: %+v", rerun[1])
	}
	unnamed := ClassifyCells(rows[1:2], []ExistingCell{{TaskID: "old", Status: "failed", Params: TaskParams{"lr": 2.0}}}, nil, true)
	if unnamed[0].Run != "lr=2" {
		t.Errorf("a rerun of an unnamed (pre-rc6) task lost the row name: %q", unnamed[0].Run)
	}
	// Without the exclusion a changed walltime makes a different cell.
	if ClassifyCells(rows[:1], existing, nil, false)[0].Class != CellNew {
		t.Error("h_rt should count when not excluded")
	}
}

func TestSummarizeCellsRenamesTakenNames(t *testing.T) {
	cells := []CellClass{{Run: "seed=2", Class: CellNew}, {Run: "base", Class: CellSkippedExisting}, {Run: "x", Class: CellRerunFailed}}
	s := SummarizeCells("jb1", 2, cells, map[string]bool{"seed=2": true, "base": true, "x": true})
	if s.New != 1 || s.SkippedExisting != 1 || s.RerunFailed != 1 || s.Submits() != 2 {
		t.Fatalf("counts: %+v", s)
	}
	if cells[0].Run != "seed=2 (b2)" || s.Renamed["seed=2"] != "seed=2 (b2)" {
		t.Fatalf("rename: %q %v", cells[0].Run, s.Renamed)
	}
	if cells[2].Run != "x" {
		t.Error("a rerun keeps its cell's name")
	}
}

// Cell identity keeps scalar types apart and only folds numbers.
func TestCellKeyIsTyped(t *testing.T) {
	if CellKey(TaskParams{"x": true}, nil) == CellKey(TaskParams{"x": "true"}, nil) {
		t.Fatal(`true and "true" must be different cells`)
	}
	if CellKey(TaskParams{"x": 1}, nil) != CellKey(TaskParams{"x": 1.0}, nil) {
		t.Fatal("1 and 1.0 must be the same cell")
	}
	if CellKey(TaskParams{"x": []any{1, "a"}}, nil) == CellKey(TaskParams{"x": []any{"1", "a"}}, nil) {
		t.Fatal("list elements keep their types")
	}
}

// Two rows that only differ in scheduler params are one cell: a plan error.
func TestDistinctCells(t *testing.T) {
	params := []TaskParams{{"lr": 1, "h_rt": "1h"}, {"lr": 1.0, "h_rt": "2h"}}
	err := DistinctCells([]string{"A", "B"}, params, map[string]bool{"h_rt": true})
	if err == nil || !strings.Contains(err.Error(), `"A" and "B"`) || !strings.Contains(err.Error(), "h_rt") {
		t.Fatalf("err = %v", err)
	}
	if err := DistinctCells([]string{"A", "B"}, params, nil); err != nil {
		t.Fatalf("h_rt counts when it is not a scheduler param: %v", err)
	}
}

// Export leaves out only values that are really the baseline's, so a
// string "true" survives the round trip as a string.
func TestExportKeepsParamTypes(t *testing.T) {
	raw, err := ExportYAML(ExportInput{Project: "p", Rows: []Row{{Run: "A", Params: TaskParams{"x": true}}, {Run: "B", Params: TaskParams{"x": "true"}}}})
	if err != nil {
		t.Fatal(err)
	}
	var cfg JobConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	rows, err := Rows(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rows[1].Params["x"].(string); !ok {
		t.Fatalf("row B x = %#v after round trip:\n%s", rows[1].Params["x"], raw)
	}
}
