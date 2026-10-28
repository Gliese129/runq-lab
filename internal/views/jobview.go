package views

import (
	"fmt"
	"sort"
	"strings"
)

// JobView is GET /jobs/{id}/view: a job with no saved view, as JobViews
// renders it. Specs are the views' sources (`runq view --experiment ex…
// --spec` prints them as editable YAML).
type JobView struct {
	JobID string    `json:"job_id"`
	Views []*Result `json:"views"`
	Specs []Spec    `json:"specs"`
}

// JobViews is what runq shows for one job when no saved view is named
// (`runq view --experiment ex…`, GET /jobs/{id}/view): DefaultSpecs, rendered
// by the same engine as a saved view.
func JobViews(project string, in Input, varying []string, now int64) ([]*Result, []Spec, error) {
	out := []*Result{}
	specs, groups := DefaultSpecs(project, in, varying)
	for i, spec := range specs {
		r, err := Render(spec, Input{Records: groups[i], Sources: in.Sources, Extras: in.Extras}, now)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, r)
	}
	return out, specs, nil
}

// DefaultSpecs lays out one view per (producer, section) of a job's
// records, so a scoring script never hides the version's own results. The
// layout follows what the records say and nothing else:
//
//   - one index axis (a runq.stats series, e.g. k): rows = metrics, columns
//     = the index values, and a chart along the index — one line per metric,
//     or with several cells one chart per metric, a line per cell;
//   - no index axis: a metric / value list, no chart;
//   - two or more index axes: a plain table with the axes as row keys, no
//     default chart.
//
// Several cells add the params in varying the records carry as row keys
// (one row per cell); in holds this job's records only. groups[i] are the
// records specs[i] reads.
func DefaultSpecs(project string, in Input, varying []string) (specs []Spec, groups [][]Record) {
	type group struct{ producer, section string }
	byGroup := map[group][]Record{}
	for _, r := range in.Records {
		g := group{r.Producer, r.Section}
		byGroup[g] = append(byGroup[g], r)
	}
	order := make([]group, 0, len(byGroup))
	for g := range byGroup {
		order = append(order, g)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].producer != order[j].producer {
			return order[i].producer < order[j].producer // the task's own ("") first
		}
		return order[i].section < order[j].section
	})
	jobs := []string{}
	for _, r := range in.Records {
		if !contains(jobs, r.JobID) {
			jobs = append(jobs, r.JobID)
		}
	}
	names := map[string]bool{}
	for _, g := range order {
		records := byGroup[g]
		producer := g.producer
		spec := Spec{Name: viewName(g.producer, g.section, names), Project: project, Section: g.section, Producer: &producer, Title: jobViewTitle(g.producer, g.section),
			Highlight: map[string]string{"best": "bold"}} // the best per an est's own better
		if len(jobs) == 1 {
			spec.Jobs = jobs
		}
		axes, metrics, params, index := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
		for _, r := range records {
			for k := range r.Axes {
				if k != "step" {
					axes[k] = true
				}
			}
			for k := range r.Metrics {
				metrics[k] = true
			}
			for k := range r.Params {
				params[k] = true
			}
			for _, k := range r.Index {
				index[k] = true
			}
		}
		indexAxes, plainAxes := []string{}, []string{}
		for _, k := range keys(axes) {
			if index[k] {
				indexAxes = append(indexAxes, k)
			} else {
				plainAxes = append(plainAxes, k)
			}
		}
		cells := []string{}
		for _, k := range varying {
			if params[k] {
				cells = append(cells, "param."+k)
			}
		}
		if !axes[CellKey] && !paramsSeparate(records, cells) {
			cells = append(cells, CellKey)
		}
		spec.Rows = append(append(append([]string{}, plainAxes...), indexAxes...), cells...)
		for _, m := range keys(metrics) {
			// Section-qualified: a metric may share its name with an axis.
			spec.Columns = append(spec.Columns, Column{Name: m, Value: "`" + g.section + "." + m + "`"})
		}
		// An axis already named "metric" keeps metrics as columns.
		byMetric := !axes[MetricKey]
		switch len(indexAxes) {
		case 0:
			spec.MetricRows = byMetric
		case 1:
			axis := indexAxes[0]
			spec.Pivot = &Pivot{Axis: axis}
			spec.MetricRows = byMetric
			color := append(append([]string{}, plainAxes...), cells...)
			if len(color) == 0 && byMetric {
				ys := keys(metrics)
				spec.Charts = []map[string]any{{"template": "sweep", "x": axis, "y": ys, "color": MetricKey}}
			} else {
				for _, m := range keys(metrics) {
					chart := map[string]any{"template": "sweep", "x": axis, "y": m}
					if len(color) > 0 {
						chart["color"] = slotValue(color)
					}
					spec.Charts = append(spec.Charts, chart)
				}
			}
		}
		specs = append(specs, spec)
		groups = append(groups, records)
	}
	return specs, groups
}

// paramsSeparate reports whether the param keys tell every two cells in
// records apart. Cells may share all their params (a rerun under another
// name); then the cell's name has to be a row key too.
func paramsSeparate(records []Record, keys []string) bool {
	seen := map[string]string{}
	for _, r := range records {
		p := map[string]any{}
		for _, k := range keys {
			p[k] = r.Params[strings.TrimPrefix(k, "param.")]
		}
		id, cell := canonical(p), cellOf(r)
		if prev, ok := seen[id]; ok && prev != cell {
			return false
		}
		seen[id] = cell
	}
	return true
}

// slotValue is one name as a string, several as a list.
func slotValue(names []string) any {
	if len(names) == 1 {
		return names[0]
	}
	return names
}

// viewName is a saveable view name for a default view: the section (and
// producer) in lower case with dashes, unique within the job.
func viewName(producer, section string, taken map[string]bool) string {
	base := "results"
	if section != "" && !strings.HasPrefix(section, "auto-") {
		base = section
	}
	if producer != "" {
		base = producer + "-" + base
	}
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r == ' ' || r == '/' || r == '\\' || r == '.':
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	name := b.String()
	for i := 2; taken[name]; i++ {
		name = fmt.Sprintf("%s-%d", b.String(), i)
	}
	taken[name] = true
	return name
}

// jobViewTitle names a table by what the script chose: its producer and a
// named section; "auto-…" sections are derived from the record's shape.
func jobViewTitle(producer, section string) string {
	parts := []string{}
	if producer != "" {
		parts = append(parts, producer)
	}
	if section != "" && !strings.HasPrefix(section, "auto-") {
		parts = append(parts, section)
	}
	if len(parts) == 0 {
		return "results"
	}
	return strings.Join(parts, " · ")
}
