package views

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"
)

func latexEscape(s string) string {
	return strings.NewReplacer("\\", "\\textbackslash{}", "{", "\\{", "}", "\\}", "_", "\\_", "%", "\\%", "&", "\\&", "#", "\\#", "$", "\\$", "^", "\\textasciicircum{}", "~", "\\textasciitilde{}").Replace(s)
}
func mdEscape(s string) string { return strings.NewReplacer("|", "\\|", "\n", "<br>").Replace(s) }
func Export(r *Result, format string, wide, color bool) (string, error) {
	var b strings.Builder
	if format == "json" {
		raw, err := json.MarshalIndent(r, "", "  ")
		return string(raw) + "\n", err
	}
	if format == "csv" {
		w := csv.NewWriter(&b)
		header := append([]string{}, r.RowKeys...)
		for _, c := range r.Columns {
			label := c.Name
			if c.Group != nil {
				label = fmt.Sprint(c.Group) + "/" + label
			}
			header = append(header, label, label+".mean", label+".std", label+".n")
		}
		_ = w.Write(header)
		for _, row := range r.Rows {
			v := []string{}
			for _, k := range r.RowKeys {
				v = append(v, plain(row.Key[k]))
			}
			for _, c := range row.Cells {
				v = append(v, plain(c.Value), ptrFloat(c.Mean), ptrFloat(c.Std), ptrInt(c.N))
			}
			_ = w.Write(v)
		}
		w.Flush()
		return b.String(), w.Error()
	}
	if format == "latex" {
		if color {
			b.WriteString("% Requires booktabs, multirow, xcolor\n")
		} else {
			b.WriteString("% Requires booktabs, multirow\n")
		}
		for _, line := range strings.Split(r.Legend, "\n") {
			if line != "" {
				fmt.Fprintf(&b, "%% %s\n", line)
			}
		}
		fmt.Fprintf(&b, "\\begin{tabular}{%s%s}\n\\toprule\n", strings.Repeat("l", len(r.RowKeys)), strings.Repeat("r", len(r.Columns)))
		if len(r.Columns) > 0 && r.Columns[0].Group != nil {
			groups := []string{}
			for range r.RowKeys {
				groups = append(groups, "")
			}
			rules := []string{}
			for i := 0; i < len(r.Columns); {
				j := i + 1
				for j < len(r.Columns) && canonical(r.Columns[j].Group) == canonical(r.Columns[i].Group) {
					j++
				}
				groups = append(groups, fmt.Sprintf("\\multicolumn{%d}{c}{%s}", j-i, latexEscape(fmt.Sprint(r.Columns[i].Group))))
				rules = append(rules, fmt.Sprintf("\\cmidrule(lr){%d-%d}", len(r.RowKeys)+i+1, len(r.RowKeys)+j))
				i = j
			}
			b.WriteString(strings.Join(groups, " & ") + " \\\\\n" + strings.Join(rules, "") + "\n")
		}
		header := []string{}
		for _, k := range r.RowKeys {
			header = append(header, latexEscape(k))
		}
		for _, c := range r.Columns {
			label := latexEscape(c.Name)
			if c.Better == "higher" {
				label += " $\\uparrow$"
			} else if c.Better == "lower" {
				label += " $\\downarrow$"
			}
			header = append(header, label)
		}
		b.WriteString(strings.Join(header, " & ") + " \\\\\n\\midrule\n")
		for ri, row := range r.Rows {
			vals := []string{}
			for ki, k := range r.RowKeys {
				text := latexEscape(plain(row.Key[k]))
				if ki < len(r.RowKeys)-1 {
					samePrefix := func(other Row) bool {
						for _, pk := range r.RowKeys[:ki+1] {
							if canonical(other.Key[pk]) != canonical(row.Key[pk]) {
								return false
							}
						}
						return true
					}
					if ri > 0 && samePrefix(r.Rows[ri-1]) {
						text = ""
					} else {
						n := 1
						for ri+n < len(r.Rows) && samePrefix(r.Rows[ri+n]) {
							n++
						}
						if n > 1 {
							text = fmt.Sprintf("\\multirow{%d}{*}{%s}", n, text)
						}
					}
				}
				vals = append(vals, text)
			}
			for ci, c := range row.Cells {
				text := strings.ReplaceAll(latexEscape(c.Text), "±", "$\\pm$")
				if c.Rank == 1 && r.Highlight["best"] == "bold" {
					text = "\\textbf{" + text + "}"
				}
				if c.Rank == 2 && r.Highlight["second"] == "underline" {
					text = "\\underline{" + text + "}"
				}
				if c.DeltaText != "" {
					delta := latexEscape(c.DeltaText)
					if color && c.Delta != nil && r.Columns[ci].Better != "" {
						good := *c.Delta > 0
						if r.Columns[ci].Better == "lower" {
							good = *c.Delta < 0
						}
						shade := "red"
						if good {
							shade = "green!50!black"
						}
						delta = "\\textcolor{" + shade + "}{" + delta + "}"
					}
					text += " (" + delta + ")"
				}
				vals = append(vals, text)
			}
			b.WriteString(strings.Join(vals, " & ") + " \\\\\n")
		}
		b.WriteString("\\bottomrule\n\\end{tabular}\n")
		return b.String(), nil
	}
	if format == "markdown" {
		header := append([]string{}, r.RowKeys...)
		for _, c := range r.Columns {
			label := c.Name
			if c.Group != nil {
				label = fmt.Sprint(c.Group) + " / " + label
			}
			header = append(header, label)
		}
		b.WriteString("| " + strings.Join(header, " | ") + " |\n| " + strings.TrimSuffix(strings.Repeat("--- | ", len(header)), " ") + "\n")
		for _, row := range r.Rows {
			v := []string{}
			for _, k := range r.RowKeys {
				v = append(v, mdEscape(plain(row.Key[k])))
			}
			for _, c := range row.Cells {
				text := mdEscape(c.Text)
				if c.Rank == 1 && r.Highlight["best"] == "bold" {
					text = "**" + text + "**"
				}
				if c.Rank == 2 && r.Highlight["second"] == "underline" {
					text = "<u>" + text + "</u>"
				}
				if c.DeltaText != "" {
					text += " (" + c.DeltaText + ")"
				}
				v = append(v, text)
			}
			b.WriteString("| " + strings.Join(v, " | ") + " |\n")
		}
		return b.String(), nil
	}
	if format != "" && format != "text" {
		return "", fmt.Errorf("unknown format %q", format)
	}
	fmt.Fprintf(&b, "%s · %s\n", r.Title, time.Unix(r.GeneratedAt, 0).Format("2006-01-02 15:04:05"))
	for _, s := range r.Sources {
		data := "no data yet"
		if s.LastDataAt != nil {
			data, _ = Format(float64(*s.LastDataAt), "ago", r.GeneratedAt)
		}
		observed := s.ObservationState
		if s.ObservedAt != nil {
			ago, _ := Format(float64(*s.ObservedAt), "ago", r.GeneratedAt)
			observed += " (" + ago + ")"
		}
		heartbeat := "--"
		if s.LastHeartbeatAt != nil {
			heartbeat, _ = Format(float64(*s.LastHeartbeatAt), "ago", r.GeneratedAt)
		}
		fmt.Fprintf(&b, "%s %s [%s] data: %s · observation: %s · heartbeat: %s\n", s.JobID, s.Note, s.Status, data, observed, heartbeat)
		for _, a := range s.Attention {
			fmt.Fprintf(&b, "  ! %s\n", attentionLine(a))
		}
		for _, w := range s.Warnings {
			fmt.Fprintf(&b, "  ! %s\n", w)
		}
	}
	for _, e := range r.Extra {
		name := e.Name
		if e.Agg != "" {
			name += "[" + e.Agg + "]"
		}
		fmt.Fprintf(&b, "%s = %s  ", name, e.Text)
	}
	if len(r.Extra) > 0 {
		b.WriteString("\n")
	}
	w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	h := append([]string{}, r.RowKeys...)
	for _, c := range r.Columns {
		label := c.Name
		if c.Group != nil {
			label = fmt.Sprint(c.Group) + "/" + label
		}
		h = append(h, label)
	}
	h = append(h, "state")
	if wide {
		h = append(h, "sources", "updated_at")
	}
	fmt.Fprintln(w, strings.Join(h, "\t"))
	for _, row := range r.Rows {
		v := []string{}
		for _, k := range r.RowKeys {
			v = append(v, plain(row.Key[k]))
		}
		for _, c := range row.Cells {
			v = append(v, c.Text)
		}
		state := row.State
		if row.Empty {
			state = "no data yet"
		}
		v = append(v, state)
		if wide {
			ids := []string{}
			for _, c := range row.Contributors {
				ids = append(ids, c.SourceID)
			}
			v = append(v, strings.Join(ids, ","), ptrTime(row.UpdatedAt))
		}
		fmt.Fprintln(w, strings.Join(v, "\t"))
	}
	w.Flush()
	if len(r.Rows) == 0 {
		b.WriteString("no data yet\n")
	}
	if r.Legend != "" {
		b.WriteString(r.Legend + "\n")
	}
	return b.String(), nil
}

// attentionLine renders one attention entry as "<id> <status>: <reason>" —
// the id first, so the next command (logs, show) is a copy away.
func attentionLine(a map[string]any) string {
	var id string
	for _, k := range []string{"task_id", "consumer_id", "job_id"} {
		if v, ok := a[k].(string); ok && v != "" {
			id = v
			break
		}
	}
	status, _ := a["status"].(string)
	reason, _ := a["reason"].(string)
	line := strings.TrimSpace(id + " " + status)
	if reason != "" {
		line += ": " + reason
	}
	if host, _ := a["host"].(string); host != "" {
		line += fmt.Sprintf(" (host %s, pid %v)", host, a["pid"])
	}
	return line
}

func plain(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func ptrFloat(p *float64) string {
	if p == nil {
		return ""
	}
	return fmt.Sprint(*p)
}
func ptrInt(p *int) string {
	if p == nil {
		return ""
	}
	return fmt.Sprint(*p)
}
func ptrTime(p *int64) string {
	if p == nil {
		return ""
	}
	return fmt.Sprint(*p)
}
