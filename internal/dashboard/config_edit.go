package dashboard

import (
	"context"

	"net/http"

	"time"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/config"

	"github.com/gliese129/runq-lab/internal/rfs"
)

// (ErrForwardRestartRequired is gone — RQ-75's lane reconciler builds a
// missing lane on the spot, so "restart to build a lane" no longer exists.)

// Targets management endpoints (spec §5.2, D10): /hpc-config* is retired,
// scheduler templates belong to targets. GET /targets is the management
// view (full TargetConfig); GET /config stays the bootstrap summary. The
// GUI form is SCHEMA-DRIVEN: placeholder vocabulary ships from
// config.HPCPlaceholders — single source of truth (C3).

type targetsListResponse struct {
	Items        []config.TargetConfig `json:"items"`
	Placeholders map[string][]string   `json:"placeholders"`
	Path         string                `json:"path"`
	// ConfigGeneration is config.yaml's semantic content hash at read time
	// (RQ-75). The edit form stores it and sends it back as If-Match; a
	// mismatch on save means someone else changed the file in between.
	ConfigGeneration string `json:"config_generation"`
	// Generations lists retired/retiring lane generations (RQ-75): same-name
	// entries render as sub-rows of their active target, removed-target
	// entries go to the collapsed archived section.
	Generations []backend.TargetGenerationView `json:"generations,omitempty"`
}

// handleListTargets — GET /targets: management view, full TargetConfig
// fields (spec §4/§5.2). Local config file, no freshness semantics.
func (s *Server) handleListTargets(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load()
	if err != nil {
		writeError(w, err)
		return
	}
	resp := targetsListResponse{
		Items:            cfg.ResolveTargets(),
		Placeholders:     config.HPCPlaceholders,
		Path:             config.ConfigPath(),
		ConfigGeneration: cfg.Generation,
	}
	if tg, ok := s.backend.(interface {
		TargetGenerations(context.Context) ([]backend.TargetGenerationView, error)
	}); ok {
		if gens, gerr := tg.TargetGenerations(r.Context()); gerr == nil {
			resp.Generations = gens
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// SetForwardStarter installs the runtime forward hook (client daemon only).
func (s *Server) SetForwardStarter(fn func(name string) error) { s.forwardStarter = fn }

// SetForwardStatus wires the client daemon's forward-status snapshot into
// /health (RQ-74).
func (s *Server) SetForwardStatus(fn func() map[string]rfs.ForwardStatus) { s.forwardStatus = fn }

// SetConfigChanged wires the daemon's config reconciler (RQ-75): called
// after every successful API write to config.yaml so lanes converge
// immediately instead of waiting for the next watch tick.
func (s *Server) SetConfigChanged(fn func()) { s.configChanged = fn }

// SetForwardStopper wires the client daemon's runtime forward teardown
// (POST /targets/{name}/disconnect).
func (s *Server) SetForwardStopper(fn func(name string) error) { s.forwardStopper = fn }

// handleDisconnectTarget — POST /targets/{name}/disconnect: stop the remote
// CLI forward at runtime. Idempotent; config (remote_cli: false) is the
// CLI's job, this endpoint only handles the live daemon state.
func (s *Server) handleDisconnectTarget(w http.ResponseWriter, r *http.Request) {
	if s.forwardStopper == nil {
		// Intentional degradation (RQ2-1 c6 closure): a server wired
		// without forward support (tests, embedded uses) reports 501
		// rather than pretending — not a stub awaiting implementation.
		notImplemented(w, "remote CLI forward")
		return
	}
	if err := s.forwardStopper(r.PathValue("name")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, backend.ActionResponse{OK: true})
}

// handleConnectTarget — POST /targets/{name}/connect: start or replace the
// target's remote CLI forward NOW, against the just-saved config. The
// forward starter runs a reconcile pass first (RQ-75), so even a target
// added moments ago gets its lane built on the spot — no restart case left.
func (s *Server) handleConnectTarget(w http.ResponseWriter, r *http.Request) {
	if s.forwardStarter == nil {
		// Intentional degradation — see handleDisconnectTarget.
		notImplemented(w, "remote CLI forward")
		return
	}
	if err := s.forwardStarter(r.PathValue("name")); err != nil {
		writeErr(w, http.StatusBadRequest, backend.CodeInvalidState, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, backend.ActionResponse{OK: true})
}

// handleRefreshTarget — POST /targets/{name}/refresh (spec §3 契约 3, D22):
// kick the lane's sensor for one forced full pass and wait for the pass
// that started AFTER the kick (generation fence in the lane), bounded by
// the request timeout. min_interval throttling and honest receipts are
// the lane's job; this handler only sets the wait budget.
func (s *Server) handleRefreshTarget(w http.ResponseWriter, r *http.Request) {
	fr, ok := s.backend.(interface {
		ForceRefreshTarget(context.Context, string) (*backend.RefreshReceipt, error)
	})
	if !ok {
		// Intentional degradation (RQ2-1 c6 closure): push-model backends
		// have no refresh concept — 501 is the honest answer, not a gap.
		notImplemented(w, "target refresh")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	receipt, err := fr.ForceRefreshTarget(ctx, r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}
