package dashboard

// One id resolver for /jobs/{id}… and /tasks/{id}…: a full id, a unique
// prefix, or (tasks) <experiment>/<cell>[@vN]. Handler() rewrites the path
// to the full id before routing, and the remote-CLI guard admits on the
// same resolution — so the object the guard checked is the object the
// handler acts on, whatever short form the client typed.

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/store"
)

// refOwner is the store-backed resolver (MultiBackend).
type refOwner interface {
	ResolveJobOwner(ctx context.Context, ref string) (backend.OwnedRef, error)
	ResolveTaskOwner(ctx context.Context, ref string) (backend.OwnedRef, error)
}

// apiSegments splits an /api/v1/… path into its decoded segments, keeping
// an escaped %2F inside one segment (an <experiment>/<cell> task ref).
// ok is false for paths outside /api/v1.
func apiSegments(u *url.URL) (segs []string, ok bool) {
	rest, ok := strings.CutPrefix(u.EscapedPath(), "/api/v1/")
	if !ok {
		return nil, false
	}
	segs = strings.Split(rest, "/")
	for i, raw := range segs {
		dec, err := url.PathUnescape(raw)
		if err != nil {
			return nil, false
		}
		segs[i] = dec
	}
	return segs, true
}

// pathRef is the {id} of a /jobs/{id}… or /tasks/{id}… path.
func pathRef(segs []string) (kind, ref string, ok bool) {
	if len(segs) < 2 {
		return "", "", false
	}
	switch segs[0] {
	case "jobs":
		return "job", segs[1], true
	case "tasks":
		return "task", segs[1], true
	}
	return "", "", false
}

// resolvePathRef resolves r's {id} and rewrites the path to the full id.
// hit is false when the path carries no job/task id.
func resolvePathRef(r *http.Request, owner refOwner) (kind string, got backend.OwnedRef, hit bool, err error) {
	segs, ok := apiSegments(r.URL)
	if !ok {
		return "", got, false, nil
	}
	kind, ref, ok := pathRef(segs)
	if !ok {
		return "", got, false, nil
	}
	if kind == "job" {
		got, err = owner.ResolveJobOwner(r.Context(), ref)
	} else {
		got, err = owner.ResolveTaskOwner(r.Context(), ref)
	}
	if err != nil {
		return kind, got, true, err
	}
	segs[1] = got.ID
	escaped := make([]string, len(segs))
	for i, seg := range segs {
		escaped[i] = url.PathEscape(seg)
	}
	r.URL.Path = "/api/v1/" + strings.Join(segs, "/")
	r.URL.RawPath = "/api/v1/" + strings.Join(escaped, "/")
	return kind, got, true, nil
}

// canonicalRefMiddleware hands every job/task handler the full id. An
// ambiguous prefix is answered here (409 + candidates); an unknown id goes
// on unchanged so the handler gives its usual 404.
func (s *Server) canonicalRefMiddleware(next http.Handler) http.Handler {
	owner, _ := s.backend.(refOwner)
	if owner == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, hit, err := resolvePathRef(r, owner); hit && err != nil && !backend.IsNotFound(err) {
			writeError(w, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isAmbiguous(err error) bool {
	var ambiguous *store.AmbiguousRefError
	return errors.As(err, &ambiguous)
}
