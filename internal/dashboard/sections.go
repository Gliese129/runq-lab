package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gliese129/runq-lab/internal/backend"
)

// Sections endpoints (rc6). Organization only — the remote-CLI forward keeps
// them read-only like every project mutation, except moving one of its own
// jobs (POST /jobs/{id}/section, ownership-gated).

func (s *Server) sectioner(w http.ResponseWriter) (backend.Sectioner, bool) {
	sec, ok := s.backend.(backend.Sectioner)
	if !ok {
		writeErrorStatus(w, http.StatusNotImplemented, fmt.Errorf("sections are not supported by this backend"))
	}
	return sec, ok
}

func (s *Server) handleListSections(w http.ResponseWriter, r *http.Request) {
	sec, ok := s.sectioner(w)
	if !ok {
		return
	}
	out, err := sec.ListSections(r.Context(), r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sections": out})
}

func (s *Server) handleCreateSection(w http.ResponseWriter, r *http.Request) {
	sec, ok := s.sectioner(w)
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	out, err := sec.CreateSection(r.Context(), r.PathValue("name"), body.Name)
	if err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleRemoveSection(w http.ResponseWriter, r *http.Request) {
	sec, ok := s.sectioner(w)
	if !ok {
		return
	}
	moved, err := sec.RemoveSection(r.Context(), r.PathValue("name"), r.PathValue("section"))
	if err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"unsorted": moved})
}

func (s *Server) handleAddSectionRule(w http.ResponseWriter, r *http.Request) {
	sec, ok := s.sectioner(w)
	if !ok {
		return
	}
	var body struct {
		Section string `json:"section"`
		Kind    string `json:"kind"`
		Pattern string `json:"pattern"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	out, err := sec.AddSectionRule(r.Context(), r.PathValue("name"), body.Section, body.Kind, body.Pattern)
	if err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleRemoveSectionRule(w http.ResponseWriter, r *http.Request) {
	sec, ok := s.sectioner(w)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErrorStatus(w, http.StatusBadRequest, fmt.Errorf("rule id must be a number"))
		return
	}
	if err := sec.RemoveSectionRule(r.Context(), r.PathValue("name"), id); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "removed"})
}

func (s *Server) handleSortSections(w http.ResponseWriter, r *http.Request) {
	sec, ok := s.sectioner(w)
	if !ok {
		return
	}
	moved, err := sec.SortSections(r.Context(), r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"moved": moved})
}

func (s *Server) handleMoveJob(w http.ResponseWriter, r *http.Request) {
	sec, ok := s.sectioner(w)
	if !ok {
		return
	}
	var body struct {
		Section string `json:"section"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	id, err := sec.MoveJob(r.Context(), r.PathValue("id"), body.Section)
	if err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "section": body.Section})
}
