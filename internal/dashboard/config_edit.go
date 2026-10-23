package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/genfile"
	"github.com/gliese129/runq-lab/internal/store"

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

// SetTargetConnector installs the runtime SSH connection hook (client daemon only).
func (s *Server) SetTargetConnector(fn func(context.Context, string) error) { s.targetConnector = fn }

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

// handleConnectTarget checks SSH and, when configured, starts the remote CLI
// forward against the just-saved config. A plain SSH target needs no forward.
func (s *Server) handleConnectTarget(w http.ResponseWriter, r *http.Request) {
	if s.targetConnector == nil {
		// Intentional degradation — see handleDisconnectTarget.
		notImplemented(w, "remote CLI forward")
		return
	}
	if err := s.targetConnector(r.Context(), r.PathValue("name")); err != nil {
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

type targetDeleteResponse struct {
	OK bool `json:"ok"`
	// WasDefault: default_target was this target and is now unset.
	WasDefault bool `json:"was_default"`
	// ActiveTasks keep running to completion on the retiring lane; no new
	// work can be submitted to the target.
	ActiveTasks int `json:"active_tasks"`
}

// handleDeleteTarget — DELETE /targets/{name}: remove the target from
// config.yaml. Refused while a visible project still names it (its home
// target or a member of targets:) — the project would stop working.
// Already accepted tasks keep running under the retiring lane (config
// reconciler), and old jobs stay readable through historical lanes.
func (s *Server) handleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	cfg, err := config.Load()
	if err != nil {
		writeError(w, err)
		return
	}
	if t, ferr := cfg.FindTarget(name); ferr != nil || t == nil {
		writeErr(w, http.StatusNotFound, backend.CodeNotFound, fmt.Sprintf("target %q not found", name))
		return
	}
	users, err := s.projectsUsingTarget(r.Context(), name)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(users) > 0 {
		writeErr(w, http.StatusConflict, backend.CodeInvalidState, fmt.Sprintf(
			"target %q is still used by project(s) %s — point them at another target (or archive them) first",
			name, strings.Join(users, ", ")))
		return
	}
	active := 0
	for _, st := range store.ActiveStatuses {
		_, n, lerr := s.backend.ListTasks(r.Context(), backend.TaskListOptions{Target: name, Status: st, Limit: 1})
		if lerr == nil {
			active += n
		}
	}
	wasDefault, err := config.RemoveTarget(name)
	if err != nil {
		var conflict *genfile.ConflictError
		if errors.As(err, &conflict) {
			writeErr(w, http.StatusConflict, backend.CodeGenerationConflict, "config.yaml changed while deleting; reload and try again")
			return
		}
		writeError(w, err)
		return
	}
	if s.configChanged != nil {
		s.configChanged()
	}
	writeJSON(w, http.StatusOK, targetDeleteResponse{OK: true, WasDefault: wasDefault, ActiveTasks: active})
}

func (s *Server) projectsUsingTarget(ctx context.Context, name string) ([]string, error) {
	projects, err := s.backend.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	var users []string
	for _, p := range projects {
		if p.Archived {
			continue
		}
		pc, perr := s.backend.GetProject(ctx, p.Name)
		if perr != nil || pc == nil {
			continue
		}
		if pc.Target == name || slices.Contains(pc.Targets, name) {
			users = append(users, p.Name)
		}
	}
	return users, nil
}
