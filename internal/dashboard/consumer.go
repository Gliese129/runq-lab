package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/consumer"
	"github.com/gliese129/runq-lab/internal/logfile"
)

func (s *Server) consumerBackend(w http.ResponseWriter) backend.Consumers {
	b, ok := s.backend.(backend.Consumers)
	if !ok {
		writeErr(w, 501, "unsupported", "consumers unavailable")
		return nil
	}
	return b
}
func (s *Server) registerConsumers() {
	s.mux.HandleFunc("POST /api/v1/jobs/{id}/consumers", s.handleAddConsumer)
	s.mux.HandleFunc("GET /api/v1/jobs/{id}/consumers", s.handleListConsumers)
	s.mux.HandleFunc("GET /api/v1/consumers/{id}", s.handleGetConsumer)
	s.mux.HandleFunc("POST /api/v1/consumers/{id}/kill", s.handleKillConsumer)
	s.mux.HandleFunc("GET /api/v1/consumers/{id}/log", s.handleConsumerLog)
}
func (s *Server) handleAddConsumer(w http.ResponseWriter, r *http.Request) {
	b := s.consumerBackend(w)
	if b == nil {
		return
	}
	var cfg consumer.Config
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&cfg); err != nil {
		writeErr(w, 400, "invalid_consumer", err.Error())
		return
	}
	if err := d.Decode(new(any)); err != io.EOF {
		writeErr(w, 400, "invalid_consumer", "expected one JSON document")
		return
	}
	if err := cfg.Validate(); err != nil {
		writeErr(w, 400, "invalid_consumer", err.Error())
		return
	}
	c, err := b.AddConsumer(r.Context(), r.PathValue("id"), cfg)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 201, c)
}
func (s *Server) handleListConsumers(w http.ResponseWriter, r *http.Request) {
	b := s.consumerBackend(w)
	if b == nil {
		return
	}
	list, err := b.ListConsumers(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, list)
}
func (s *Server) handleGetConsumer(w http.ResponseWriter, r *http.Request) {
	b := s.consumerBackend(w)
	if b == nil {
		return
	}
	c, err := b.GetConsumer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, c)
}
func (s *Server) handleKillConsumer(w http.ResponseWriter, r *http.Request) {
	b := s.consumerBackend(w)
	if b == nil {
		return
	}
	s.handleAction(w, r, func() error { return b.KillConsumer(r.Context(), r.PathValue("id")) })
}
func (s *Server) handleConsumerLog(w http.ResponseWriter, r *http.Request) {
	b := s.consumerBackend(w)
	if b == nil {
		return
	}
	q := r.URL.Query()
	offset, _ := strconv.ParseInt(q.Get("offset"), 10, 64)
	budget, _ := strconv.ParseInt(q.Get("max_bytes"), 10, 64)
	page, err := b.ConsumerLog(r.Context(), r.PathValue("id"), logfile.PageRequest{Offset: offset, MaxBytes: budget, Tail: !q.Has("offset")})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, page)
}
