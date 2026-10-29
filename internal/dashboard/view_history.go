package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/views"
	"github.com/gliese129/runq-lab/internal/workspace"
)

type sourceHistorian interface {
	SourceHistory(context.Context, string, string, string, int) (*workspace.ExactHistory, error)
	SourceFramesAt(context.Context, string, string, []float64) (*workspace.ExactHistory, error)
}

// viewSeries draws the curves of a view scope: the versions the table
// reads (same experiments, selected versions, filter), each one's metrics.
func (s *Server) viewSeries(ctx context.Context, scope *store.ViewScope, metrics []string, points int, chart map[string]any) (any, error) {
	if points == 0 {
		points = 400
	}
	if points < 2 || points > 2000 {
		return nil, &views.Invalid{Errors: []string{"points must be 2..2000"}}
	}
	h, ok := s.backend.(sourceHistorian)
	if !ok {
		return nil, fmt.Errorf("history queries unavailable")
	}
	// One series per (metric, color/facet key): each version's line as
	// logged (curveLine, the curves tab's type, estimate band included),
	// or with aggregate the mean over its versions (meanLine).
	type series struct {
		Key    map[string]any `json:"key"`
		Metric string         `json:"metric"`
		Tasks  []string       `json:"tasks"`
		// Lines: each version's curve; with aggregate only when the
		// versions share too few steps to average (Warning says so).
		Lines []curveLine `json:"lines,omitempty"`
		// Mean: the mean at the steps every version logged, lo/hi = ±std
		// where N ≥ 2; N is the versions averaged per step.
		Mean    *curveLine `json:"mean,omitempty"`
		N       []int      `json:"n,omitempty"`
		Warning string     `json:"warning,omitempty"` // no_common_steps | common_steps_only
		lines   []curveLine
	}
	_, aggregate := chart["aggregate"]
	fetch := points
	if aggregate {
		fetch = 2000 // average the full curves, thin the mean afterwards
	}
	groups := map[string]*series{}
	order := []string{}
	for _, t := range scope.Shown() {
		params := t.Params
		key := map[string]any{}
		for _, slot := range []string{"color", "facet"} {
			if name, ok := chart[slot].(string); ok {
				if len(name) < 7 || name[:6] != "param." {
					return nil, &views.Invalid{Errors: []string{"curves color/facet must reference param.<name>"}}
				}
				key[name] = params[name[6:]]
			}
		}
		history, err := h.SourceHistory(ctx, t.ID, "metrics", "step", fetch)
		if err != nil {
			return nil, err
		}
		cell := t.RunName
		if cell == "" {
			cell = t.ID
		}
		for _, raw := range history.Series {
			if !slices.Contains(metrics, raw.Key) {
				continue
			}
			identity, _ := json.Marshal([]any{raw.Key, key})
			id := string(identity)
			g := groups[id]
			if g == nil {
				g = &series{Key: key, Metric: raw.Key}
				groups[id] = g
				order = append(order, id)
			}
			l := lineOf(t.ID, cell, raw)
			if maxStep, ok := chart["max_step"].(float64); ok {
				l = l.upTo(maxStep)
			}
			g.lines = append(g.lines, l)
			g.Tasks = append(g.Tasks, t.ID)
		}
	}
	out := []*series{}
	for _, id := range order {
		g := groups[id]
		if !aggregate {
			g.Lines = g.lines
			out = append(out, g)
			continue
		}
		mean, n, partial, ok := meanLine(g.lines, points)
		switch {
		case !ok:
			g.Lines = g.lines
			g.Warning = "no_common_steps"
		case partial:
			g.Mean, g.N = &mean, n
			g.Warning = "common_steps_only"
		default:
			g.Mean, g.N = &mean, n
		}
		out = append(out, g)
	}
	metric := ""
	if len(metrics) == 1 {
		metric = metrics[0]
	}
	return map[string]any{"metric": metric, "series": out}, nil
}
func (s *Server) handleViewSeries(w http.ResponseWriter, r *http.Request) {
	st := s.viewStore(w)
	if st == nil {
		return
	}
	v, err := st.GetView(r.Context(), r.PathValue("p"), r.PathValue("name"))
	if err != nil {
		viewError(w, err)
		return
	}
	if v == nil {
		writeErr(w, 404, "not_found", "view not found")
		return
	}
	i, err := strconv.Atoi(r.PathValue("i"))
	if err != nil || i < 0 || i >= len(v.Spec.Charts) || v.Spec.Charts[i]["template"] != "curves" {
		writeErr(w, 400, "invalid_view", "chart must reference a curves template")
		return
	}
	chart := v.Spec.Charts[i]
	metric, _ := chart["metric"].(string)
	points := 400
	if r.URL.Query().Has("points") {
		points, _ = strconv.Atoi(r.URL.Query().Get("points"))
	}
	scope, err := st.ViewScope(r.Context(), v.Spec, r.URL.Query()["job"])
	if err != nil {
		viewError(w, err)
		return
	}
	out, err := s.viewSeries(r.Context(), scope, []string{metric}, points, chart)
	if err != nil {
		viewError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleViewFrames(w http.ResponseWriter, r *http.Request) {
	st := s.viewStore(w)
	if st == nil {
		return
	}
	v, err := st.GetView(r.Context(), r.PathValue("p"), r.PathValue("name"))
	if err != nil {
		viewError(w, err)
		return
	}
	if v == nil {
		writeErr(w, 404, "not_found", "view not found")
		return
	}
	q := r.URL.Query()
	chart, err := strconv.Atoi(q.Get("chart"))
	if q.Get("chart") == "" {
		chart = 0
		err = nil
	}
	count := 60
	if q.Has("count") {
		count, err = strconv.Atoi(q.Get("count"))
	}
	by := q.Get("by")
	if by == "" {
		by = "step"
	}
	if err != nil || chart < 0 || chart >= len(v.Spec.Charts) || v.Spec.Charts[chart]["template"] == "curves" || count < 2 || count > 300 || (by != "step" && by != "time") {
		writeErr(w, 400, "invalid_view", "frames require non-curves chart, count 2..300, by step|time")
		return
	}
	h, ok := s.backend.(sourceHistorian)
	if !ok {
		writeErr(w, 501, "unsupported", "history unavailable")
		return
	}
	// The frames read the table's scope, ?job= included: same experiments,
	// selected versions, filter and producer.
	scope, err := st.ViewScope(r.Context(), v.Spec, q["job"])
	if err != nil {
		viewError(w, err)
		return
	}
	sources, err := st.ViewSources(r.Context(), scope)
	if err != nil {
		viewError(w, err)
		return
	}
	// The span comes from the sections the view reads (all when it names
	// none), so another section's longer run does not stretch the frames.
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, src := range sources {
		history, err := h.SourceHistory(r.Context(), src.ID, "results", by, 2)
		if err != nil {
			viewError(w, err)
			return
		}
		for section, span := range history.Ranges {
			if v.Spec.Section != "" && section != v.Spec.Section {
				continue
			}
			lo = math.Min(lo, span[0])
			hi = math.Max(hi, span[1])
		}
	}
	type frame struct {
		At   float64     `json:"at"`
		Rows []views.Row `json:"rows"`
	}
	frames := []frame{}
	if math.IsInf(lo, 1) {
		writeJSON(w, 200, map[string]any{"by": by, "frames": frames})
		return
	}
	ats := []float64{}
	for i := 0; i < count; i++ {
		at := lo + float64(i)*(hi-lo)/float64(count-1)
		if i == 0 || at != ats[len(ats)-1] {
			ats = append(ats, at)
		}
	}
	inputs := make([]views.Input, len(ats))
	for _, src := range sources {
		history, err := h.SourceFramesAt(r.Context(), src.ID, by, ats)
		if err != nil {
			viewError(w, err)
			return
		}
		for i, f := range history.Frames {
			if i >= len(inputs) {
				break
			}
			for _, rec := range f.Records {
				inputs[i].Records = append(inputs[i].Records, src.Record(rec.Section, rec.TS, rec.Axes, rec.Metrics))
			}
		}
	}
	for i, input := range inputs {
		result, err := views.Render(v.Spec, input, time.Now().Unix())
		if err != nil {
			viewError(w, err)
			return
		}
		frames = append(frames, frame{At: ats[i], Rows: result.Rows})
	}
	writeJSON(w, 200, map[string]any{"by": by, "frames": frames})
}
