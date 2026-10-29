package views

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type Record struct {
	// SourceID is the task id, or "<task>@<producer>" for results a script
	// wrote with runq.job() (Producer then names it); cs… = a removed consumer (read-only).
	SourceID string         `json:"source_id"`
	Producer string         `json:"producer,omitempty"`
	JobID    string         `json:"job_id"`
	Section  string         `json:"section"`
	TS       int64          `json:"ts"`
	Axes     map[string]any `json:"axes"`
	// Index names the axes that came from a runq.stats series (k for
	// pass@k): the default view lays out and plots along these only.
	Index   []string       `json:"index,omitempty"`
	Metrics map[string]any `json:"metrics"`
	// MetricOrder is the order the writer gave Metrics (runq.result keeps
	// a section's first call's order); default views list metrics in it.
	MetricOrder []string       `json:"metric_order,omitempty"`
	Params      map[string]any `json:"params,omitempty"`
	// Cell is the name of the cell the record's version belongs to: its
	// identity, which params alone are not (two cells may share them).
	Cell string `json:"cell,omitempty"`
	// The version the record came from: its status, its number within
	// its cell, and whether the researcher pinned it (else it is the
	// cell's latest).
	Status  string `json:"status,omitempty"`
	Version int    `json:"version,omitempty"`
	Pinned  bool   `json:"pinned,omitempty"`
}
type Contributor struct {
	SourceID  string `json:"source_id"`
	Producer  string `json:"producer,omitempty"`
	JobID     string `json:"job_id"`
	Section   string `json:"section"`
	UpdatedAt int64  `json:"updated_at"`
	Status    string `json:"status,omitempty"`
	Version   int    `json:"version,omitempty"`
	Pinned    bool   `json:"pinned,omitempty"`
}
type Source struct {
	JobID            string           `json:"job_id"`
	Note             string           `json:"note"`
	Status           string           `json:"status"`
	LastDataAt       *int64           `json:"last_data_at"`
	Tasks            int              `json:"tasks"`
	Running          int              `json:"running"`
	ObservedAt       *int64           `json:"observed_at"`
	ObservationState string           `json:"observation_state"`
	LastHeartbeatAt  *int64           `json:"last_heartbeat_at"`
	Attention        []map[string]any `json:"attention"`
	Incomplete       bool             `json:"incomplete,omitempty"`
	Warnings         []string         `json:"warnings,omitempty"`
}
type Data struct {
	State     string `json:"state"`
	UpdatedAt *int64 `json:"updated_at"`
	Value     any    `json:"value"`
	Error     string `json:"error,omitempty"`
}
type Cell struct {
	Value     any      `json:"value"`
	Text      string   `json:"text"`
	Mean      *float64 `json:"mean,omitempty"`
	Std       *float64 `json:"std,omitempty"`
	N         *int     `json:"n,omitempty"`
	SEM       *float64 `json:"sem,omitempty"`
	CI95      *float64 `json:"ci95,omitempty"`
	Rank      int      `json:"rank,omitempty"`
	Delta     *float64 `json:"delta,omitempty"`
	DeltaText string   `json:"delta_text,omitempty"`
	// An estimate's own presentation (runq.stats): which of std / stderr /
	// ci95 / n it shows, its unit and better direction, and the band the
	// shown error spans (lo..hi), which the table's ± and the chart share.
	Show   []string `json:"show,omitempty"`
	Unit   string   `json:"unit,omitempty"`
	Better string   `json:"better,omitempty"`
	Lo     *float64 `json:"lo,omitempty"`
	Hi     *float64 `json:"hi,omitempty"`
	// CILo / CIHi are an estimate's 95% interval, shown or not.
	CILo *float64 `json:"ci95_lo,omitempty"`
	CIHi *float64 `json:"ci95_hi,omitempty"`
	// Exact is an estimate's text in full decimal when Text rounds its
	// error (the dashboard shows it on hover).
	Exact        string        `json:"exact,omitempty"`
	Contributors []Contributor `json:"contributors,omitempty"`
}
type Row struct {
	Key          map[string]any `json:"key"`
	Cells        []Cell         `json:"cells"`
	State        string         `json:"state,omitempty"`
	UpdatedAt    *int64         `json:"updated_at"`
	TaskID       string         `json:"task_id,omitempty"`
	JobID        string         `json:"job_id,omitempty"`
	Empty        bool           `json:"empty"`
	Contributors []Contributor  `json:"contributors,omitempty"`
}
type ExtraCell struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
	Text  string `json:"text"`
	Agg   string `json:"agg,omitempty"`
}
type Result struct {
	Name        string            `json:"name"`
	Title       string            `json:"title"`
	GeneratedAt int64             `json:"generated_at"`
	Sources     []Source          `json:"sources"`
	Extra       []ExtraCell       `json:"extra"`
	Columns     []Column          `json:"columns"`
	Rows        []Row             `json:"rows"`
	Legend      string            `json:"legend,omitempty"`
	Charts      []map[string]any  `json:"charts,omitempty"`
	Data        map[string]Data   `json:"data,omitempty"`
	RowKeys     []string          `json:"row_keys"`
	PivotAxis   string            `json:"pivot_axis,omitempty"` // the axis pivoted into column groups
	Highlight   map[string]string `json:"highlight,omitempty"`
	KeyFormats  map[string]string `json:"key_formats,omitempty"`
}
type Input struct {
	Records []Record
	Sources []Source
	Extras  map[string]map[string]any
}

// Plain prints a value with no format: numbers in full decimal (an 8-digit
// seed stays 12345678, 1e-6 prints 0.000001). Scientific notation is only
// ever chosen explicitly (sci0..sci3).
func Plain(v any) string {
	switch n := v.(type) {
	case nil:
		return ""
	case float64:
		if n == math.Trunc(n) && math.Abs(n) < 1e15 {
			return strconv.FormatInt(int64(n), 10)
		}
		return strconv.FormatFloat(n, 'f', -1, 64)
	case float32:
		return Plain(float64(n))
	}
	return fmt.Sprint(v)
}

// KeyText prints a row-key value with its key_formats entry, else in full.
func (r *Result) KeyText(key string, v any) string {
	if f := r.KeyFormats[key]; f != "" && v != nil {
		if s, err := Format(v, f, r.GeneratedAt); err == nil {
			return s
		}
	}
	return Plain(v)
}

func Format(v any, f string, now int64) (string, error) {
	if v == nil {
		return "--", nil
	}
	if f == "" {
		return Plain(v), nil
	}
	n, ok := number(v)
	if !ok {
		return "", fmt.Errorf("format %q requires a number, got %T", f, v)
	}
	switch {
	case f == "int":
		return fmt.Sprintf("%.0f", n), nil
	case strings.HasPrefix(f, "float"):
		d, _ := strconv.Atoi(strings.TrimPrefix(f, "float"))
		return fmt.Sprintf("%.*f", d, n), nil
	case strings.HasPrefix(f, "sci"):
		d, _ := strconv.Atoi(strings.TrimPrefix(f, "sci"))
		// 1.2e-06 → 1.2e-6: the exponent without padding or '+'.
		mant, exp, _ := strings.Cut(fmt.Sprintf("%.*e", d, n), "e")
		e, _ := strconv.Atoi(exp)
		return fmt.Sprintf("%se%d", mant, e), nil
	case strings.HasPrefix(f, "pct"):
		d, _ := strconv.Atoi(strings.TrimPrefix(f, "pct"))
		return fmt.Sprintf("%.*f%%", d, n*100), nil
	case f == "duration" || f == "ago":
		if f == "ago" {
			n = float64(now) - n
		}
		sign := ""
		if n < 0 {
			sign = "-"
			n = -n
		}
		v := int64(n)
		text := fmt.Sprintf("%ds", v)
		if v >= 86400 {
			text = fmt.Sprintf("%dd %dh", v/86400, v%86400/3600)
		} else if v >= 3600 {
			text = fmt.Sprintf("%dh %dm", v/3600, v%3600/60)
		} else if v >= 60 {
			text = fmt.Sprintf("%dm %ds", v/60, v%60)
		}
		if f == "ago" {
			text += " ago"
		}
		return sign + text, nil
	}
	return "", fmt.Errorf("invalid format %q", f)
}
func Render(s Spec, in Input, now int64) (*Result, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	out := &Result{Name: s.Name, Title: s.Title, GeneratedAt: now, Sources: in.Sources, Extra: []ExtraCell{}, Columns: append([]Column{}, s.Columns...), Rows: []Row{}, Legend: s.Legend, Charts: s.Charts, Highlight: s.Highlight, KeyFormats: s.KeyFormats}
	if out.Title == "" {
		out.Title = s.Name
	}
	if out.Sources == nil {
		out.Sources = []Source{}
	}
	// A table uses main, or the sole record source. Custom-only views need no table.
	if len(s.Sources) > 0 {
		var recordSources []DataSource
		for _, source := range s.Sources {
			if source.From == "record" {
				recordSources = append(recordSources, source)
			}
		}
		main, hasMain := s.Sources["main"]
		if hasMain && main.From == "record" {
			s.Section = main.Section
		} else if len(recordSources) == 1 {
			s.Section = recordSources[0].Section
		} else if len(s.Columns) > 0 || len(s.Rows) > 0 {
			return nil, &Invalid{[]string{"table requires main or a sole record source"}}
		}
		if len(recordSources) == 0 {
			in.Records = nil
		}
	}
	sections := append([]string{}, s.Group...)
	if s.Section != "" {
		sections = []string{s.Section}
	}
	if len(sections) == 0 {
		seen := map[string]bool{}
		for _, r := range in.Records {
			if s.Producer == nil || r.Producer == *s.Producer {
				seen[r.Section] = true
			}
		}
		sections = keys(seen)
		if len(sections) > 1 {
			return nil, &Invalid{[]string{fmt.Sprintf("choose section or group; available sections: %v", sections)}}
		}
	}
	schemas := map[string][]string{}
	fields := map[string]bool{}
	metricOwners := map[string][]string{}
	ambiguous := map[string]bool{}
	selected := []Record{}
	for _, r := range in.Records {
		if !contains(sections, r.Section) || s.Producer != nil && r.Producer != *s.Producer {
			continue
		}
		selected = append(selected, r)
		axes := []string{}
		for k := range r.Axes {
			if k != "step" {
				axes = append(axes, k)
			}
			fields[k] = true
		}
		sort.Strings(axes)
		if prev, ok := schemas[r.Section]; ok && canonical(prev) != canonical(axes) {
			return nil, &Invalid{[]string{fmt.Sprintf("source %s section %s has missing identity fields: expected %v got %v", r.SourceID, r.Section, prev, axes)}}
		}
		schemas[r.Section] = axes
		for k := range r.Metrics {
			fields[r.Section+"."+k] = true
			if !contains(metricOwners[k], r.Section) {
				metricOwners[k] = append(metricOwners[k], r.Section)
			}
		}
		for k := range r.Params {
			fields["param."+k] = true
		}
		fields[CellKey] = true
	}
	identity := []string{}
	for _, sec := range sections {
		if a, ok := schemas[sec]; ok {
			if len(identity) > 0 && canonical(identity) != canonical(a) {
				return nil, &Invalid{[]string{fmt.Sprintf("group identity differs: %v vs %v", identity, a)}}
			}
			identity = a
		}
	}
	for k, owners := range metricOwners {
		if len(owners) == 1 && !fields[k] {
			fields[k] = true
		} else {
			ambiguous[k] = true // expressions must qualify these metric names
		}
	}
	rowKeys := append([]string{}, s.Rows...)
	if len(rowKeys) == 0 {
		rowKeys = append(rowKeys, identity...)
		if len(rowKeys) == 0 {
			rowKeys = keys(s.Expect)
		}
	}
	if s.Aggregate != nil {
		for _, k := range s.Aggregate.Over {
			if !contains(rowKeys, k) {
				rowKeys = append(rowKeys, k)
			}
		}
	}
	errors := []string{}
	checkFields := func(label, expr string) {
		if expr == "" {
			return
		}
		var refs []string
		if strings.Contains(expr, "{") {
			refs, _ = templateFields(expr)
		} else {
			e, _ := Parse(expr)
			refs = e.Fields()
		}
		for _, k := range refs {
			if !fields[k] || ambiguous[k] {
				errors = append(errors, fmt.Sprintf("%s: unknown or ambiguous field %q (available: %s)", label, k, strings.Join(keys(fields), ", ")))
			}
		}
	}
	if len(selected) > 0 {
		for _, k := range identity {
			if !contains(rowKeys, k) {
				errors = append(errors, "rows missing identity field "+k)
			}
		}
		for _, k := range rowKeys {
			if !contains(identity, k) && !isCellKey(k) {
				errors = append(errors, "rows contains non-identity field "+k)
			}
			if !fields[k] {
				errors = append(errors, "unknown row field "+k)
			}
		}
		for k := range s.Filter {
			if !fields[k] {
				errors = append(errors, "unknown filter "+k)
			}
		}
		for _, c := range s.Columns {
			checkFields("column "+c.Name, c.Value)
			checkFields("when "+c.Name, c.When)
		}
		for _, r := range s.State {
			checkFields("state "+r.Label, r.When)
		}
	}
	// Freeze the display identity before removing aggregate and pivot dimensions.
	if len(s.Expect) > 0 && canonical(keys(s.Expect)) != canonical(sorted(rowKeys)) {
		errors = append(errors, fmt.Sprintf("expect must cover rows exactly: rows=%v expect=%v", rowKeys, keys(s.Expect)))
	}
	slots := map[string]bool{}
	for _, k := range rowKeys {
		slots[k] = true
	}
	for _, c := range s.Columns {
		slots[c.Name] = true
	}
	for i, c := range s.Charts {
		if c["template"] == "curves" {
			continue
		}
		for _, slot := range []string{"x", "y", "color", "facet", "label", "value", "done", "total"} {
			for _, v := range slotNames(c[slot]) {
				if !slots[v] && !(s.MetricRows && v == MetricKey) {
					errors = append(errors, fmt.Sprintf("chart %d %s: unknown %q; available %v", i, slot, v, keys(slots)))
				}
			}
		}
	}
	if len(errors) > 0 {
		return nil, &Invalid{errors}
	}
	type sample struct {
		key, values map[string]any
		records     map[string]Record
	}
	samples := map[string]*sample{}
	for _, r := range selected {
		vals := map[string]any{}
		// runq's own keys win over an axis of the same name (written
		// before the SDK refused them): the cell and params are the
		// version's, never a record's say.
		for k, v := range r.Axes {
			vals[k] = v
		}
		for k, v := range r.Params {
			vals["param."+k] = v
		}
		vals[CellKey] = cellOf(r)
		key := map[string]any{}
		for _, k := range rowKeys {
			key[k] = vals[k]
		}
		if !matches(vals, s.Filter) {
			continue
		}
		id := canonical(key)
		x := samples[id]
		if x == nil {
			x = &sample{key: key, values: vals, records: map[string]Record{}}
			samples[id] = x
		}
		prev, exists := x.records[r.Section]
		if !exists || r.TS > prev.TS || r.TS == prev.TS && r.SourceID >= prev.SourceID {
			x.records[r.Section] = r
		}
	}
	order := []string{}
	if len(s.Expect) > 0 {
		combos := []map[string]any{{}}
		for _, k := range rowKeys {
			next := []map[string]any{}
			for _, c := range combos {
				for _, v := range s.Expect[k] {
					n := clone(c)
					n[k] = v
					next = append(next, n)
				}
			}
			combos = next
		}
		for _, k := range combos {
			if !matches(k, s.Filter) {
				continue
			}
			id := canonical(k)
			order = append(order, id)
			if samples[id] == nil {
				samples[id] = &sample{key: k, values: clone(k), records: map[string]Record{}}
			}
		}
	}
	// Rows without expect: by their keys in rows order, numbers by value
	// (k = 1, 2, 4, 16 — not 1, 16, 2, 4).
	rest := []string{}
	for _, id := range keys(samples) {
		if !contains(order, id) {
			rest = append(rest, id)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		a, b := samples[rest[i]].key, samples[rest[j]].key
		for _, k := range rowKeys {
			if c := compareValues(a[k], b[k]); c != 0 {
				return c < 0
			}
		}
		return false
	})
	order = append(order, rest...)
	for _, id := range order {
		x := samples[id]
		row := Row{Key: x.key, Cells: []Cell{}, Empty: len(x.records) == 0}
		for _, sec := range sections {
			r, ok := x.records[sec]
			if !ok {
				continue
			}
			for k, v := range r.Axes {
				x.values[k] = v
			}
			for k, v := range r.Metrics {
				x.values[sec+"."+k] = v
				if len(metricOwners[k]) == 1 && !contains(identity, k) {
					x.values[k] = v
				}
			}
			c := Contributor{SourceID: r.SourceID, Producer: r.Producer, JobID: r.JobID, Section: r.Section, UpdatedAt: r.TS,
				Status: r.Status, Version: r.Version, Pinned: r.Pinned}
			row.Contributors = append(row.Contributors, c)
			if row.UpdatedAt == nil || r.TS > *row.UpdatedAt {
				ts := r.TS
				row.UpdatedAt = &ts
			}
		}
		if len(row.Contributors) == 1 {
			c := row.Contributors[0]
			if !strings.HasPrefix(c.SourceID, "cs") { // removed consumers' records have no task
				row.TaskID, _, _ = strings.Cut(c.SourceID, "@")
			}
			row.JobID = c.JobID
		}
		for _, col := range s.Columns {
			cell := Cell{Text: "--"}
			if !row.Empty {
				show := true
				if col.When != "" {
					e, _ := Parse(col.When)
					show = e.Eval(x.values) == true
				}
				if show {
					v, err := evalValue(col.Value, x.values)
					if err != nil {
						return nil, err
					}
					cell.Value = v
					if est, ok := asEst(v); ok {
						err = applyEst(&cell, est, col.Format, now)
					} else {
						cell.Text, err = Format(v, col.Format, now)
					}
					if err != nil {
						return nil, &Invalid{[]string{"column " + col.Name + ": " + err.Error()}}
					}
					cell.Contributors = row.Contributors
				} else {
					cell.Text = ""
				}
			}
			row.Cells = append(row.Cells, cell)
		}
		if !row.Empty {
			for _, state := range s.State {
				if state.When == "" {
					row.State = state.Label
					break
				}
				e, _ := Parse(state.When)
				if e.Eval(x.values) == true {
					row.State = state.Label
					break
				}
			}
		}
		out.Rows = append(out.Rows, row)
	}
	out.RowKeys = rowKeys
	if s.Aggregate != nil {
		aggregate(out, *s.Aggregate)
	}
	if s.Pivot != nil {
		if !contains(out.RowKeys, s.Pivot.Axis) {
			return nil, &Invalid{[]string{"pivot.axis must be a row dimension"}}
		}
		pivot(out, *s.Pivot)
	}
	if s.MetricRows {
		if contains(out.RowKeys, MetricKey) {
			return nil, &Invalid{[]string{"metric_rows: a row key is already named " + MetricKey}}
		}
		metricRows(out)
	}
	rankAndBaseline(out, s)
	rankEst(out)
	for _, e := range s.Extra {
		v, err := evalValue(e.Value, in.Extras[e.Job])
		if err != nil {
			return nil, err
		}
		txt, err := Format(v, e.Format, now)
		if err != nil {
			return nil, &Invalid{[]string{err.Error()}}
		}
		agg := ""
		ex, _ := Parse(e.Value)
		for _, ref := range ex.Fields() {
			if strings.HasPrefix(ref, "metric:") {
				parts := strings.Split(ref, ":")
				if len(parts) == 3 {
					agg = parts[2]
				} else {
					agg = "latest"
				}
			}
		}
		out.Extra = append(out.Extra, ExtraCell{e.Name, v, txt, agg})
	}
	return out, nil
}

// KeepsCell reports whether the view's filter keeps a cell with these
// params and name. Only the keys a cell decides (param.<name>, cell)
// count: filters on result axes apply per record in Render. Curves and
// animation frames scope their versions with it, as the table's rows are.
func (s Spec) KeepsCell(params map[string]any, cell string) bool {
	vals := map[string]any{CellKey: cell}
	for k, v := range params {
		vals["param."+k] = v
	}
	filter := map[string][]any{}
	for k, options := range s.Filter {
		if isCellKey(k) {
			filter[k] = options
		}
	}
	return matches(vals, filter)
}
func matches(values map[string]any, filter map[string][]any) bool {
	for k, options := range filter {
		ok := false
		for _, v := range options {
			if canonical(values[k]) == canonical(v) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
func clone(m map[string]any) map[string]any {
	n := map[string]any{}
	for k, v := range m {
		n[k] = v
	}
	return n
}
func aggregate(out *Result, a Aggregate) {
	groups := map[string][]Row{}
	order := []string{}
	for _, r := range out.Rows {
		k := clone(r.Key)
		for _, f := range a.Over {
			delete(k, f)
		}
		id := canonical(k)
		if _, ok := groups[id]; !ok {
			order = append(order, id)
		}
		r.Key = k
		groups[id] = append(groups[id], r)
	}
	out.Rows = []Row{}
	for _, id := range order {
		g := groups[id]
		r := Row{Key: g[0].Key, Empty: true, Cells: make([]Cell, len(out.Columns))}
		for _, sample := range g {
			if !sample.Empty {
				r.Empty = false
			}
			r.Contributors = append(r.Contributors, sample.Contributors...)
			if sample.UpdatedAt != nil && (r.UpdatedAt == nil || *sample.UpdatedAt > *r.UpdatedAt) {
				r.UpdatedAt = sample.UpdatedAt
			}
		}
		for ci, c := range out.Columns {
			values := []float64{}
			for _, sample := range g {
				if v, ok := number(sample.Cells[ci].Value); ok {
					values = append(values, v)
				}
			}
			cell := Cell{Text: "--", Contributors: r.Contributors}
			n := len(values)
			cell.N = &n
			if n > 0 {
				mean := 0.
				for _, v := range values {
					mean += v
				}
				mean /= float64(n)
				std := 0.
				for _, v := range values {
					std += (v - mean) * (v - mean)
				}
				if n > 1 {
					std = math.Sqrt(std / float64(n-1))
				}
				value := mean
				if a.Stat == "median" {
					sort.Float64s(values)
					value = values[n/2]
					if n%2 == 0 {
						value = (values[n/2-1] + value) / 2
					}
				}
				cell.Value = value
				cell.Mean = &mean
				cell.Std = &std
				cell.Text, _ = Format(value, c.Format, out.GeneratedAt)
				if n > 1 && a.Stat != "mean" && a.Stat != "median" {
					spread := std
					if a.Stat == "mean_sem" || a.Stat == "mean_ci95" {
						spread /= math.Sqrt(float64(n))
						sem := spread
						cell.SEM = &sem
					}
					if a.Stat == "mean_ci95" {
						spread *= tCritical(n - 1)
						ci := spread
						cell.CI95 = &ci
					}
					txt, _ := Format(spread, c.Format, out.GeneratedAt)
					cell.Text += " ± " + txt
				}
			}
			r.Cells[ci] = cell
		}
		out.Rows = append(out.Rows, r)
	}
	ks := []string{}
	for _, k := range out.RowKeys {
		if !contains(a.Over, k) {
			ks = append(ks, k)
		}
	}
	out.RowKeys = ks
}

// Two-sided 95% Student-t quantiles. Larger df uses a standard expansion.
func tCritical(df int) float64 {
	table := []float64{0, 12.7062047, 4.3026527, 3.1824463, 2.7764451, 2.5705818, 2.4469118, 2.3646243, 2.3060041, 2.2621572, 2.2281389, 2.2009852, 2.1788128, 2.1603687, 2.1447867, 2.1314495, 2.1199053, 2.1098156, 2.1009220, 2.0930241, 2.0859634, 2.0796138, 2.0738731, 2.0686576, 2.0638986, 2.0595386, 2.0555294, 2.0518305, 2.0484071, 2.0452296, 2.0422725}
	if df < len(table) {
		return table[df]
	}
	z := 1.95996398454005
	v := float64(df)
	return z + (z*z*z+z)/(4*v) + (5*math.Pow(z, 5)+16*z*z*z+3*z)/(96*v*v)
}
func pivot(out *Result, p Pivot) {
	out.PivotAxis = p.Axis
	vals := append([]any{}, p.Order...)
	seen := map[string]bool{}
	for _, v := range vals {
		seen[canonical(v)] = true
	}
	for _, r := range out.Rows {
		v := r.Key[p.Axis]
		if !seen[canonical(v)] {
			seen[canonical(v)] = true
			vals = append(vals, v)
		}
	}
	cols := out.Columns
	out.Columns = []Column{}
	for _, v := range vals {
		for _, c := range cols {
			c.Group = v
			out.Columns = append(out.Columns, c)
		}
	}
	groups := map[string]*Row{}
	order := []string{}
	for _, r := range out.Rows {
		k := clone(r.Key)
		v := k[p.Axis]
		delete(k, p.Axis)
		id := canonical(k)
		g := groups[id]
		if g == nil {
			g = &Row{Key: k, Empty: true, Cells: make([]Cell, len(out.Columns))}
			for i := range g.Cells {
				g.Cells[i].Text = "--"
			}
			groups[id] = g
			order = append(order, id)
		}
		for i, match := range vals {
			if canonical(v) == canonical(match) {
				copy(g.Cells[i*len(cols):], r.Cells)
				break
			}
		}
		g.Empty = g.Empty && r.Empty
		g.Contributors = append(g.Contributors, r.Contributors...)
		if r.UpdatedAt != nil && (g.UpdatedAt == nil || *r.UpdatedAt > *g.UpdatedAt) {
			g.UpdatedAt = r.UpdatedAt
		}
	}
	out.Rows = []Row{}
	for _, id := range order {
		out.Rows = append(out.Rows, *groups[id])
	}
	rowKeys := out.RowKeys
	out.RowKeys = []string{}
	for _, k := range rowKeys {
		if k != p.Axis {
			out.RowKeys = append(out.RowKeys, k)
		}
	}
}
func rankAndBaseline(out *Result, s Spec) {
	for ci, col := range out.Columns {
		if len(s.Highlight) > 0 && col.Better != "" {
			byN := map[int][]float64{}
			for _, r := range out.Rows {
				if v, ok := number(r.Cells[ci].Value); ok {
					n := 1
					if r.Cells[ci].N != nil {
						n = *r.Cells[ci].N
					}
					byN[n] = append(byN[n], v)
				}
			}
			for n, vs := range byN {
				sort.Float64s(vs)
				if col.Better == "higher" {
					sort.Sort(sort.Reverse(sort.Float64Slice(vs)))
				}
				rank := map[float64]int{}
				for _, v := range vs {
					if rank[v] == 0 {
						rank[v] = len(rank) + 1
					}
				}
				for i := range out.Rows {
					cell := &out.Rows[i].Cells[ci]
					cn := 1
					if cell.N != nil {
						cn = *cell.N
					}
					if v, ok := number(cell.Value); ok && cn == n {
						cell.Rank = rank[v]
					}
				}
			}
		}
		if len(s.Baseline) > 0 {
			for i := range out.Rows {
				r := &out.Rows[i]
				baseKey := clone(r.Key)
				same := true
				for k, v := range s.Baseline {
					if canonical(baseKey[k]) != canonical(v) {
						same = false
					}
					baseKey[k] = v
				}
				if same {
					continue
				}
				for _, b := range out.Rows {
					if canonical(b.Key) != canonical(baseKey) {
						continue
					}
					v, ok := number(r.Cells[ci].Value)
					bv, bok := number(b.Cells[ci].Value)
					if ok && bok {
						delta := v - bv
						r.Cells[ci].Delta = &delta
						if strings.HasPrefix(col.Format, "pct") {
							r.Cells[ci].DeltaText = fmt.Sprintf("%+.1fpp", 100*delta)
						} else {
							r.Cells[ci].DeltaText = fmt.Sprintf("%+.3g", delta)
						}
					}
					break
				}
			}
		}
	}
}
