package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/views"
)

func (s *Server) viewStore(w http.ResponseWriter) *store.Store {
	b, ok := s.backend.(interface{ ViewStore() *store.Store })
	if !ok {
		writeErr(w, 501, "unsupported", "views require a store-backed controller")
		return nil
	}
	return b.ViewStore()
}
func viewError(w http.ResponseWriter, err error) {
	var invalid *views.Invalid
	if errors.As(err, &invalid) {
		writeErr(w, 400, "invalid_view", err.Error())
		return
	}
	writeError(w, err)
}
func (s *Server) registerViews() {
	s.mux.HandleFunc("GET /api/v1/projects/{p}/views/{name}/charts/{i}/series", s.handleViewSeries)
	s.mux.HandleFunc("GET /api/v1/projects/{p}/views/{name}/frames", s.handleViewFrames)
	s.mux.HandleFunc("GET /api/v1/views", s.handleListViews)
	s.mux.HandleFunc("GET /api/v1/projects/{p}/views", s.handleListViews)
	s.mux.HandleFunc("GET /api/v1/projects/{p}/views/{name}", s.handleGetView)
	s.mux.HandleFunc("PUT /api/v1/projects/{p}/views/{name}", s.handlePutView)
	s.mux.HandleFunc("DELETE /api/v1/projects/{p}/views/{name}", s.handleDeleteView)
	s.mux.HandleFunc("GET /api/v1/projects/{p}/views/{name}/render", s.handleRenderView)
	s.mux.HandleFunc("GET /api/v1/jobs/{id}/view", s.handleJobView)
}
func (s *Server) handleListViews(w http.ResponseWriter, r *http.Request) {
	st := s.viewStore(w)
	if st == nil {
		return
	}
	list, err := st.ListViews(r.Context(), r.PathValue("p"))
	if err != nil {
		viewError(w, err)
		return
	}
	writeJSON(w, 200, list)
}
func (s *Server) handleGetView(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, 200, v)
}
func (s *Server) handlePutView(w http.ResponseWriter, r *http.Request) {
	st := s.viewStore(w)
	if st == nil {
		return
	}
	var v store.SavedView
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		writeErr(w, 400, "invalid_view", err.Error())
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		writeErr(w, 400, "invalid_view", "expected one JSON document")
		return
	}
	if v.Spec.Project != r.PathValue("p") || v.Spec.Name != r.PathValue("name") {
		writeErr(w, 400, "invalid_view", "path project/name must match spec")
		return
	}
	if _, err := s.backend.GetProject(r.Context(), v.Spec.Project); err != nil {
		viewError(w, err)
		return
	}
	for kind := range v.Spec.Custom {
		if _, ok := v.Templates[kind]; !ok {
			writeErr(w, 400, "invalid_view", "missing uploaded template "+kind)
			return
		}
	}
	for name, text := range v.Templates {
		if name != "html" && name != "latex" && name != "markdown" {
			writeErr(w, 400, "invalid_view", "unknown template kind "+name)
			return
		}
		if name != "html" {
			if err := views.ValidateTemplate(text); err != nil {
				writeErr(w, 400, "invalid_view", err.Error())
				return
			}
		}
		if len(text) > 256<<10 {
			writeErr(w, 400, "invalid_view", fmt.Sprintf("template %s exceeds 256 KB", name))
			return
		}
	}
	input, err := st.ViewInput(r.Context(), v.Spec, nil)
	if err == nil {
		_, err = views.Render(v.Spec, input, time.Now().Unix())
	}
	if err == nil {
		err = st.SaveView(r.Context(), v)
	}
	if err != nil {
		viewError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"name": v.Spec.Name})
}
func (s *Server) handleDeleteView(w http.ResponseWriter, r *http.Request) {
	st := s.viewStore(w)
	if st == nil {
		return
	}
	err := st.DeleteView(r.Context(), r.PathValue("p"), r.PathValue("name"))
	if err != nil {
		viewError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) handleJobView(w http.ResponseWriter, r *http.Request) {
	st := s.viewStore(w)
	if st == nil {
		return
	}
	j, err := st.ResolveJobRef(r.Context(), r.PathValue("id"))
	if err != nil {
		var ambiguous *store.AmbiguousRefError
		if !errors.As(err, &ambiguous) {
			err = fmt.Errorf("%w: %v", backend.ErrNotFound, err)
		}
		writeError(w, err)
		return
	}
	input, err := st.ViewInput(r.Context(), views.Spec{Name: "results", Project: j.ProjectName}, []string{j.ID})
	if err != nil {
		viewError(w, err)
		return
	}
	tasks, err := st.ListTasks(r.Context(), store.TaskFilter{JobID: j.ID, LiveOnly: true})
	if err != nil {
		writeError(w, err)
		return
	}
	rows := make([]job.Row, 0, len(tasks))
	for _, t := range tasks {
		var p job.TaskParams
		_ = json.Unmarshal([]byte(t.ParamsJSON), &p)
		rows = append(rows, job.Row{Params: p})
	}
	results, specs, err := views.JobViews(j.ProjectName, input, job.VaryingKeys(rows), time.Now().Unix())
	if err != nil {
		viewError(w, err)
		return
	}
	// ?format=latex|markdown|csv&name=<view>: one default view as text, as a
	// saved view's render exports it.
	if format := r.URL.Query().Get("format"); format != "" && format != "json" {
		name := r.URL.Query().Get("name")
		for _, res := range results {
			if res.Name == name {
				text, err := views.Export(res, format, r.URL.Query().Get("wide") == "true", r.URL.Query().Get("latex_color") == "true")
				if err != nil {
					writeErr(w, 400, "invalid_view", err.Error())
					return
				}
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				fmt.Fprint(w, text)
				return
			}
		}
		writeErr(w, 404, "not_found", "no default view "+name)
		return
	}
	writeJSON(w, 200, views.JobView{JobID: j.ID, Views: results, Specs: specs})
}

func (s *Server) handleRenderView(w http.ResponseWriter, r *http.Request) {
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
	input, err := st.ViewInput(r.Context(), v.Spec, r.URL.Query()["job"])
	if err != nil {
		viewError(w, err)
		return
	}
	result, err := views.Render(v.Spec, input, time.Now().Unix())
	if err != nil {
		viewError(w, err)
		return
	}
	if err := s.resolveViewSources(r, st, v, input, result); err != nil {
		viewError(w, err)
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" || format == "json" {
		writeJSON(w, 200, result)
		return
	}
	text, err := views.Export(result, format, r.URL.Query().Get("wide") == "true", r.URL.Query().Get("latex_color") == "true")
	if tpl, ok := v.Templates[format]; ok {
		text, err = views.RenderTemplate(tpl, format, result)
	}
	if err != nil {
		writeErr(w, 400, "invalid_view", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, text)
}
