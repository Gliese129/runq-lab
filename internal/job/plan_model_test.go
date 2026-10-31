package job

import (
	"strings"
	"testing"
)

// The one plan layout: values compare by type (1 and "1" differ), a
// scheduler param is listed apart, nulls and lists read the same everywhere.
func TestBuildPlan(t *testing.T) {
	rows := []Row{
		{Run: "base", Params: TaskParams{"seed": float64(1), "lr": 0.1, "h_rt": "1:00", "tags": []any{float64(1), float64(2)}}},
		{Run: "s", Params: TaskParams{"seed": "1", "lr": 0.1, "h_rt": "2:00", "tags": []any{float64(1), float64(2)}}},
		{Run: "x", Params: TaskParams{"seed": float64(1), "lr": nil, "h_rt": "1:00", "tags": []any{float64(1), float64(2)}}},
	}
	m := BuildPlan(rows, "", []string{"lr"}, map[string]bool{"h_rt": true})
	if strings.Join(m.Columns, ",") != "lr,seed" {
		t.Fatalf("columns = %v", m.Columns)
	}
	if len(m.Fixed) != 1 || m.Fixed[0] != [2]string{"tags", "[1,2]"} {
		t.Fatalf("fixed = %v", m.Fixed)
	}
	if len(m.Scheduler) != 1 || m.Scheduler[0] != [2]string{"h_rt", "1:00 / 2:00"} {
		t.Fatalf("scheduler = %v", m.Scheduler)
	}
	// h_rt differs between cells: each row says its own value
	if strings.Join(m.SchedulerVaries, ",") != "h_rt" || m.Rows[0].Sched["h_rt"] != "1:00" || m.Rows[1].Sched["h_rt"] != "2:00" {
		t.Fatalf("scheduler varies = %v, sched = %v %v", m.SchedulerVaries, m.Rows[0].Sched, m.Rows[1].Sched)
	}
	// seed "1" differs from 1; lr null reads ∅ and differs from 0.1
	if c := m.Rows[1].Cells[1]; c.Same || c.Text != "1" {
		t.Errorf(`seed "1" vs 1: %+v`, c)
	}
	if c := m.Rows[2].Cells[0]; c.Same || c.Text != "∅" || c.Missing {
		t.Errorf("lr null: %+v", c)
	}
	if c := m.Rows[2].Cells[1]; !c.Same {
		t.Errorf("seed 1 vs 1: %+v", c)
	}
	text := RenderPlan(m)
	if !strings.Contains(text, "scheduler: h_rt=1:00 / 2:00") || !strings.Contains(text, "fixed: tags=[1,2]") {
		t.Fatalf("render:\n%s", text)
	}
}
