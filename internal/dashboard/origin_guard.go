package dashboard

// Cross-site write guard for the TCP listener. A browser lets any page it
// renders send "simple" requests (a form post, a text/plain fetch) to the
// dashboard's address without a CORS preflight, and corsMiddleware only
// shapes response headers — it never stops the request. So on TCP every
// state-changing request must come from the dashboard's own origin:
//
//   - Origin (or, without it, Referer), when present, must name the host
//     the request was sent to (the Host header, or X-Forwarded-Host behind
//     a reverse proxy). Same-host, not an allow-list: the dashboard opened
//     at 127.0.0.1:8077, at a tailnet name or through the vite dev proxy
//     all pass; a page on any other origin does not.
//   - The body is JSON: a Content-Type outside application/json (and the
//     YAML a draft edit takes) is refused, as is a body without one. These
//     are the content types a browser cannot send cross-site without a
//     preflight, which corsMiddleware never grants to other origins.
//
// Requests without Origin and Referer — the CLI, the SDK, curl — are not
// browsers acting for another site and keep working. The unix socket and
// the remote-CLI forward serve Handler, without this guard.

import (
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/gliese129/runq-lab/internal/backend"
)

// TCPHandler is Handler for the dashboard's TCP listener: the same surface
// behind the cross-site write guard.
func (s *Server) TCPHandler() http.Handler {
	return s.handler(sameOriginWrites)
}

// sameOriginWrites refuses cross-site state-changing requests with 403.
func sameOriginWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if msg := crossSiteWrite(r); msg != "" {
				writeErr(w, http.StatusForbidden, backend.CodeForbidden, msg)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// crossSiteWrite returns why a write must be refused, or "".
func crossSiteWrite(r *http.Request) string {
	header, source := "Origin", r.Header.Get("Origin")
	if source == "" {
		header, source = "Referer", r.Header.Get("Referer")
	}
	if source != "" && !sameHost(source, r) {
		return fmt.Sprintf("cross-site %s %s refused: %s %q does not match this dashboard's host %q; writes are accepted only from the dashboard's own page or a non-browser client",
			r.Method, r.URL.Path, header, source, r.Host)
	}
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		if r.ContentLength != 0 {
			return fmt.Sprintf("%s %s refused: a request body needs Content-Type: application/json", r.Method, r.URL.Path)
		}
		return ""
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil || !acceptedWriteType(r, mt) {
		return fmt.Sprintf("%s %s refused: Content-Type %q is not accepted here; send application/json", r.Method, r.URL.Path, ct)
	}
	return ""
}

// sameHost reports whether the Origin/Referer URL names the host the
// request was sent to.
func sameHost(source string, r *http.Request) bool {
	u, err := url.Parse(source)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false // includes Origin: null (sandboxed or opaque origins)
	}
	want := canonicalHost(u.Host)
	if want == canonicalHost(r.Host) {
		return true
	}
	// Behind a reverse proxy (tailscale serve, nginx) the Host the browser
	// used may arrive as X-Forwarded-Host. A browser cannot set it on a
	// cross-site request without a preflight that is never granted.
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		first, _, _ := strings.Cut(fwd, ",")
		return want == canonicalHost(strings.TrimSpace(first))
	}
	return false
}

// canonicalHost lowercases host[:port] and drops a default port, so
// "https://box.ts.net" matches Host "box.ts.net:443".
func canonicalHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	for _, p := range []string{":80", ":443"} {
		if strings.HasSuffix(h, p) {
			return strings.TrimSuffix(h, p)
		}
	}
	return h
}

// acceptedWriteType: JSON everywhere; a draft edit (PUT /drafts/{id}) also
// takes its YAML raw. None of these is a CORS-simple type.
func acceptedWriteType(r *http.Request, mt string) bool {
	if mt == "application/json" || strings.HasSuffix(mt, "+json") {
		return true
	}
	if r.Method == http.MethodPut && isDraftPath(r.URL.Path) {
		switch mt {
		case "application/yaml", "application/x-yaml", "text/yaml", "text/x-yaml":
			return true
		}
	}
	return false
}

func isDraftPath(p string) bool {
	rest, ok := strings.CutPrefix(p, "/api/v1/drafts/")
	return ok && rest != "" && !strings.Contains(rest, "/")
}
