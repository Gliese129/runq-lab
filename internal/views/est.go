package views

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// MetricKey is the row key metric_rows adds: the metric a row shows.
const MetricKey = "metric"

// asEst reports whether v is an estimate written by runq.stats: an object
// with a "value" (null when no unit had one).
func asEst(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	_, has := m["value"]
	return m, has
}

// applyEst fills a cell from an estimate: its value, spread and the error
// it shows. format (the column's) wins over the estimate's unit; without
// one, every number in the cell is rounded to the place of the error's
// second significant digit (errorDecimals).
func applyEst(cell *Cell, est map[string]any, format string, now int64) error {
	value, ok := est["value"].(float64)
	cell.Value = nil
	if v, ok := est["std"].(float64); ok {
		cell.Std = &v
	}
	if v, ok := est["stderr"].(float64); ok {
		cell.SEM = &v
	}
	if v, ok := est["n"].(float64); ok {
		n := int(v)
		cell.N = &n
	}
	if raw, ok := est["show"].([]any); ok {
		for _, s := range raw {
			if s, ok := s.(string); ok {
				cell.Show = append(cell.Show, s)
			}
		}
	}
	cell.Unit, _ = est["unit"].(string)
	cell.Better, _ = est["better"].(string)
	if !ok {
		cell.Text = "--"
		return nil
	}
	cell.Value = value
	pct := cell.Unit == "pct"
	decimals, rounded := errorDecimals(est, cell, pct)
	num := func(v float64) (string, error) {
		switch {
		case format != "":
			return Format(v, format, now)
		case rounded:
			return fixed(v, decimals, pct), nil
		case pct:
			return PlainPct(v), nil
		}
		return Plain(v), nil
	}
	text, err := num(value)
	if err != nil {
		return err
	}
	for _, s := range cell.Show {
		var lo, hi float64
		switch s {
		case "STD", "STDERR":
			spread := cell.Std
			if s == "STDERR" {
				spread = cell.SEM
			}
			if spread == nil {
				continue
			}
			lo, hi = value-*spread, value+*spread
			t, err := num(*spread)
			if err != nil {
				return err
			}
			text += " ± " + t
		case "CI95":
			bounds, ok := est["ci95"].([]any)
			if !ok || len(bounds) != 2 {
				continue
			}
			l, lok := bounds[0].(float64)
			h, hok := bounds[1].(float64)
			if !lok || !hok {
				continue
			}
			lo, hi = l, h
			lt, err := num(l)
			if err != nil {
				return err
			}
			ht, _ := num(h)
			text += " [" + lt + ", " + ht + "]"
		default:
			continue
		}
		cell.Lo, cell.Hi = &lo, &hi
	}
	if contains(cell.Show, "N") && cell.N != nil {
		text += fmt.Sprintf(" (n=%d)", *cell.N)
	}
	cell.Text = text
	return nil
}

// errorDecimals is the decimal place (in the printed unit, so ×100 for a
// pct) of the error's second significant digit: the value's stderr, else
// the spread or CI half-width it shows. false when it has no error.
func errorDecimals(est map[string]any, cell *Cell, pct bool) (int, bool) {
	err := 0.0
	switch {
	case cell.SEM != nil:
		err = *cell.SEM
	case cell.Std != nil && contains(cell.Show, "STD"):
		err = *cell.Std
	default:
		if b, ok := est["ci95"].([]any); ok && len(b) == 2 {
			l, lok := b[0].(float64)
			h, hok := b[1].(float64)
			if lok && hok {
				err = (h - l) / 2
			}
		}
	}
	if pct {
		err *= 100
	}
	if !(err > 0) || math.IsInf(err, 0) {
		return 0, false
	}
	d := 1 - int(math.Floor(math.Log10(err)))
	// 0.0996 rounds to 0.10: keep two significant digits, not 0.100.
	if math.Round(err*math.Pow10(d)) >= 100 {
		d--
	}
	return d, true
}

// fixed prints v with d decimals (d < 0 rounds to tens, hundreds, …), as a
// percentage when pct.
func fixed(v float64, d int, pct bool) string {
	if pct {
		v *= 100
	}
	if d < 0 {
		p := math.Pow10(-d)
		v, d = math.Round(v/p)*p, 0
	}
	s := strconv.FormatFloat(v, 'f', d, 64)
	if strings.Trim(s, "-0.") == "" {
		s = strings.TrimPrefix(s, "-")
	}
	if pct {
		s += "%"
	}
	return s
}

// PlainPct prints a fraction as a percentage in full decimal: the decimal
// point moves two places in the printed digits, so 0.5598 is "55.98%" and
// never 55.980000000000004%.
func PlainPct(v float64) string {
	s := Plain(v)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	whole, frac, _ := strings.Cut(s, ".")
	frac += "00"
	whole, frac = strings.TrimLeft(whole+frac[:2], "0"), strings.TrimRight(frac[2:], "0")
	if whole == "" {
		whole = "0"
	}
	if frac != "" {
		whole += "." + frac
	}
	if whole == "0" {
		sign = ""
	}
	return sign + whole + "%"
}

// metricRows turns a table so metrics are row groups: one row per (metric,
// original row), columns = the pivot values (or one "value" column).
func metricRows(out *Result) {
	type slot struct{ group, metric int }
	groups := []any{}
	metrics := []string{}
	at := map[slot]int{}
	for i, c := range out.Columns {
		g := -1
		for j, v := range groups {
			if canonical(v) == canonical(c.Group) {
				g = j
			}
		}
		if g < 0 {
			groups = append(groups, c.Group)
			g = len(groups) - 1
		}
		m := -1
		for j, name := range metrics {
			if name == c.Name {
				m = j
			}
		}
		if m < 0 {
			metrics = append(metrics, c.Name)
			m = len(metrics) - 1
		}
		at[slot{g, m}] = i
	}
	pivoted := len(out.Columns) > 0 && out.Columns[0].Group != nil
	cols := []Column{}
	if pivoted {
		for _, g := range groups {
			cols = append(cols, Column{Name: out.PivotAxis + "=" + Plain(g), At: g})
		}
	} else {
		cols = []Column{{Name: "value"}}
		groups = []any{nil}
	}
	rows := []Row{}
	for m, name := range metrics {
		for _, r := range out.Rows {
			key := clone(r.Key)
			key[MetricKey] = name
			row := Row{Key: key, Cells: make([]Cell, len(groups)), State: r.State, UpdatedAt: r.UpdatedAt, TaskID: r.TaskID, JobID: r.JobID, Empty: r.Empty, Contributors: r.Contributors}
			for g := range groups {
				if i, ok := at[slot{g, m}]; ok {
					row.Cells[g] = r.Cells[i]
				} else {
					row.Cells[g] = Cell{Text: "--"}
				}
			}
			rows = append(rows, row)
		}
	}
	out.Columns, out.Rows = cols, rows
	out.RowKeys = append([]string{MetricKey}, out.RowKeys...)
}

// rankEst marks the best estimate (rank 1 = bold, ties too) among the rows that
// differ only in their cell (param.* keys), per column, by each estimate's
// own better. A lone value has no rank.
func rankEst(out *Result) {
	for ci := range out.Columns {
		groups := map[string][]*Cell{}
		for i := range out.Rows {
			r := &out.Rows[i]
			if ci >= len(r.Cells) {
				continue
			}
			c := &r.Cells[ci]
			if c.Better == "" {
				continue
			}
			if _, ok := number(c.Value); !ok {
				continue
			}
			k := map[string]any{"better": c.Better}
			for name, v := range r.Key {
				if !strings.HasPrefix(name, "param.") {
					k[name] = v
				}
			}
			id := canonical(k)
			groups[id] = append(groups[id], c)
		}
		for _, cells := range groups {
			if len(cells) < 2 {
				continue
			}
			vs := make([]float64, len(cells))
			for i, c := range cells {
				vs[i], _ = number(c.Value)
			}
			sorted := append([]float64{}, vs...)
			sort.Float64s(sorted)
			if cells[0].Better == "higher" {
				sort.Sort(sort.Reverse(sort.Float64Slice(sorted)))
			}
			rank := map[float64]int{}
			for _, v := range sorted {
				if rank[v] == 0 {
					rank[v] = len(rank) + 1
				}
			}
			for i, c := range cells {
				if rank[vs[i]] == 1 { // bold the best; no second place
					c.Rank = 1
				}
			}
		}
	}
}

// compareValues orders row-key values: numbers by value, before strings
// and other values, which compare as their JSON.
func compareValues(a, b any) int {
	x, xn := number(a)
	y, yn := number(b)
	switch {
	case xn && yn:
		if x < y {
			return -1
		} else if x > y {
			return 1
		}
		return 0
	case xn:
		return -1
	case yn:
		return 1
	}
	return strings.Compare(canonical(a), canonical(b))
}

// slotNames reads a chart slot: one name or a list of names.
func slotNames(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []string:
		return x
	case []any:
		out := []string{}
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
