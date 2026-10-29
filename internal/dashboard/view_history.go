package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
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
	// Value/Mean/Std: a gap (null value, NaN in memory) stays a gap — a
	// mean over a step where any line has a gap is a gap too.
	type line struct {
		TaskID    string                `json:"task_id"`
		Step      []float64             `json:"step"`
		Value     workspace.CurveValues `json:"value"`
		signature string
	}
	type series struct {
		Key     map[string]any        `json:"key"`
		Metric  string                `json:"metric"`
		Tasks   []string              `json:"tasks"`
		Aligned bool                  `json:"aligned"`
		Step    []float64             `json:"step,omitempty"`
		Mean    workspace.CurveValues `json:"mean,omitempty"`
		Std     workspace.CurveValues `json:"std,omitempty"`
		N       []int                 `json:"n,omitempty"`
		PerTask []line                `json:"per_task,omitempty"`
		Warning string                `json:"warning,omitempty"`
		lines   []line
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
		history, err := h.SourceHistory(ctx, t.ID, "metrics", "step", points)
		if err != nil {
			return nil, err
		}
		for _, raw := range history.Series {
			selected := false
			for _, m := range metrics {
				if m == raw.Key {
					selected = true
				}
			}
			if !selected {
				continue
			}
			identity, _ := json.Marshal([]any{raw.Key, key})
			id := string(identity)
			g := groups[id]
			if g == nil {
				g = &series{Key: key, Metric: raw.Key, Aligned: true}
				groups[id] = g
				order = append(order, id)
			}
			l := line{TaskID: t.ID, Step: raw.Step, Value: raw.Value, signature: raw.Signature}
			if maxStep, ok := chart["max_step"].(float64); ok {
				l.Step = nil
				l.Value = nil
				for i, step := range raw.Step {
					if step <= maxStep {
						l.Step = append(l.Step, step)
						l.Value = append(l.Value, raw.Value[i])
					}
				}
			}
			g.lines = append(g.lines, l)
			g.Tasks = append(g.Tasks, t.ID)
		}
	}
	out := []*series{}
	for _, id := range order {
		g := groups[id]
		first := g.lines[0]
		for _, l := range g.lines[1:] {
			if l.signature != first.signature || len(l.Step) != len(first.Step) {
				g.Aligned = false
			}
		}
		_, aggregate := chart["aggregate"]
		if !aggregate {
			g.PerTask = g.lines
			out = append(out, g)
			continue
		}
		if g.Aligned {
			g.Step = first.Step
			for i := range first.Step {
				mean := 0.
				for _, l := range g.lines {
					mean += l.Value[i]
				}
				n := len(g.lines)
				mean /= float64(n)
				std := 0.
				for _, l := range g.lines {
					std += (l.Value[i] - mean) * (l.Value[i] - mean)
				}
				if n > 1 {
					std = math.Sqrt(std / float64(n-1))
				}
				g.Mean = append(g.Mean, mean)
				g.Std = append(g.Std, std)
				g.N = append(g.N, n)
			}
		} else {
			g.PerTask = g.lines
			g.Warning = "steps differ; showing exact recorded points without interpolation"
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
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, src := range sources {
		history, err := h.SourceHistory(r.Context(), src.ID, "results", by, 2)
		if err != nil {
			viewError(w, err)
			return
		}
		for _, frame := range history.Frames {
			lo = math.Min(lo, frame.At)
			hi = math.Max(hi, frame.At)
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
