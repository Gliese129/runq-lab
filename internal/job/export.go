package job

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ExportInput is a submitted job, frozen: what `runq export` writes back as
// a job.yaml. Empty Target/Command mean "the project's default" and are
// left out; so is a Command equal to DefaultCommand (the project's).
type ExportInput struct {
	JobID          string
	DraftID        string // set: the file is a draft's editable form (draft/rev header)
	Rev            int
	Into           string   // a draft's append target
	Rerun          []string // a draft's succeeded cells to run again
	ExportedAt     time.Time
	DefaultCommand string
	Project        string
	Name           string
	Description    string
	Section        string // "" or "unsorted" = left out
	Target         string
	Command        string
	Overrides      *Overrides
	Baseline       string // run name; left out when it is the first row
	Rows           []Row
	ParamOrder     []string // project param order; others follow sorted
}

// ExportYAML renders the job as `cells:` rows (grid/list blocks are not
// reconstructed). fixed_params holds the baseline row's values (for every
// param all rows have); each row lists only what differs from it, one flow
// mapping per line — the plan reads as "what each run changes". Feeding
// the output back as a job.yaml reproduces the same rows, names, params and
// baseline; a row whose Command differs from the file's carries it as a
// comment only.
func ExportYAML(in ExportInput) ([]byte, error) {
	order := paramOrder(in.Rows, in.ParamOrder)
	base := TaskParams{}
	if len(in.Rows) > 0 {
		base = in.Rows[BaselineIndex(in.Rows, in.Baseline)].Params
	}
	fixed := map[string]bool{}
	for _, k := range order {
		if _, ok := base[k]; ok && inEveryRow(in.Rows, k) {
			fixed[k] = true
		}
	}

	doc := &yaml.Node{Kind: yaml.MappingNode}
	add := func(key string, val *yaml.Node) {
		doc.Content = append(doc.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, val)
	}
	str := func(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }

	if in.DraftID != "" {
		add("draft", str(in.DraftID))
		add("rev", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(in.Rev)})
		if in.Into != "" {
			add("into", str(in.Into))
		}
		if len(in.Rerun) > 0 {
			rr := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
			for _, name := range in.Rerun {
				rr.Content = append(rr.Content, str(name))
			}
			add("rerun", rr)
		}
	} else {
		add("from_job", str(in.JobID))
	}
	add("project", str(in.Project))
	if in.Name != "" {
		add("name", str(in.Name))
	}
	if in.Description != "" {
		add("description", str(in.Description))
	}
	if in.Section != "" && in.Section != "unsorted" {
		add("section", str(in.Section))
	}
	if in.Target != "" {
		add("target", str(in.Target))
	}
	if in.Command != "" && in.Command != in.DefaultCommand {
		add("command", str(in.Command))
	}
	if len(in.Rows) > 0 && in.Baseline != "" && in.Baseline != in.Rows[0].Run {
		add("baseline", str(in.Baseline))
	}
	if len(fixed) > 0 && len(in.Rows) > 0 {
		fm := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
		for _, k := range order {
			if fixed[k] {
				v, err := valueNode(base[k])
				if err != nil {
					return nil, err
				}
				fm.Content = append(fm.Content, str(k), v)
			}
		}
		add("fixed_params", fm)
	}
	for _, extra := range []struct {
		key string
		val any
	}{{"overrides", in.Overrides}} {
		if in.Overrides == nil {
			continue
		}
		n := &yaml.Node{}
		if err := n.Encode(extra.val); err != nil {
			return nil, fmt.Errorf("encode %s: %w", extra.key, err)
		}
		add(extra.key, n)
	}

	tasks := &yaml.Node{Kind: yaml.SequenceNode}
	for _, r := range in.Rows {
		m := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
		m.Content = append(m.Content, str("cell"), str(r.Run))
		for _, k := range order {
			v, ok := r.Params[k]
			if !ok || fixed[k] && SameValue(v, base[k]) {
				continue
			}
			vn, err := valueNode(v)
			if err != nil {
				return nil, err
			}
			m.Content = append(m.Content, str(k), vn)
		}
		if len(r.NeedsCode) > 0 {
			nc := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
			for _, p := range r.NeedsCode {
				nc.Content = append(nc.Content, str(p))
			}
			m.Content = append(m.Content, str("needs_code"), nc)
		}
		if r.Command != "" && !SameCommand(r.Command, in.Command) {
			m.LineComment = "command: " + r.Command
		}
		tasks.Content = append(tasks.Content, m)
	}
	add("cells", tasks)

	var buf bytes.Buffer
	if in.DraftID != "" {
		fmt.Fprintf(&buf, "# draft %s rev %d — save your edits with: runq draft edit %s -f <this file>\n", in.DraftID, in.Rev, in.DraftID)
	} else {
		fmt.Fprintf(&buf, "# exported from %s at %s — a snapshot; editing it does not change the job\n",
			in.JobID, in.ExportedAt.UTC().Format(time.RFC3339))
	}
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{doc}}); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func inEveryRow(rows []Row, k string) bool {
	for _, r := range rows {
		if _, ok := r.Params[k]; !ok {
			return false
		}
	}
	return true
}

func paramOrder(rows []Row, declared []string) []string {
	present := map[string]bool{}
	for _, r := range rows {
		for k := range r.Params {
			present[k] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, k := range declared {
		if present[k] && !seen[k] {
			out = append(out, k)
			seen[k] = true
		}
	}
	var rest []string
	for k := range present {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// valueNode writes numbers in full decimal (0.000001, not 1e-06) so the
// file reads like the one the researcher wrote; strings keep their type.
func valueNode(v any) (*yaml.Node, error) {
	switch x := v.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprint(x)}, nil
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: x}, nil
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: FormatValue(x)}, nil
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: FormatValue(x)}, nil
	case float32:
		return valueNode(float64(x))
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(x)}, nil
	}
	n := &yaml.Node{}
	if err := n.Encode(v); err != nil {
		return nil, err
	}
	n.Style = yaml.FlowStyle
	return n, nil
}

// NameRows fills empty run names (jobs submitted before rc6) from their
// difference to the baseline row (the first).
func NameRows(rows []Row) {
	for _, r := range rows {
		if r.Run == "" {
			nameRows(rows, 0)
			return
		}
	}
}

// PlanTable renders rows as a table of run × the params that vary, with
// cells equal to the baseline left blank, and a "fixed:" line for the rest.
func PlanTable(rows []Row, baseline string, declared []string) string {
	if len(rows) == 0 {
		return "(no rows)\n"
	}
	b := BaselineIndex(rows, baseline)
	base := rows[b].Params
	order := paramOrder(rows, declared)
	var cols, fixedParts []string
	for _, k := range order {
		if inEveryRow(rows, k) && !varies(rows, k) {
			fixedParts = append(fixedParts, k+"="+FormatValue(base[k]))
		} else {
			cols = append(cols, k)
		}
	}
	header := append([]string{"CELL"}, cols...)
	header = append(header, "NOTE")
	table := [][]string{header}
	for i, r := range rows {
		run := r.Run
		if i == b {
			run += " (baseline)"
		}
		line := []string{run}
		for _, k := range cols {
			v, ok := r.Params[k]
			switch {
			case !ok:
				line = append(line, "∅")
			case i != b && SameValue(v, base[k]):
				line = append(line, "")
			default:
				line = append(line, FormatValue(v))
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
	if len(fixedParts) > 0 {
		out.WriteString("fixed: " + strings.Join(fixedParts, ", ") + "\n")
	}
	return out.String()
}

func varies(rows []Row, k string) bool {
	first := rows[0].Params[k]
	for _, r := range rows[1:] {
		if !SameValue(r.Params[k], first) {
			return true
		}
	}
	return false
}
