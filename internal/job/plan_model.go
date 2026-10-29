package job

import (
	"encoding/json"
	"strings"
)

// PlanModel is a plan as a table: one row per cell, one column per param
// that varies, a cell equal to the baseline's left blank — the table reads
// as "what each cell changes". Params only the scheduler reads (walltime,
// reservation …) are listed apart and never make a column. It is the one
// layout of a plan: `runq draft` / `runq show` print it (RenderPlan), the
// dashboard draws it.
type PlanModel struct {
	Columns       []string    `json:"columns"`
	Fixed         [][2]string `json:"fixed"`
	Scheduler     [][2]string `json:"scheduler"`
	BaselineIndex int         `json:"baseline_index"`
	Rows          []PlanRow   `json:"rows"`
}

type PlanRow struct {
	Run       string     `json:"run"`
	Baseline  bool       `json:"baseline"`
	NeedsCode []string   `json:"needs_code"`
	Cells     []PlanCell `json:"cells"`
}

type PlanCell struct {
	Text    string `json:"text"`
	Same    bool   `json:"same"`
	Missing bool   `json:"missing"`
}

// PlanText is how a plan shows a value: FormatValue's full decimals, ∅ for
// null, JSON for a list or map.
func PlanText(v any) string {
	switch v.(type) {
	case nil:
		return "∅"
	case []any, map[string]any:
		raw, err := json.Marshal(v)
		if err == nil {
			return string(raw)
		}
	}
	return FormatValue(v)
}

// BuildPlan lays out rows; declared is the project's param order (others
// follow sorted), scheduler the params only the scheduler reads. Values
// compare with SameValue (1 and "1" differ).
func BuildPlan(rows []Row, baseline string, declared []string, scheduler map[string]bool) PlanModel {
	m := PlanModel{Columns: []string{}, Fixed: [][2]string{}, Scheduler: [][2]string{}, Rows: []PlanRow{}}
	if len(rows) == 0 {
		return m
	}
	b := BaselineIndex(rows, baseline)
	m.BaselineIndex = b
	base := rows[b].Params
	for _, k := range paramOrder(rows, declared) {
		if scheduler[k] {
			var seen []string
			for _, r := range rows {
				if v, ok := r.Params[k]; ok {
					if text := PlanText(v); !contains(seen, text) {
						seen = append(seen, text)
					}
				}
			}
			m.Scheduler = append(m.Scheduler, [2]string{k, strings.Join(seen, " / ")})
			continue
		}
		if inEveryRow(rows, k) && !varies(rows, k) {
			m.Fixed = append(m.Fixed, [2]string{k, PlanText(base[k])})
		} else {
			m.Columns = append(m.Columns, k)
		}
	}
	for i, r := range rows {
		row := PlanRow{Run: r.Run, Baseline: i == b, NeedsCode: r.NeedsCode, Cells: []PlanCell{}}
		if row.NeedsCode == nil {
			row.NeedsCode = []string{}
		}
		for _, k := range m.Columns {
			v, ok := r.Params[k]
			bv, inBase := base[k]
			row.Cells = append(row.Cells, PlanCell{Text: planCellText(v, ok), Missing: !ok,
				Same: i != b && ok && inBase && SameValue(v, bv)})
		}
		m.Rows = append(m.Rows, row)
	}
	return m
}

func planCellText(v any, ok bool) string {
	if !ok {
		return "∅"
	}
	return PlanText(v)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// RenderPlan prints a plan model as the CLI shows it.
func RenderPlan(m PlanModel) string {
	if len(m.Rows) == 0 {
		return "(no rows)\n"
	}
	header := append([]string{"CELL"}, m.Columns...)
	header = append(header, "NOTE")
	table := [][]string{header}
	for _, r := range m.Rows {
		run := r.Run
		if r.Baseline {
			run += " (baseline)"
		}
		line := []string{run}
		for _, c := range r.Cells {
			if c.Same {
				line = append(line, "")
			} else {
				line = append(line, c.Text)
			}
		}
		note := ""
		if len(r.NeedsCode) > 0 {
			note = "needs code: " + strings.Join(r.NeedsCode, ", ")
		}
		table = append(table, append(line, note))
	}
	widths := make([]int, len(header))
	for _, line := range table {
		for i, c := range line {
			if n := len([]rune(c)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	var out strings.Builder
	for _, line := range table {
		for i, c := range line {
			out.WriteString(c)
			if i < len(line)-1 {
				out.WriteString(strings.Repeat(" ", widths[i]-len([]rune(c))+2))
			}
		}
		out.WriteString("\n")
	}
	for _, block := range []struct {
		label string
		items [][2]string
	}{{"fixed", m.Fixed}, {"scheduler", m.Scheduler}} {
		if len(block.items) == 0 {
			continue
		}
		parts := make([]string, len(block.items))
		for i, kv := range block.items {
			parts[i] = kv[0] + "=" + kv[1]
		}
		out.WriteString(block.label + ": " + strings.Join(parts, ", ") + "\n")
	}
	return out.String()
}

// PlanTable renders rows as RenderPlan prints them.
func PlanTable(rows []Row, baseline string, declared []string, scheduler map[string]bool) string {
	return RenderPlan(BuildPlan(rows, baseline, declared, scheduler))
}
