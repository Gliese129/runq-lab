package dashboard

// Remote-CLI forward guard (RQ-45). The forwarded socket on a login node
// turns "compromised cluster account" into "full proxy of the laptop
// daemon" unless the surface is scoped. The guard enforces the remote
// CLI's honest worldview — "I am runq on THIS cluster, and only this
// cluster":
//
//   - identity is the TRANSPORT, not a credential: each target's forward
//     wraps its own guard instance, so the target name is bound at wiring
//     time. A token would be readable by exactly the uid that can already
//     open the socket — a credential to manage, no boundary gained.
//   - default-deny route allowlist; config/target mutation, log-sessions,
//     and everything belonging to OTHER targets is refused with 403.
//   - clean IS allowed, forced to this target: an attacker with the ssh
//     account can already rm -rf this cluster's files — blocking clean
//     here protects nothing (the cross-target reach is what must die).
//   - submit/plan/preview bodies get target forced-or-verified; job/task
//     actions resolve ownership through the store first (killing another
//     cluster's job from here is exactly the escalation this exists for).
//     The id is resolved by the same resolver the handlers use (refs.go:
//     full id, unique prefix, <experiment>/<cell>[@vN]) and the path is
//     rewritten to the full id, so the guard checks the object the handler
//     acts on. Unknown and ambiguous ids stop here. A job is admitted only
//     when EVERY target it spans is this one: an experiment whose versions
//     run on several targets is whole-job-addressable only from the
//     controller (its versions on this target stay reachable as tasks).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gliese129/runq-lab/internal/backend"
)

// guardBodyLimit caps buffered request bodies (submit configs are small).
const guardBodyLimit = 10 << 20

// RemoteCLIHandler wraps the full v1 mux in the forward guard for one
// target. Everything not explicitly allowed is 403.
func (s *Server) RemoteCLIHandler(target string) http.Handler {
	owner, _ := s.backend.(refOwner)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/health" {
			writeJSON(w, http.StatusOK, map[string]any{"controller_id": s.controllerID, "forward_target": target, "version": s.version})
			return
		}
		if s.guardAllows(w, r, target, owner) {
			s.Handler().ServeHTTP(w, r)
		}
	})
}

// guardAllows applies the allowlist. It may REWRITE r (query/body target
// scoping) before approving. On refusal it writes the 403 itself.
func (s *Server) guardAllows(w http.ResponseWriter, r *http.Request, target string, owner refOwner) bool {
	deny := func(what string) bool {
		writeErr(w, http.StatusForbidden, backend.CodeForbidden,
			fmt.Sprintf("%s is not available through the remote-CLI forward (scoped to target %q)", what, target))
		return false
	}

	// Decoded segments; an escaped %2F stays inside its segment.
	seg, ok := apiSegments(r.URL)
	if !ok { // not under /api/v1 (static SPA etc.)
		return deny("non-API path")
	}
	path := strings.Join(seg, "/")
	get := r.Method == http.MethodGet

	switch seg[0] {
	case "health", "config":
		return get || deny(r.Method+" "+path)

	case "projects":
		// Read-only: a poisoned command_template would execute wherever a
		// LAPTOP user later submits that project — cross-target via human.
		if len(seg) > 2 && seg[2] == "views" {
			return deny("views require the controller connection")
		}
		return get || deny("project mutation")

	case "jobs":
		switch {
		case len(seg) == 1 && get: // GET /jobs — scope the listing
			q := r.URL.Query()
			q.Set("target", target)
			r.URL.RawQuery = q.Encode()
			return true
		case len(seg) >= 2: // /jobs/{id}[/...] — ownership gate
			return s.checkOwner(w, r, target, seg[1], owner)
		}
		return deny(r.Method + " " + path)

	case "drafts":
		switch {
		case len(seg) == 1 && get:
			return true
		case len(seg) == 1 && r.Method == http.MethodPost: // create: pinned to this target
			return s.forceBodyField(w, r, "target", target)
		}
		resolver, ok := s.backend.(interface {
			DraftTarget(context.Context, string) (string, error)
		})
		if !ok {
			return deny("draft ownership unavailable")
		}
		got, err := resolver.DraftTarget(r.Context(), seg[1])
		if err != nil {
			writeDraftError(w, err)
			return false
		}
		return got == target || deny("draft belongs to another target")

	case "tasks":
		switch {
		case len(seg) == 1 && get:
			q := r.URL.Query()
			q.Set("target", target)
			r.URL.RawQuery = q.Encode()
			return true
		case len(seg) >= 2:
			return s.checkOwner(w, r, target, seg[1], owner)
		}
		return deny(r.Method + " " + path)

	case "targets":
		// Only THIS target's resources, and only the non-mutating ones
		// (+refresh, +parse-script). PUT/DELETE/check/connect stay refused.
		if len(seg) < 3 || seg[1] != target {
			return deny(r.Method + " " + path)
		}
		rest := strings.Join(seg[2:], "/")
		switch {
		case get && (rest == "gpus" || rest == "fs/list"):
			return true
		case r.Method == http.MethodPost && rest == "refresh":
			return true
		}
		return deny(r.Method + " " + path)

	case "clean":
		if r.Method != http.MethodPost {
			return deny(r.Method + " " + path)
		}
		// Same-target clean is allowed: the ssh account can already delete
		// this cluster's files by hand. Force the scope so shared-DB
		// selectors (job_id, older_than) cannot reach other targets.
		return s.forceBodyField(w, r, "target", target)
	}

	return deny(r.Method + " " + path)
}

// checkOwner resolves the addressed job/task with the handlers' resolver
// (rewriting r to the full id) and admits only an object whose every
// target is this forward's. Fail closed: no resolver, lookup error,
// unknown id (404) or ambiguous prefix (409, candidates withheld — they
// may name other targets' ids) are all answered here.
func (s *Server) checkOwner(w http.ResponseWriter, r *http.Request, target, ref string, owner refOwner) bool {
	if owner == nil {
		writeErr(w, http.StatusForbidden, backend.CodeForbidden, "ownership resolution unavailable — refusing by default")
		return false
	}
	kind, got, _, err := resolvePathRef(r, owner)
	switch {
	case err == nil:
	case isAmbiguous(err):
		writeErr(w, http.StatusConflict, backend.CodeAmbiguous, fmt.Sprintf("%s id prefix is ambiguous — use a longer id", kind))
		return false
	default:
		writeError(w, err)
		return false
	}
	var foreign []string
	for _, t := range got.Targets {
		if t != target {
			foreign = append(foreign, t)
		}
	}
	switch {
	case len(foreign) == 0:
		return true
	case len(foreign) < len(got.Targets): // spans this target and others
		writeErr(w, http.StatusForbidden, backend.CodeForbidden,
			fmt.Sprintf("%s %s also runs on other targets (%s) — act on the whole %s from the controller, or on this target's versions by task id",
				kind, got.ID, strings.Join(foreign, ", "), kind))
	default:
		writeErr(w, http.StatusForbidden, backend.CodeForbidden,
			fmt.Sprintf("%s %q belongs to another target", kind, ref))
	}
	return false
}

// forceBodyField overwrites one body field unconditionally.
func (s *Server) forceBodyField(w http.ResponseWriter, r *http.Request, field, value string) bool {
	doc, ok := s.readBodyDoc(w, r)
	if !ok {
		return false
	}
	doc[field] = value
	return s.replaceBody(w, r, doc)
}

func (s *Server) readBodyDoc(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, guardBodyLimit))
	if err != nil {
		writeErr(w, http.StatusBadRequest, backend.CodeBadRequest, "read body: "+err.Error())
		return nil, false
	}
	doc := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &doc); err != nil {
			writeErr(w, http.StatusBadRequest, backend.CodeBadRequest, "invalid JSON body: "+err.Error())
			return nil, false
		}
	}
	return doc, true
}

func (s *Server) replaceBody(w http.ResponseWriter, r *http.Request, doc map[string]any) bool {
	buf, err := json.Marshal(doc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, backend.CodeInternal, "re-encode body: "+err.Error())
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(buf))
	r.ContentLength = int64(len(buf))
	return true
}
