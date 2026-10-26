package job

import "testing"

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
