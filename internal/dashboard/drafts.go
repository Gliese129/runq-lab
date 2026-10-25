package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/store"
)

// Draft endpoints (rc6). The CLI (`runq draft …`, `runq submit dr…`) and,
// later, the dashboard's plan editor share them; every write is rev-checked.

func (s *Server) drafter(w http.ResponseWriter) (backend.Drafter, bool) {
	d, ok := s.backend.(backend.Drafter)
	if !ok {
		writeErrorStatus(w, http.StatusNotImplemented, fmt.Errorf("drafts are not supported by this backend"))
	}
	return d, ok
}

// writeDraftError maps draft conflicts to 409 {code: changed|submitted}
// and misses to 404; anything else is a 400 (the plan is invalid).
func writeDraftError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrDraftChanged):
		writeJSON(w, http.StatusConflict, backend.ErrorResponse{Error: err.Error(), Code: "changed"})
	case errors.Is(err, store.ErrDraftSubmitted):
		writeJSON(w, http.StatusConflict, backend.ErrorResponse{Error: err.Error(), Code: "submitted"})
	case errors.Is(err, store.ErrDraftNotFound):
		writeJSON(w, http.StatusNotFound, backend.ErrorResponse{Error: err.Error(), Code: "not_found"})
	default:
		writeJSON(w, http.StatusBadRequest, backend.ErrorResponse{Error: err.Error(), Code: "invalid_plan"})
	}
}

func (s *Server) handleCreateDraft(w http.ResponseWriter, r *http.Request) {
	d, ok := s.drafter(w)
	if !ok {
		return
	}
	var req backend.DraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	res, err := d.CreateDraft(r.Context(), req)
	if err != nil {
		writeDraftError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) handleListDrafts(w http.ResponseWriter, r *http.Request) {
	d, ok := s.drafter(w)
	if !ok {
		return
	}
	list, err := d.ListDrafts(r.Context(), r.URL.Query().Get("project"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": list})
}

func (s *Server) handleGetDraft(w http.ResponseWriter, r *http.Request) {
	d, ok := s.drafter(w)
	if !ok {
		return
	}
	if r.URL.Query().Get("format") == "yaml" {
		out, err := d.DraftYAML(r.Context(), r.PathValue("id"))
		if err != nil {
			writeDraftError(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		_, _ = w.Write(out)
		return
	}
	res, err := d.GetDraft(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDraftError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleEditDraft — PUT /drafts/{id}: the draft's editable YAML, either as
// the raw body (application/yaml) or as JSON {"yaml": "…"}.
func (s *Server) handleEditDraft(w http.ResponseWriter, r *http.Request) {
	d, ok := s.drafter(w)
	if !ok {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	text := string(raw)
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt == "application/json" {
		var body struct {
			YAML string `json:"yaml"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			writeErrorStatus(w, http.StatusBadRequest, err)
			return
		}
		text = body.YAML
	}
	if strings.TrimSpace(text) == "" {
		writeErrorStatus(w, http.StatusBadRequest, fmt.Errorf("empty draft yaml"))
		return
	}
	res, err := d.EditDraft(r.Context(), r.PathValue("id"), text)
	if err != nil {
		writeDraftError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSubmitDraft(w http.ResponseWriter, r *http.Request) {
	d, ok := s.drafter(w)
	if !ok {
		return
	}
	var body struct {
		Rev         int  `json:"rev"`
		NoPreflight bool `json:"no_preflight"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
			writeErrorStatus(w, http.StatusBadRequest, err)
			return
		}
	}
	res, err := d.SubmitDraft(r.Context(), r.PathValue("id"), body.Rev, body.NoPreflight)
	if err != nil {
		if errors.Is(err, store.ErrDraftChanged) || errors.Is(err, store.ErrDraftNotFound) {
			writeDraftError(w, err)
			return
		}
		writeError(w, err) // submit failures keep their usual codes
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleDiscardDraft(w http.ResponseWriter, r *http.Request) {
	d, ok := s.drafter(w)
	if !ok {
		return
	}
	if err := d.DiscardDraft(r.Context(), r.PathValue("id")); err != nil {
		writeDraftError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "discarded"})
}
