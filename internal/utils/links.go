package utils

import (
	"net"
	"net/url"
	"strings"
)

// DashboardBase turns the dashboard listen address into the URL a browser
// on the same machine opens. A wildcard bind (0.0.0.0, ::, ":8077") is
// reachable on loopback, so the link points there.
func DashboardBase(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://" + listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// DashboardPage is a page of the dashboard SPA (web/dashboard/src/plugins/
// router.ts). Empty base → "" (no UI served).
func DashboardPage(base string, segments ...string) string {
	if base == "" {
		return ""
	}
	escaped := make([]string, len(segments))
	for i, s := range segments {
		escaped[i] = url.PathEscape(s)
	}
	// The SPA uses hash routing (createWebHashHistory): a plain path would
	// load index.html and land on the Overview.
	return strings.TrimRight(base, "/") + "/#/" + strings.Join(escaped, "/")
}
