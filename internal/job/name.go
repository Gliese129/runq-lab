package job

import (
	"fmt"
	"strings"
)

// NormalizeLegacy folds pre-rc6 naming keys into Name and returns one
// warning line per legacy key (lossless mappings warn and continue).
//
//   - note: → name: (note was the display name)
//   - name: next to note: was the scheduler job-name override → ignored;
//     scheduler names come from project.yaml job_name
//   - {{…}} in the name: note templates are gone; shown as written
//   - preflight: (job level) → dropped with a warning: preflight always
//     runs, only warns, and is configured in project.yaml (hf:)
func (c *JobConfig) NormalizeLegacy() []string {
	var warns []string
	if c.Note != "" {
		if c.Name != "" && c.Name != c.Note {
			warns = append(warns, fmt.Sprintf("\"name\" next to \"note\" was a scheduler job-name override and is ignored (scheduler names come from project.yaml job_name); %q is now the job name", c.Note))
		}
		warns = append(warns, `"note" is renamed to "name"; rename the key`)
		c.Name, c.Note = c.Note, ""
	}
	if c.Preflight != nil {
		warns = append(warns, `job-level "preflight:" is no longer applied (preflight always runs and only warns; declare hf: repos in project.yaml); remove it`)
		c.Preflight = nil
	}
	if strings.Contains(c.Name, "{{") {
		warns = append(warns, fmt.Sprintf("job name %q: placeholders are no longer rendered; write the name as plain text", c.Name))
	}
	return warns
}
