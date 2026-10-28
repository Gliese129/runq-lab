package job

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestClassifyCells(t *testing.T) {
	existing := []ExistingCell{
		{Name: "base", TaskID: "t1", Status: "success", Params: TaskParams{"lr": 1.0, "h_rt": "1:00"}, Command: "python a.py"},
		{Name: "lr=2", TaskID: "t2", Status: "failed", Params: TaskParams{"lr": 2.0}, Command: "python a.py"},
		{Name: "lr=3", TaskID: "t3", Status: "killed", InFlight: true, Params: TaskParams{"lr": 3.0}},
	}
	rows := []Row{
		{Run: "base", Params: TaskParams{"lr": 1, "h_rt": "4:00"}}, // 1 == 1.0; h_rt is the scheduler's
		{Run: "lr=2", Params: TaskParams{"lr": 2}},
		{Run: "lr=3", Params: TaskParams{"lr": 3}},
		{Run: "lr=4", Params: TaskParams{"lr": 4}},
		{Run: "again", Params: TaskParams{"lr": 4}}, // same params, another name: another cell
	}
	exclude := map[string]bool{"h_rt": true}
	got, err := ClassifyCells(rows, existing, exclude, nil, "python  a.py")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{CellSkipped, CellNewVersion, CellSkipped, CellNew, CellNew}
	for i := range want {
		if got[i].Class != want[i] {
			t.Errorf("row %s: %s, want %s", rows[i].Run, got[i].Class, want[i])
		}
	}
	if got[1].TaskID != "t2" || got[1].CommandChanged {
		t.Errorf("lr=2: %+v (whitespace is not a command change)", got[1])
	}

	got, err = ClassifyCells(rows[:2], existing, exclude, []string{"base"}, "venv/bin/python a.py")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Class != CellRerun || !got[0].CommandChanged || !got[1].CommandChanged {
		t.Errorf("rerun with another command: %+v", got)
	}
	s := SummarizeCells("jb1", 2, got)
	if s.Rerun != 1 || s.NewVersion != 1 || s.Submits() != 2 {
		t.Errorf("summary: %+v", s)
	}
}

func TestClassifyCellsRefuses(t *testing.T) {
	existing := []ExistingCell{
		{Name: "base", TaskID: "t1", Status: "success", Params: TaskParams{"lr": 1.0}},
		{Name: "run", TaskID: "t2", Status: "running", InFlight: true, Params: TaskParams{"lr": 2.0}},
	}
	for name, tc := range map[string]struct {
		rows  []Row
		rerun []string
		want  string
	}{
		"other params":       {[]Row{{Run: "base", Params: TaskParams{"lr": 2}}}, nil, `lr=2 (the cell has 1)`},
		"param added":        {[]Row{{Run: "base", Params: TaskParams{"lr": 1, "wd": 0.1}}}, nil, `wd=0.1 (the cell has none)`},
		"rerun unknown cell": {nil, []string{"nope"}, `--rerun "nope": the job has no such cell`},
		"rerun running cell": {nil, []string{"run"}, `still queued or running`},
	} {
		_, err := ClassifyCells(tc.rows, existing, nil, tc.rerun, "")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
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
