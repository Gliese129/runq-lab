package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/store"
)

type curve struct {
	Key   string      `json:"key"`
	Lines []curveLine `json:"lines"`
}

// jobCurves is GET /jobs/{id}/curves: every runq.log curve of the
// experiment's cells (selected versions), and the project's pinned keys.
type jobCurves struct {
	JobID  string   `json:"job_id"`
	Pins   []string `json:"pins"`
	Curves []curve  `json:"curves"`
	// Unreadable: cells whose metrics.jsonl could not be read (e.g. their
	// target is no longer configured); the rest still show.
	Unreadable []unreadableCell `json:"unreadable"`
}

type unreadableCell struct {
	TaskID string `json:"task_id"`
	Cell   string `json:"cell"`
	Error  string `json:"error"`
}

func (s *Server) registerCurves() {
	s.mux.HandleFunc("GET /api/v1/jobs/{id}/curves", s.handleJobCurves)
	s.mux.HandleFunc("PUT /api/v1/jobs/{id}/curve-pins", s.handlePutCurvePins)
}

func (s *Server) curveJob(w http.ResponseWriter, r *http.Request) (*store.Store, *store.JobRow) {
	st := s.viewStore(w)
	if st == nil {
		return nil, nil
	}
	j, err := st.ResolveJobRef(r.Context(), r.PathValue("id"))
	if err != nil {
		var ambiguous *store.AmbiguousRefError
		if !errors.As(err, &ambiguous) {
			err = fmt.Errorf("%w: %v", backend.ErrNotFound, err)
		}
		writeError(w, err)
		return nil, nil
	}
	return st, j
}

func (s *Server) handleJobCurves(w http.ResponseWriter, r *http.Request) {
	st, j := s.curveJob(w, r)
	if j == nil {
		return
	}
	points := 400
	if raw := r.URL.Query().Get("points"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 2 || n > 2000 {
			writeErrorStatus(w, http.StatusBadRequest, fmt.Errorf("points must be 2..2000"))
			return
		}
		points = n
	}
	h, ok := s.backend.(sourceHistorian)
	if !ok {
		writeErrorStatus(w, http.StatusNotImplemented, fmt.Errorf("history queries unavailable"))
		return
	}
	pins, err := st.CurvePins(r.Context(), j.ProjectName)
	if err != nil {
		writeError(w, err)
		return
	}
	tasks, err := st.ListTasks(r.Context(), store.TaskFilter{JobID: j.ID, LiveOnly: true})
	if err != nil {
		writeError(w, err)
		return
	}
	out := jobCurves{JobID: j.ID, Pins: pins, Curves: []curve{}, Unreadable: []unreadableCell{}}
	at := map[string]int{}
	for _, t := range tasks {
		cell := t.RunName
		if cell == "" {
			cell = t.ID
		}
		history, err := h.SourceHistory(r.Context(), t.ID, "metrics", "step", points)
		if err != nil {
			out.Unreadable = append(out.Unreadable, unreadableCell{TaskID: t.ID, Cell: cell, Error: err.Error()})
			continue
		}
		for _, sr := range history.Series {
			i, seen := at[sr.Key]
			if !seen {
				i = len(out.Curves)
				at[sr.Key] = i
				out.Curves = append(out.Curves, curve{Key: sr.Key})
			}
			out.Curves[i].Lines = append(out.Curves[i].Lines, lineOf(t.ID, cell, sr))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePutCurvePins(w http.ResponseWriter, r *http.Request) {
	st, j := s.curveJob(w, r)
	if j == nil {
		return
	}
	var body struct {
		Keys []string `json:"keys"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	if len(body.Keys) > 200 {
		writeErrorStatus(w, http.StatusBadRequest, fmt.Errorf("at most 200 pinned curves"))
		return
	}
	if err := st.SetCurvePins(r.Context(), j.ProjectName, body.Keys); err != nil {
		writeError(w, err)
		return
	}
	pins, err := st.CurvePins(r.Context(), j.ProjectName)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pins": pins})
}
