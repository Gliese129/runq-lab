package preflight

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gliese129/runq-lab/internal/job"
)

// HFRef is one HuggingFace Hub repository project.yaml declares under
// preflight.hf. RepoType "any" = model or dataset (whichever is cached).
type HFRef struct {
	RepoID   string `json:"repo_id"`
	RepoType string `json:"repo_type"` // "model" | "dataset" | "any"
}

// hfRepoIDRegex is the shape of a Hub repo id: `name` or `org/name`.
var hfRepoIDRegex = regexp.MustCompile(`^[A-Za-z0-9][\w.\-]*(?:/[A-Za-z0-9][\w.\-]*)?$`)

func looksLikeHFRepoID(s string) bool {
	if s == "" || strings.ContainsAny(s[:1], "/.~") || strings.ContainsAny(s, "{$") {
		return false
	}
	return hfRepoIDRegex.MatchString(s)
}

// cacheDirs are the hub-cache directory names the repo may live under
// (huggingface_hub layout: models--org--name / datasets--org--name).
func (r HFRef) cacheDirs() []string {
	flat := strings.ReplaceAll(r.RepoID, "/", "--")
	switch r.RepoType {
	case "model":
		return []string{"models--" + flat}
	case "dataset":
		return []string{"datasets--" + flat}
	}
	return []string{"models--" + flat, "datasets--" + flat}
}

// DownloadCommand is the ready-made pre-download command for a ref,
// surfaced with the warning (CLI prints it, the dashboard can offer it).
func (r HFRef) DownloadCommand() string {
	if r.RepoType == "dataset" {
		return "huggingface-cli download --repo-type dataset " + r.RepoID
	}
	return "huggingface-cli download " + r.RepoID
}

// contractParamRegex matches a "{{param.NAME}}" preflight.hf entry.
var contractParamRegex = regexp.MustCompile(`^\{\{param\.(\w+)}}$`)

// expandContractHF turns the declared preflight.hf list into refs.
// "{{param.NAME}}" entries expand against EVERY task's value. Entries that
// cannot be checked (not a repo id, a param no task defines) come back as
// problems — warned, never dropped silently.
func expandContractHF(declared []string, taskParams []job.TaskParams) ([]HFRef, []string) {
	var refs []HFRef
	var problems []string
	seen := map[string]bool{}
	add := func(id, origin string) {
		if !looksLikeHFRepoID(id) {
			problems = append(problems, fmt.Sprintf("preflight.hf entry %s resolves to %q, not a HuggingFace repo id", origin, id))
			return
		}
		if !seen[id] {
			seen[id] = true
			refs = append(refs, HFRef{RepoID: id, RepoType: "any"})
		}
	}
	for _, entry := range declared {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		m := contractParamRegex.FindStringSubmatch(entry)
		if m == nil {
			add(entry, fmt.Sprintf("%q", entry))
			continue
		}
		found := false
		for _, params := range taskParams {
			if v, ok := params[m[1]]; ok {
				found = true
				add(fmt.Sprintf("%v", v), entry)
			}
		}
		if !found {
			problems = append(problems, fmt.Sprintf("preflight.hf entry %q references a param no task defines", entry))
		}
	}
	return refs, problems
}
