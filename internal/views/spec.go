package views

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Spec struct {
	Name      string           `json:"view" yaml:"view"`
	Project   string           `json:"project" yaml:"project"`
	Title     string           `json:"title,omitempty" yaml:"title,omitempty"`
	Jobs      any              `json:"jobs,omitempty" yaml:"jobs,omitempty"`
	Section   string           `json:"section,omitempty" yaml:"section,omitempty"`
	Group     []string         `json:"group,omitempty" yaml:"group,omitempty"`
	Rows      []string         `json:"rows,omitempty" yaml:"rows,omitempty"`
	Expect    map[string][]any `json:"expect,omitempty" yaml:"expect,omitempty"`
	Filter    map[string][]any `json:"filter,omitempty" yaml:"filter,omitempty"`
	Columns   []Column         `json:"columns,omitempty" yaml:"columns,omitempty"`
	State     []State          `json:"state,omitempty" yaml:"state,omitempty"`
	Extra     []Extra          `json:"extra,omitempty" yaml:"extra,omitempty"`
	Legend    string           `json:"legend,omitempty" yaml:"legend,omitempty"`
	Charts    []map[string]any `json:"charts,omitempty" yaml:"charts,omitempty"`
	Aggregate *Aggregate       `json:"aggregate,omitempty" yaml:"aggregate,omitempty"`
	Pivot     *Pivot           `json:"pivot,omitempty" yaml:"pivot,omitempty"`
	// MetricRows turns the table so each metric is a row group (key
	// "metric") and the columns are the pivot values, or one "value"
	// column without a pivot.
	MetricRows bool              `json:"metric_rows,omitempty" yaml:"metric_rows,omitempty"`
	Highlight  map[string]string `json:"highlight,omitempty" yaml:"highlight,omitempty"`
	// KeyFormats formats row-key values (e.g. {"param.lr": "sci0"}); keys
	// without one print in full.
	KeyFormats map[string]string     `json:"key_formats,omitempty" yaml:"key_formats,omitempty"`
	Baseline   map[string]any        `json:"baseline,omitempty" yaml:"baseline,omitempty"`
	Sources    map[string]DataSource `json:"sources,omitempty" yaml:"sources,omitempty"`
	Custom     map[string]string     `json:"custom,omitempty" yaml:"custom,omitempty"`
}
type Column struct {
	Name   string `json:"name" yaml:"name"`
	Value  string `json:"value,omitempty" yaml:"value,omitempty"`
	Format string `json:"format,omitempty" yaml:"format,omitempty"`
	When   string `json:"when,omitempty" yaml:"when,omitempty"`
	Better string `json:"better,omitempty" yaml:"better,omitempty"`
	Group  any    `json:"group,omitempty" yaml:"group,omitempty"`
	// At is the pivot value of a metric_rows column (named "<axis>=<at>").
	At any `json:"at,omitempty" yaml:"-"`
}
type State struct {
	Label string `json:"label" yaml:"label"`
	When  string `json:"when,omitempty" yaml:"when,omitempty"`
}
type Extra struct {
	Name   string `json:"name" yaml:"name"`
	Job    string `json:"job" yaml:"job"`
	Value  string `json:"value" yaml:"value"`
	Format string `json:"format,omitempty" yaml:"format,omitempty"`
}
type Aggregate struct {
	Over []string `json:"over" yaml:"over"`
	Stat string   `json:"stat" yaml:"stat"`
}
type Pivot struct {
	Axis  string `json:"axis" yaml:"axis"`
	Order []any  `json:"order,omitempty" yaml:"order,omitempty"`
}
type DataSource struct {
	From    string   `json:"from" yaml:"from"`
	Section string   `json:"section,omitempty" yaml:"section,omitempty"`
	Metric  []string `json:"metric,omitempty" yaml:"metric,omitempty"`
	Points  int      `json:"points,omitempty" yaml:"points,omitempty"`
	Run     string   `json:"run,omitempty" yaml:"run,omitempty"`
	Every   string   `json:"every,omitempty" yaml:"every,omitempty"`
	Timeout string   `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}
type Invalid struct{ Errors []string }

func (e *Invalid) Error() string { return "invalid_view: " + strings.Join(e.Errors, "; ") }
func canonical(v any) string     { b, _ := json.Marshal(v); return string(b) }
func keys[V any](m map[string]V) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func contains(s []string, v string) bool {
	for _, x := range s {
		if v == x {
			return true
		}
	}
	return false
}
func (s Spec) Validate() error {
	errs := []string{}
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	if s.Name == "" || s.Project == "" {
		add("view and project are required")
	}
	if s.Section != "" && len(s.Group) > 0 {
		add("use section or group, not both")
	}
	seen := map[string]bool{}
	for _, r := range s.Rows {
		if seen[r] {
			add("duplicate row %q", r)
		}
		seen[r] = true
	}
	if len(s.Expect) > 0 {
		if len(s.Rows) > 0 && canonical(keys(s.Expect)) != canonical(sorted(s.Rows)) {
			add("expect must cover rows exactly: rows=%v expect=%v", s.Rows, keys(s.Expect))
		}
		count := 1
		for k, vals := range s.Expect {
			if len(vals) == 0 {
				add("expect.%s must be nonempty", k)
			}
			dups := map[string]bool{}
			for _, v := range vals {
				key := canonical(v)
				if dups[key] {
					add("expect.%s has duplicate %s", k, key)
				}
				dups[key] = true
			}
			if len(vals) > 100000 || count > 100000/max(1, len(vals)) {
				add("expect exceeds 100000 cells")
			} else {
				count *= len(vals)
			}
		}
		for k := range s.Filter {
			if _, ok := s.Expect[k]; !ok {
				add("filter %q must be an expect dimension", k)
			}
		}
	}
	check := func(label, expr string) {
		if expr == "" {
			return
		}
		var err error
		if strings.Contains(expr, "{") {
			_, err = templateFields(expr)
		} else {
			_, err = Parse(expr)
		}
		if err != nil {
			add("%s: %v", label, err)
		}
	}
	names := map[string]bool{}
	for _, c := range s.Columns {
		if c.Name == "" || names[c.Name] {
			add("column name missing or duplicate: %q", c.Name)
		}
		names[c.Name] = true
		if c.Value == "" {
			add("column %q needs value", c.Name)
		}
		check("column "+c.Name, c.Value)
		check("when "+c.Name, c.When)
		if !validFormat(c.Format) {
			add("column %q: invalid format %q", c.Name, c.Format)
		}
		if c.Better != "" && c.Better != "higher" && c.Better != "lower" {
			add("column %q: better must be higher or lower", c.Name)
		}
	}
	for i, r := range s.State {
		check("state "+r.Label, r.When)
		if r.When == "" && i != len(s.State)-1 {
			add("unconditional state must be last")
		}
	}
	for _, e := range s.Extra {
		if e.Job == "" {
			add("extra %q needs job", e.Name)
		}
		check("extra "+e.Name, e.Value)
		if !validFormat(e.Format) {
			add("extra %q: invalid format", e.Name)
		}
	}
	if a := s.Aggregate; a != nil {
		if len(a.Over) == 0 {
			add("aggregate.over is required")
		}
		if !contains([]string{"mean_std", "mean_sem", "mean_ci95", "mean", "median"}, a.Stat) {
			add("unknown aggregate stat %q", a.Stat)
		}
	}
	for k, f := range s.KeyFormats {
		if f == "" || !validFormat(f) {
			add("key_formats.%s: unknown format %q", k, f)
		}
	}
	if s.Pivot != nil && s.Pivot.Axis == "" {
		add("pivot.axis is required")
	}
	required := map[string][]string{"compare": {"x", "y", "color"}, "sweep": {"x", "y"}, "heatmap": {"x", "y", "value"}, "curves": {"metric"}, "progress": {"done", "total"}, "tradeoff": {"x", "y", "color"}}
	for i, c := range s.Charts {
		tpl, _ := c["template"].(string)
		req, ok := required[tpl]
		if !ok {
			add("chart %d: unknown template %q", i, tpl)
		}
		for _, k := range req {
			if c[k] == nil || c[k] == "" {
				add("chart %d: missing %s", i, k)
			}
		}
	}
	for name, d := range s.Sources {
		switch d.From {
		case "record":
		case "report":
			if len(d.Metric) == 0 {
				add("source %s: metric required", name)
			}
		case "script":
			if d.Run == "" {
				add("source %s: run required", name)
			}
			for _, x := range []string{d.Every, d.Timeout} {
				if x != "" {
					if v, e := time.ParseDuration(x); e != nil || v <= 0 {
						add("source %s: invalid duration %q", name, x)
					}
				}
			}
		default:
			add("source %s: unknown from %q", name, d.From)
		}
	}
	if len(errs) > 0 {
		return &Invalid{errs}
	}
	return nil
}
func sorted(s []string) []string { out := append([]string{}, s...); sort.Strings(out); return out }
func validFormat(s string) bool {
	return contains([]string{"", "int", "float1", "float2", "float3", "float4", "pct0", "pct1", "pct2", "sci0", "sci1", "sci2", "sci3", "duration", "ago"}, s)
}
