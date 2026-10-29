package job

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func parseJob(t *testing.T, src string) JobConfig {
	t.Helper()
	var c JobConfig
	if err := yaml.Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTasksRows(t *testing.T) {
	c := parseJob(t, `
project: p
baseline: B0
fixed_params: { lr: 0.000001, seed: s1, std: on }
tasks:
  - { run: B0 }
  - { run: "B0'", seed: s2 }
  - { run: R5 Dr.GRPO, std: off, needs_code: [std] }
`)
	rows, err := Rows(&c)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[1].Run != "B0'" || rows[1].Params["seed"] != "s2" || rows[1].Params["lr"] != 0.000001 {
		t.Fatalf("rows = %+v", rows)
	}
	for _, r := range rows {
		if _, ok := r.Params["run"]; ok {
			t.Fatal("run must not be a param")
		}
		if _, ok := r.Params["needs_code"]; ok {
			t.Fatal("needs_code must not be a param")
		}
	}
	if len(rows[2].NeedsCode) != 1 || rows[2].NeedsCode[0] != "std" {
		t.Fatalf("needs_code lost: %+v", rows[2])
	}
}

func TestTasksRowsErrors(t *testing.T) {
	for src, want := range map[string]string{
		"tasks: [{lr: 1}]":                                                  "run is required",
		"tasks: [{run: a}, {run: a}]":                                       "used twice",
		"tasks: [{run: a, needs_code: [std]}]":                              "not a param",
		"baseline: z\ntasks: [{run: a}]":                                    "baseline \"z\"",
		"tasks: [{run: a}]\nsweep: [{method: grid, parameters: {lr: [1]}}]": "both sweep: and cells:",
	} {
		c := parseJob(t, src)
		if _, err := Rows(&c); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", src, err, want)
		}
	}
}

func TestSweepRowNames(t *testing.T) {
	c := parseJob(t, `
fixed_params: { lr: 0.000001 }
sweep:
  - method: grid
    parameters: { kl: [0.0, 0.1], seed: [1, 2] }
`)
	rows, err := Rows(&c)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range rows {
		names = append(names, r.Run)
	}
	if got := strings.Join(names, " | "); got != "base | seed=2 | kl=0.1 | kl=0.1, seed=2" {
		t.Fatalf("names relative to the first row: %s", got)
	}

	c.Baseline = "kl=0.1"
	rows, err = Rows(&c)
	if err != nil {
		t.Fatal(err)
	}
	if rows[2].Run != "base" || rows[0].Run != "kl=0" {
		t.Fatalf("names relative to the chosen baseline: %+v", rows)
	}

	// Identical rows get #2.
	c = parseJob(t, "sweep: [{method: list, parameters: {lr: [1, 1]}}]")
	if rows, _ = Rows(&c); rows[1].Run != "base #2" {
		t.Fatalf("duplicate name: %q", rows[1].Run)
	}
}

func TestExportNamesLegacyRows(t *testing.T) {
	rows := []Row{{Params: TaskParams{"lr": 0.1}}, {Params: TaskParams{"lr": 0.2}}}
	NameRows(rows)
	out, err := ExportYAML(ExportInput{JobID: "jb1", Project: "p", Rows: rows})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "{cell: base}") || !strings.Contains(string(out), "{cell: lr=0.2, lr: 0.2}") {
		t.Fatalf("pre-rc6 rows need derived names:\n%s", out)
	}
}

func TestRunNamesStayShort(t *testing.T) {
	c := parseJob(t, `
sweep:
  - method: list
    parameters:
      condition_id: [f1-en-en, f2-ja-en]
      query_language: [English, Japanese]
  - method: list
    parameters:
      model: [Qwen/Qwen3-14B, tokyotech-llm/Qwen3-Swallow-14B-v0.2-CPT-LR1.5E-5-iter0025000]
      model_id: [qwen3-14b, swallow]
`)
	rows, err := Rows(&c)
	if err != nil {
		t.Fatal(err)
	}
	if got := rows[3].Run; got != "condition_id=f2-ja-en, model=Qwen3-Swallow-14B-v0.2-CPT-LR1.…" {
		t.Fatalf("zip followers are left out and long values cut: %q", got)
	}
}

func TestLegacyRunNamesFollowTheStoredPlan(t *testing.T) {
	c := parseJob(t, "sweep: [{method: grid, parameters: {kl: [0.0, 0.1]}}]")
	got := LegacyRunNames(&c, []TaskParams{{"kl": 0.1}, {"kl": 0.0}, {"kl": 0.5}})
	if strings.Join(got, "|") != "kl=0.1|base|kl=0.5" {
		t.Fatalf("names = %v (planned rows by params, else diff from the first task)", got)
	}
}

// Sweep names come only from non-scheduler params (approach
// append-cell-rules): changing walltime alone does not make another cell.
func TestPlanRowsLeaveSchedulerParamsOutOfNames(t *testing.T) {
	sched := map[string]bool{"h_rt": true}
	// A zip block led (alphabetically) by the scheduler param: the cell
	// param leads the name instead.
	c := parseJob(t, `
sweep:
  - method: list
    parameters: { lr: [0.1, 0.2], h_rt: [1, 2] }
`)
	rows, err := PlanRows(&c, sched)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{rows[0].Run, rows[1].Run}; got[0] != "base" || got[1] != "lr=0.2" {
		t.Fatalf("names = %v, want [base lr=0.2]", got)
	}
	if rows[1].Params["h_rt"] != 2 {
		t.Fatalf("scheduler params stay in the row's params: %v", rows[1].Params)
	}

	// The same cell planned with another walltime keeps its name, which is
	// what --into matches on.
	again := parseJob(t, `
fixed_params: { h_rt: 9 }
sweep:
  - method: grid
    parameters: { lr: [0.1, 0.2] }
`)
	other, err := PlanRows(&again, sched)
	if err != nil {
		t.Fatal(err)
	}
	if other[1].Run != rows[1].Run {
		t.Fatalf("walltime changed the cell: %q vs %q", other[1].Run, rows[1].Run)
	}
	existing := []ExistingCell{{Name: rows[1].Run, TaskID: "t1", Status: "failed", Params: rows[1].Params}}
	cells, err := ClassifyCells(other[1:], existing, sched, nil, "")
	if err != nil || cells[0].Class == CellNew {
		t.Fatalf("append should version the existing cell, got %+v, %v", cells, err)
	}

	// Without the scheduler set, Rows still names by every param (the
	// zip block's first key leads).
	if plain, _ := Rows(&c); plain[1].Run != "h_rt=2" {
		t.Fatalf("Rows = %q", plain[1].Run)
	}
}

func TestPlanRowsRefuseCellsDifferingOnlyInScheduler(t *testing.T) {
	c := parseJob(t, `
sweep:
  - method: grid
    parameters: { lr: [0.1, 0.2], h_rt: [1, 2] }
`)
	_, err := PlanRows(&c, map[string]bool{"h_rt": true})
	if err == nil || !strings.Contains(err.Error(), "differ only in scheduler params (h_rt)") {
		t.Fatalf("want a plan error naming h_rt, got %v", err)
	}
	// Fully identical rows are not a scheduler collision: " #2" as before.
	dup := parseJob(t, `
sweep:
  - method: list
    parameters: { lr: [0.1, 0.1], h_rt: [1, 1] }
`)
	rows, err := PlanRows(&dup, map[string]bool{"h_rt": true})
	if err != nil || rows[1].Run != "base #2" {
		t.Fatalf("rows = %+v, err = %v", rows, err)
	}
}

// Names runq reads itself are refused at plan time: a param named like a
// row key, and "@" in a cell name.
func TestPlanRowsRefusesRunqsNames(t *testing.T) {
	for _, c := range []struct{ yaml, want string }{
		{"project: p\nsweep:\n  - method: grid\n    parameters: {run: [1, 2]}\n", `param "run"`},
		{"project: p\nfixed_params: {cell: x}\ncells:\n  - {cell: a, lr: 1}\n", `param "cell"`},
		{"project: p\ncells:\n  - {cell: ckpt@v3, lr: 1}\n", "cannot contain @"},
	} {
		var cfg JobConfig
		if err := yaml.Unmarshal([]byte(c.yaml), &cfg); err != nil {
			t.Fatal(err)
		}
		if _, err := Rows(&cfg); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, want %s", c.yaml, err, c.want)
		}
	}
}
