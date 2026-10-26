package job

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Appending to a job (`runq submit dr… --into jb…`): one experiment = one
// job that keeps growing. A cell is its params minus scheduler-only params
// (walltime, reservation id …): moving a cell to another queue does not
// make it a different experiment.

const (
	CellNew             = "new"
	CellSkippedExisting = "skipped_existing" // done, running or queued
	CellSkippedFailed   = "skipped_failed"   // failed or killed, no --rerun-failed
	CellRerunFailed     = "rerun_failed"     // failed or killed, rerun as a new task
)

// ExistingCell is a live (not superseded) task of the target job.
type ExistingCell struct {
	TaskID  string
	RunName string
	Status  string
	Params  TaskParams
}

// CellClass is the verdict for one planned row.
type CellClass struct {
	Run    string `json:"run"`
	Class  string `json:"class"`
	TaskID string `json:"task_id,omitempty"` // the existing task it matched
	Status string `json:"status,omitempty"`  // that task's status
}

// AppendSummary is what an append would do (preview) or did (submit).
type AppendSummary struct {
	Into            string      `json:"into"`
	Batch           int         `json:"batch"` // the batch number it gets
	New             int         `json:"new"`
	SkippedExisting int         `json:"skipped_existing"`
	SkippedFailed   int         `json:"skipped_failed"`
	RerunFailed     int         `json:"rerun_failed"`
	Cells           []CellClass `json:"cells"`
	// Renamed lists new cells whose run name was taken in the job.
	Renamed map[string]string `json:"renamed,omitempty"`
}

// Submits reports how many tasks the append creates.
func (s AppendSummary) Submits() int { return s.New + s.RerunFailed }

// Line is the one-line count used by CLI output and previews.
func (s AppendSummary) Line() string {
	line := fmt.Sprintf("new %d, skipped_existing %d, skipped_failed %d", s.New, s.SkippedExisting, s.SkippedFailed)
	if s.RerunFailed > 0 {
		line += fmt.Sprintf(", rerun_failed %d", s.RerunFailed)
	}
	return line
}

// CellKey is the identity of a cell: params in sorted key order, values by
// SameValue (1 and 1.0 are the same cell, true and "true" are not), exclude
// left out.
func CellKey(p TaskParams, exclude map[string]bool) string {
	if len(exclude) == 0 {
		return paramsKey(p)
	}
	kept := make(TaskParams, len(p))
	for k, v := range p {
		if !exclude[k] {
			kept[k] = v
		}
	}
	return paramsKey(kept)
}

// DistinctCells is a plan error when two rows are the same cell once the
// scheduler-only params are left out: they would run the same experiment
// twice (and an append would rerun one failure twice).
func DistinctCells(runs []string, params []TaskParams, exclude map[string]bool) error {
	seen := make(map[string]int, len(params))
	for i, p := range params {
		k := CellKey(p, exclude)
		if j, ok := seen[k]; ok {
			hint := ""
			if len(exclude) > 0 {
				names := make([]string, 0, len(exclude))
				for n := range exclude {
					names = append(names, n)
				}
				sort.Strings(names)
				hint = " (scheduler-only params do not make a cell: " + strings.Join(names, ", ") + ")"
			}
			return fmt.Errorf("rows %q and %q are the same cell%s; drop one or make their params differ", runs[j], runs[i], hint)
		}
		seen[k] = i
	}
	return nil
}

func failedStatus(s string) bool { return s == "failed" || s == "killed" }

// ClassifyCells sorts planned rows against the job's live tasks. When one
// cell has several live tasks, a non-failed one wins (the cell has a run).
func ClassifyCells(rows []Row, existing []ExistingCell, exclude map[string]bool, rerunFailed bool) []CellClass {
	byKey := make(map[string]ExistingCell, len(existing))
	for _, e := range existing {
		k := CellKey(e.Params, exclude)
		if prev, ok := byKey[k]; ok && !failedStatus(prev.Status) {
			continue
		}
		byKey[k] = e
	}
	out := make([]CellClass, len(rows))
	for i, r := range rows {
		c := CellClass{Run: r.Run, Class: CellNew}
		if e, ok := byKey[CellKey(r.Params, exclude)]; ok {
			c.TaskID, c.Status = e.TaskID, e.Status
			switch {
			case !failedStatus(e.Status):
				c.Class = CellSkippedExisting
			case rerunFailed:
				c.Class = CellRerunFailed
				if e.RunName != "" { // a rerun keeps the cell's name (pre-rc6 tasks have none)
					c.Run = e.RunName
				}
			default:
				c.Class = CellSkippedFailed
			}
		}
		out[i] = c
	}
	return out
}

// SummarizeCells counts the verdicts. taken holds every run name already in
// the job; a new cell whose name is taken gets a " (bN)" suffix, recorded
// in Renamed and applied to cells.
func SummarizeCells(into string, batch int, cells []CellClass, taken map[string]bool) AppendSummary {
	s := AppendSummary{Into: into, Batch: batch, Cells: cells}
	for i := range cells {
		switch cells[i].Class {
		case CellNew:
			s.New++
			if taken[cells[i].Run] {
				name := uniqueName(cells[i].Run, batch, taken)
				if s.Renamed == nil {
					s.Renamed = map[string]string{}
				}
				s.Renamed[cells[i].Run] = name
				cells[i].Run = name
			}
			taken[cells[i].Run] = true
		case CellSkippedExisting:
			s.SkippedExisting++
		case CellSkippedFailed:
			s.SkippedFailed++
		case CellRerunFailed:
			s.RerunFailed++
		}
	}
	return s
}

func uniqueName(name string, batch int, taken map[string]bool) string {
	cand := fmt.Sprintf("%s (b%d)", name, batch)
	for n := 2; taken[cand]; n++ {
		cand = fmt.Sprintf("%s (b%d.%d)", name, batch, n)
	}
	return cand
}

// ErrNothingToAppend: every planned cell already exists in the job.
var ErrNothingToAppend = errors.New("nothing to append")

// NothingToAppendError explains ErrNothingToAppend for s.
func NothingToAppendError(s AppendSummary) error {
	total := len(s.Cells)
	if s.SkippedFailed > 0 {
		return fmt.Errorf("%w: all %d cells already exist in %s (%d failed — add --rerun-failed to run them again)", ErrNothingToAppend, total, s.Into, s.SkippedFailed)
	}
	return fmt.Errorf("%w: all %d cells already exist in %s", ErrNothingToAppend, total, s.Into)
}

// Report is the append block of previews and `show dr…`: the counts,
// then one line per skipped, rerun or renamed cell.
func (s AppendSummary) Report() string {
	out := fmt.Sprintf("append to %s as batch %d: %s\n", s.Into, s.Batch, s.Line())
	for _, c := range s.Cells {
		switch c.Class {
		case CellSkippedExisting:
			out += fmt.Sprintf("  skip %q: task %s is %s\n", c.Run, c.TaskID, c.Status)
		case CellSkippedFailed:
			out += fmt.Sprintf("  skip %q: task %s %s (--rerun-failed reruns it)\n", c.Run, c.TaskID, c.Status)
		case CellRerunFailed:
			out += fmt.Sprintf("  rerun %q: replaces %s task %s\n", c.Run, c.Status, c.TaskID)
		}
	}
	froms := make([]string, 0, len(s.Renamed))
	for from := range s.Renamed {
		froms = append(froms, from)
	}
	sort.Strings(froms)
	for _, from := range froms {
		out += fmt.Sprintf("  rename %q → %q: the job already has a run named %q\n", from, s.Renamed[from], from)
	}
	return out
}
