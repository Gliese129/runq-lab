package job

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Appending to a job (`runq draft job.yaml --into jb…`): one experiment = one
// job that keeps growing. A cell is a run name. A planned row whose name the
// job already has is a new version of that cell, and it must keep the cell's
// params apart from the scheduler-only ones (walltime, reservation id …);
// the command may change and is only flagged.

const (
	CellNew        = "new"
	CellNewVersion = "new_version" // the selected version failed or was killed
	CellSkipped    = "skipped"     // succeeded, or a version is queued or running
	CellRerun      = "rerun"       // succeeded, run again on request (--rerun)
)

// ExistingCell is a cell of the target job, described by its selected version.
type ExistingCell struct {
	Name     string
	TaskID   string // the selected version
	Status   string // its status
	InFlight bool   // some version is still queued or running
	Params   TaskParams
	Command  string // the command template its version was submitted with
	Versions int    // how many versions the cell has
}

// CellClass is the verdict for one planned row.
type CellClass struct {
	Run    string `json:"run"`
	Class  string `json:"class"`
	TaskID string `json:"task_id,omitempty"` // the cell's selected version
	Status string `json:"status,omitempty"`  // its status
	// CommandChanged: this version runs another command than the cell's
	// selected one (allowed, shown next to the cell).
	CommandChanged bool `json:"command_changed,omitempty"`
}

// AppendSummary is what an append would do (preview) or did (submit).
type AppendSummary struct {
	Into       string      `json:"into"`
	Batch      int         `json:"batch"` // the submission number it gets
	New        int         `json:"new"`
	NewVersion int         `json:"new_version"`
	Skipped    int         `json:"skipped"`
	Rerun      int         `json:"rerun"`
	Cells      []CellClass `json:"cells"`
}

// Submits reports how many versions the append creates.
func (s AppendSummary) Submits() int { return s.New + s.NewVersion + s.Rerun }

// Line is the one-line count used by CLI output and previews.
func (s AppendSummary) Line() string {
	line := fmt.Sprintf("new %d, new_version %d, skipped %d", s.New, s.NewVersion, s.Skipped)
	if s.Rerun > 0 {
		line += fmt.Sprintf(", rerun %d", s.Rerun)
	}
	return line
}

// SameCommand compares two command templates ignoring whitespace runs.
func SameCommand(a, b string) bool {
	return strings.Join(strings.Fields(a), " ") == strings.Join(strings.Fields(b), " ")
}

// paramDiff lists the params (scheduler-only ones left out) whose values
// differ between a planned row and the cell it names.
func paramDiff(row, cell TaskParams, exclude map[string]bool) []string {
	keys := map[string]bool{}
	for k := range row {
		keys[k] = true
	}
	for k := range cell {
		keys[k] = true
	}
	var out []string
	for k := range keys {
		if exclude[k] {
			continue
		}
		a, inRow := row[k]
		b, inCell := cell[k]
		switch {
		case inRow && inCell && SameValue(a, b):
		case !inCell:
			out = append(out, fmt.Sprintf("%s=%s (the cell has none)", k, FormatValue(a)))
		case !inRow:
			out = append(out, fmt.Sprintf("%s (the cell has %s)", k, FormatValue(b)))
		default:
			out = append(out, fmt.Sprintf("%s=%s (the cell has %s)", k, FormatValue(a), FormatValue(b)))
		}
	}
	sort.Strings(out)
	return out
}

// ClassifyCells sorts planned rows against the job's cells. rerun names
// succeeded cells to run again; command is this submission's template. A row
// that names a cell with other params, or a rerun of a cell that is not
// there or still running, is a plan error.
func ClassifyCells(rows []Row, existing []ExistingCell, exclude map[string]bool, rerun []string, command string) ([]CellClass, error) {
	byName := make(map[string]ExistingCell, len(existing))
	for _, e := range existing {
		byName[e.Name] = e
	}
	again := map[string]bool{}
	var problems []string
	for _, name := range rerun {
		e, ok := byName[name]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("--rerun %q: the job has no such cell", name))
		case e.InFlight:
			problems = append(problems, fmt.Sprintf("--rerun %q: a version is still queued or running; kill it first", name))
		}
		again[name] = true
	}
	out := make([]CellClass, len(rows))
	for i, r := range rows {
		c := CellClass{Run: r.Run, Class: CellNew}
		e, ok := byName[r.Run]
		if ok {
			if diff := paramDiff(r.Params, e.Params, exclude); len(diff) > 0 {
				problems = append(problems, fmt.Sprintf("row %q names an existing cell with other params: %s — give the row a new name, or keep the cell's params", r.Run, strings.Join(diff, ", ")))
			}
			c.TaskID, c.Status = e.TaskID, e.Status
			switch {
			case e.InFlight:
				c.Class = CellSkipped
			case e.Status == "success" && again[r.Run]:
				c.Class = CellRerun
			case e.Status == "success":
				c.Class = CellSkipped
			default:
				c.Class = CellNewVersion
			}
			if c.Class != CellSkipped && command != "" && e.Command != "" && !SameCommand(command, e.Command) {
				c.CommandChanged = true
			}
		}
		out[i] = c
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "\n"))
	}
	return out, nil
}

// SummarizeCells counts the verdicts.
func SummarizeCells(into string, batch int, cells []CellClass) AppendSummary {
	s := AppendSummary{Into: into, Batch: batch, Cells: cells}
	for _, c := range cells {
		switch c.Class {
		case CellNew:
			s.New++
		case CellNewVersion:
			s.NewVersion++
		case CellSkipped:
			s.Skipped++
		case CellRerun:
			s.Rerun++
		}
	}
	return s
}

// ErrNothingToAppend: every planned cell already succeeded or is running.
var ErrNothingToAppend = errors.New("nothing to append")

// NothingToAppendError explains ErrNothingToAppend for s.
func NothingToAppendError(s AppendSummary) error {
	return fmt.Errorf("%w: all %d cells in %s already succeeded or are still running (--rerun <cell> runs a succeeded one again)", ErrNothingToAppend, len(s.Cells), s.Into)
}

// Report is the append block of previews and `show dr…`: the counts, then
// one line per cell that gets a new version or is skipped.
func (s AppendSummary) Report() string {
	out := fmt.Sprintf("append to %s as submission %d: %s\n", s.Into, s.Batch, s.Line())
	for _, c := range s.Cells {
		note := ""
		if c.CommandChanged {
			note = " (command differs from the selected version)"
		}
		switch c.Class {
		case CellSkipped:
			out += fmt.Sprintf("  skip %q: version %s is %s\n", c.Run, c.TaskID, c.Status)
		case CellNewVersion:
			out += fmt.Sprintf("  new version of %q: the selected one (%s) %s%s\n", c.Run, c.TaskID, c.Status, note)
		case CellRerun:
			out += fmt.Sprintf("  rerun %q: new version after %s%s\n", c.Run, c.TaskID, note)
		}
	}
	return out
}
