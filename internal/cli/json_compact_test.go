package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/preflight"
	"github.com/gliese129/runq-lab/internal/views"
	"github.com/spf13/cobra"
)

// Every command with --json takes --detail, so an agent can add it anywhere.
func TestEveryJSONCommandTakesDetail(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Flags().Lookup("json") != nil && c.Flags().Lookup("detail") == nil {
			t.Errorf("%s has --json but no --detail", c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// The compact task list keeps each cell's selected version and what an
// agent acts on; params, commands and scheduler fields are --detail.
func TestCompactTasks(t *testing.T) {
	exit := 1
	tasks := []backend.TaskView{
		{ID: "tk1", RunName: "a", Version: 1, SupersededBy: "tk2", Status: "failed", Params: map[string]any{"lr": 0.1}},
		{ID: "tk2", RunName: "a", Version: 2, Status: "failed", ExitCode: &exit, Params: map[string]any{"lr": 0.1},
			Command: "python train.py", ExternalID: "123", NativeState: "r", FailureDetail: "qsub said no",
			Capabilities: &backend.Capabilities{StateModel: "poll"}, Metrics: map[string]float64{"loss": 0.5},
			URL: "http://x/tk2"},
	}
	got := compactTasks(tasks)
	if len(got) != 1 || got[0].ID != "tk2" {
		t.Fatalf("want only the selected version tk2, got %+v", got)
	}
	out := marshal(t, got)
	for _, gone := range []string{"params", "command", "external_id", "native_state", "capabilities", "failure_detail"} {
		if strings.Contains(out, `"`+gone+`"`) {
			t.Errorf("compact list has %q: %s", gone, out)
		}
	}
	for _, kept := range []string{`"run_name":"a"`, `"version":2`, `"exit_code":1`, `"loss":0.5`, `"url":"http://x/tk2"`} {
		if !strings.Contains(out, kept) {
			t.Errorf("compact list lacks %s: %s", kept, out)
		}
	}
	if show := marshal(t, compactTaskShow(tasks[1])); !strings.Contains(show, `"failure_detail":"qsub said no"`) {
		t.Errorf("show tk… should keep the failure evidence: %s", show)
	}
}

func TestCompactJobDetail(t *testing.T) {
	d := &backend.JobDetail{
		Job:             backend.JobSummary{ID: "ex1", Name: "n", Description: "long text", TargetSource: "project"},
		Tasks:           []backend.TaskView{{ID: "tk1", Status: "done", Params: map[string]any{"seed": 1}}},
		Config:          json.RawMessage(`{"command":"x"}`),
		Plan:            &job.PlanModel{},
		SchedulerParams: []string{"h_rt"},
		Capabilities:    &backend.Capabilities{},
		Batches:         []backend.JobBatchView{{Seq: 1, TaskCount: 1, Command: "python x.py", DraftID: "dr1"}},
	}
	out := marshal(t, compactJobDetail(d, nil))
	for _, gone := range []string{"config", "plan", "scheduler_params", "capabilities", "params", "command", "description", "target_source"} {
		if strings.Contains(out, `"`+gone+`"`) {
			t.Errorf("compact show ex… has %q: %s", gone, out)
		}
	}
	if !strings.Contains(out, `"draft_id":"dr1"`) || !strings.Contains(out, `"id":"tk1"`) {
		t.Errorf("compact show ex… lost its refs: %s", out)
	}
}

func TestCompactDraftResult(t *testing.T) {
	r := &backend.DraftResult{
		Draft:   backend.DraftView{ID: "dr1", Description: "why", SourcePath: "job.yaml"},
		Table:   "CELL\n",
		Preview: "run.sh …", YAML: "rev: 1", Rows: []job.Row{{}},
		Preflight: []preflight.CheckResult{{Name: "a", Status: "passed"}, {Name: "b", Status: "warning", Detail: "d"}},
		Append:    &job.AppendSummary{Into: "ex1", New: 1, Cells: []job.CellClass{{Run: "a", Class: "new"}}},
	}
	out := marshal(t, compactDraftResult(r))
	for _, gone := range []string{`"preview"`, `"yaml"`, `"rows":[`, `"plan"`, `"cells"`, `"description"`, `"source_path"`, `"passed"`} {
		if strings.Contains(out, gone) {
			t.Errorf("compact draft has %s: %s", gone, out)
		}
	}
	if !strings.Contains(out, `"table":"CELL\n"`) || !strings.Contains(out, `"warning"`) || !strings.Contains(out, `"new":1`) {
		t.Errorf("compact draft lost the table, a warning or the append counts: %s", out)
	}
}

func TestCompactView(t *testing.T) {
	mean := 0.5
	r := &views.Result{Name: "v", RowKeys: []string{"series"},
		Columns: []views.Column{{Name: "acc", Format: "pct1", Value: "acc"}},
		Rows: []views.Row{{Key: map[string]any{"series": "a"},
			Cells: []views.Cell{{Value: 0.5, Text: "50.0%", Mean: &mean, Show: []string{"std"}}}}},
		Charts:     []map[string]any{{"type": "line"}},
		KeyFormats: map[string]string{"acc": "pct1"},
	}
	out := marshal(t, compactView(r))
	for _, gone := range []string{"charts", "key_formats", "mean", "show", "format"} {
		if strings.Contains(out, `"`+gone+`"`) {
			t.Errorf("compact view has %q: %s", gone, out)
		}
	}
	if !strings.Contains(out, `"value":0.5,"text":"50.0%"`) {
		t.Errorf("compact view lost a cell: %s", out)
	}
}
