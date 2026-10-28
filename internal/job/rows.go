package job

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// TaskRow is one `cells:` entry: a named cell. `cell` (formerly `run`) and
// `needs_code` are row metadata; every other key is a param overriding
// fixed_params. Both YAML and JSON keep the flat shape
// `{cell: R1, lr: 2e-6, needs_code: [std]}`.
type TaskRow struct {
	Run       string
	NeedsCode []string
	Params    TaskParams

	legacyRunKey bool // named with run: instead of cell:
}

// Row is one planned task: its run name, code-change note and full params.
// Command is export-only: the template the row's selected version ran.
type Row struct {
	Run       string     `json:"run"`
	NeedsCode []string   `json:"needs_code,omitempty"`
	Params    TaskParams `json:"params"`
	Command   string     `json:"-"`
	// Export only: the row's submission ran on another target, or with
	// other overrides, than the file's (shown as a comment).
	Target    string     `json:"-"`
	Overrides *Overrides `json:"-"`
}

// ReservedRowKeys never become params. cell names the row; run is its
// older spelling.
var ReservedRowKeys = []string{"cell", "run", "needs_code"}

func (r *TaskRow) fromMap(m map[string]any) error {
	r.Params = TaskParams{}
	if c, ok := m["cell"]; ok && c != nil {
		if run, ok := m["run"]; ok && run != nil && fmt.Sprint(run) != fmt.Sprint(c) {
			return fmt.Errorf("a row names its cell twice (cell: %v, run: %v); keep cell:", c, run)
		}
	}
	for k, v := range m {
		switch k {
		case "cell", "run":
			if v == nil {
				continue
			}
			r.Run = strings.TrimSpace(fmt.Sprint(v))
			if k == "run" {
				r.legacyRunKey = true
			}
		case "needs_code":
			switch vv := v.(type) {
			case nil:
			case string:
				r.NeedsCode = []string{vv}
			case []any:
				for _, x := range vv {
					r.NeedsCode = append(r.NeedsCode, fmt.Sprint(x))
				}
			default:
				return fmt.Errorf("needs_code must be a list of param names")
			}
		default:
			r.Params[k] = v
		}
	}
	return nil
}

func (r TaskRow) toMap() map[string]any {
	m := make(map[string]any, len(r.Params)+2)
	for k, v := range r.Params {
		m[k] = v
	}
	m["cell"] = r.Run
	if len(r.NeedsCode) > 0 {
		m["needs_code"] = r.NeedsCode
	}
	return m
}

func (r *TaskRow) UnmarshalYAML(value *yaml.Node) error {
	var m map[string]any
	if err := value.Decode(&m); err != nil {
		return fmt.Errorf("cells: each row is a mapping like {cell: R1, lr: 2e-6}: %w", err)
	}
	return r.fromMap(m)
}

func (r TaskRow) MarshalYAML() (any, error) { return r.toMap(), nil }

func (r *TaskRow) UnmarshalJSON(b []byte) error {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	return r.fromMap(m)
}

func (r TaskRow) MarshalJSON() ([]byte, error) { return json.Marshal(r.toMap()) }

// Rows plans a job's tasks in order with run names. cells: rows keep their
// names; sweep rows are named by what differs from the baseline row
// ("kl=0.1, seed=2"; the baseline itself is "base"). Validation errors say
// which row is wrong.
func Rows(cfg *JobConfig) ([]Row, error) {
	if len(cfg.Tasks) > 0 && len(cfg.Sweep) > 0 {
		return nil, fmt.Errorf("job has both sweep: and cells: — use one (sweep generates rows, cells lists them)")
	}
	var rows []Row
	if len(cfg.Tasks) > 0 {
		seen := map[string]bool{}
		for i, tr := range cfg.Tasks {
			if tr.Run == "" {
				return nil, fmt.Errorf("tasks[%d]: run is required (a short row name, e.g. {run: R1 lr, lr: 2e-6})", i)
			}
			if seen[tr.Run] {
				return nil, fmt.Errorf("tasks[%d]: run %q is used twice; run names are unique in a job", i, tr.Run)
			}
			seen[tr.Run] = true
			params := make(TaskParams, len(cfg.FixedParams)+len(tr.Params))
			for k, v := range cfg.FixedParams {
				params[k] = v
			}
			for k, v := range tr.Params {
				params[k] = v
			}
			for _, p := range tr.NeedsCode {
				if _, ok := params[p]; !ok {
					return nil, fmt.Errorf("row %q: needs_code names %q, which is not a param of this row", tr.Run, p)
				}
			}
			rows = append(rows, Row{Run: tr.Run, NeedsCode: tr.NeedsCode, Params: params})
		}
		if _, err := baselineIndex(rows, cfg.Baseline); err != nil {
			return nil, err
		}
		return rows, nil
	}

	expanded, err := Expand(cfg)
	if err != nil {
		return nil, err
	}
	for _, p := range expanded {
		rows = append(rows, Row{Params: p})
	}
	quiet := zipFollowers(cfg.Sweep)
	nameRowsQuiet(rows, 0, quiet)
	if cfg.Baseline != "" {
		b, err := baselineIndex(rows, cfg.Baseline)
		if err != nil {
			return nil, err
		}
		nameRowsQuiet(rows, b, quiet)
	}
	return rows, nil
}

// zipFollowers are the keys of list (zip) blocks other than the block's
// first key: they move together with it, so naming a row by the first key
// says it all ("condition_id=f2", not "condition_id=f2, query_language=…").
func zipFollowers(sweep []SweepBlock) map[string]bool {
	quiet := map[string]bool{}
	for _, b := range sweep {
		if b.Method != "list" || len(b.Parameters) < 2 {
			continue
		}
		var keys []string
		for k := range b.Parameters {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys[1:] {
			quiet[k] = true
		}
	}
	return quiet
}

// BaselineIndex is the row the plan compares against (default: first).
func BaselineIndex(rows []Row, baseline string) int {
	i, err := baselineIndex(rows, baseline)
	if err != nil {
		return 0
	}
	return i
}

func baselineIndex(rows []Row, baseline string) (int, error) {
	if baseline == "" || len(rows) == 0 {
		return 0, nil
	}
	var names []string
	for i, r := range rows {
		if r.Run == baseline {
			return i, nil
		}
		names = append(names, r.Run)
	}
	return 0, fmt.Errorf("baseline %q is not a run of this job (runs: %s)", baseline, strings.Join(names, ", "))
}

// nameRows names rows by their difference from rows[base]; duplicates get
// " #2", " #3" in order.
func nameRows(rows []Row, base int) { nameRowsQuiet(rows, base, nil) }

func nameRowsQuiet(rows []Row, base int, quiet map[string]bool) {
	count := map[string]int{}
	for i := range rows {
		name := diffName(rows[base].Params, rows[i].Params, quiet)
		count[name]++
		if count[name] > 1 {
			name = fmt.Sprintf("%s #%d", name, count[name])
		}
		rows[i].Run = name
	}
}

// DiffName lists the params of p that differ from base, sorted by key:
// "kl=0.1, seed=2". Identical params name the row "base". Values are
// shortened for a label: a path keeps its last segment, and anything past
// 32 characters is cut with "…".
func DiffName(base, p TaskParams) string { return diffName(base, p, nil) }

func diffName(base, p TaskParams, quiet map[string]bool) string {
	keys := map[string]bool{}
	for k := range base {
		keys[k] = true
	}
	for k := range p {
		keys[k] = true
	}
	var sorted []string
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	var parts []string
	for _, k := range sorted {
		if quiet[k] {
			continue
		}
		bv, inBase := base[k]
		v, inRow := p[k]
		switch {
		case inRow && (!inBase || !SameValue(bv, v)):
			parts = append(parts, k+"="+labelValue(v))
		case !inRow && inBase:
			parts = append(parts, "-"+k)
		}
	}
	if len(parts) == 0 {
		return "base"
	}
	return strings.Join(parts, ", ")
}

// VaryingKeys are the params whose values differ across rows, sorted.
func VaryingKeys(rows []Row) []string {
	if len(rows) == 0 {
		return nil
	}
	var out []string
	keys := map[string]bool{}
	for _, r := range rows {
		for k := range r.Params {
			keys[k] = true
		}
	}
	for k := range keys {
		first, inFirst := rows[0].Params[k]
		for _, r := range rows[1:] {
			v, ok := r.Params[k]
			if ok != inFirst || !SameValue(v, first) {
				out = append(out, k)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

func labelValue(v any) string {
	s := FormatValue(v)
	if i := strings.LastIndex(strings.TrimRight(s, "/"), "/"); i >= 0 && i < len(s)-1 {
		s = strings.TrimRight(s[i+1:], "/")
	}
	if r := []rune(s); len(r) > 32 {
		s = string(r[:31]) + "…"
	}
	return s
}

// LegacyRunNames names tasks submitted before rc6 (no stored run name):
// each task takes the name of the planned row with the same params when
// the stored config still plans one, else its difference from the first
// task. Returned in the order of params.
func LegacyRunNames(cfg *JobConfig, params []TaskParams) []string {
	planned := map[string]string{}
	if rows, err := Rows(cfg); err == nil {
		for _, r := range rows {
			planned[paramsKey(r.Params)] = r.Run
		}
	}
	out := make([]string, len(params))
	for i, p := range params {
		if name, ok := planned[paramsKey(p)]; ok {
			out[i] = name
		} else if len(params) > 0 {
			out[i] = DiffName(params[0], p)
		}
	}
	return out
}

func paramsKey(p TaskParams) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "\x00" + valueKey(p[k]) + "\x00")
	}
	return b.String()
}
