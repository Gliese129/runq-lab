package dashboard

import "github.com/gliese129/runq-lab/internal/utils"

// BaseURL turns the dashboard listen address into the URL a browser on
// the same machine opens (utils.DashboardBase).
func BaseURL(listen string) string { return utils.DashboardBase(listen) }

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

// DraftURL is a draft's editor page. Empty base → "".
func DraftURL(base, draftID string) string {
	if base == "" || draftID == "" {
		return ""
	}
	return pageURL(base, "drafts", draftID)
}

// ViewURL is a project view page. Empty base → "".
func ViewURL(base, project, name string) string {
	if base == "" || project == "" || name == "" {
		return ""
	}
	return pageURL(base, "projects", project, "views", name)
}

func pageURL(base string, segments ...string) string {
	return utils.DashboardPage(base, segments...)
}
