package dashboard

import (
	"net"
	"net/url"
	"strings"
)

// BaseURL turns the dashboard listen address into the URL a browser on
// the same machine opens. A wildcard bind (0.0.0.0, ::, ":8077") is
// reachable on loopback, so the link points there.
func BaseURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://" + listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// Page paths mirror web/dashboard/src/plugins/router.ts.

// JobURL is the job-detail page. Empty base → "" (no UI served).
func JobURL(base, project, jobID string) string {
	if base == "" || project == "" || jobID == "" {
		return ""
	}
	return pageURL(base, "projects", project, jobID)
}

// TaskURL is the task-detail page. Empty base → "".
func TaskURL(base, project, jobID, taskID string) string {
	if base == "" || project == "" || jobID == "" || taskID == "" {
		return ""
	}
	return pageURL(base, "projects", project, jobID, taskID)
}

// ViewURL is a project view page. Empty base → "".
func ViewURL(base, project, name string) string {
	if base == "" || project == "" || name == "" {
		return ""
	}
	return pageURL(base, "projects", project, "views", name)
}

func pageURL(base string, segments ...string) string {
	escaped := make([]string, len(segments))
	for i, s := range segments {
		escaped[i] = url.PathEscape(s)
	}
	return strings.TrimRight(base, "/") + "/" + strings.Join(escaped, "/")
}
