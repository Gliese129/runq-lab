package views

import (
	"sort"
	"strings"
)

// JobView is GET /jobs/{id}/view: a job with no saved view, as JobViews
// renders it.
type JobView struct {
	JobID string    `json:"job_id"`
	Views []*Result `json:"views"`
}

// JobViews is what runq shows for one job when no saved view is named
// (`runq view --job jb…`, GET /jobs/{id}/view): the same engine as a saved
// view, one table per (producer, section) of the job's records so a scoring
// script never hides the task's own results. Rows are the records' identity
// plus the params in varying that the records carry (one row per cell);
// there is a column per metric. in holds this job's records only.
func JobViews(project string, in Input, varying []string, now int64) ([]*Result, error) {
	type group struct{ producer, section string }
	groups := map[group][]Record{}
	for _, r := range in.Records {
		g := group{r.Producer, r.Section}
		groups[g] = append(groups[g], r)
	}
	order := make([]group, 0, len(groups))
	for g := range groups {
		order = append(order, g)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].producer != order[j].producer {
			return order[i].producer < order[j].producer // the task's own ("") first
		}
		return order[i].section < order[j].section
	})
	out := []*Result{}
	for _, g := range order {
		records := groups[g]
		spec := Spec{Name: "results", Project: project, Section: g.section, Title: jobViewTitle(g.producer, g.section)}
		axes, metrics, params := map[string]bool{}, map[string]bool{}, map[string]bool{}
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
		}
		spec.Rows = keys(axes)
		for _, k := range varying {
			if params[k] {
				spec.Rows = append(spec.Rows, "param."+k)
			}
		}
		for _, m := range keys(metrics) {
			// Section-qualified: a metric may share its name with an axis.
			spec.Columns = append(spec.Columns, Column{Name: m, Value: "`" + g.section + "." + m + "`"})
		}
		r, err := Render(spec, Input{Records: records, Sources: in.Sources, Extras: in.Extras}, now)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
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
