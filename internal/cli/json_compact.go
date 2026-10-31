package cli

import (
	"encoding/json"
	"io"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/preflight"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/views"
	"github.com/spf13/cobra"
)

// ── --json is compact; --detail is the full payload ──
//
// Agents read --json, and most of what the HTTP API returns is for the
// dashboard: every version's params, rendered commands, the job config,
// scheduler fields, chart specs. The CLI keeps what an agent acts on —
// ids, names, statuses, counts, times, cell/version refs, metrics, links —
// with the API's field names, so the compact form is a subset of the full
// one. --detail prints the API response as it is. The API stays whole.

const detailHelp = "with --json: the full payload (default: a compact summary)"

// addJSONFlags registers --json and --detail on a command.
func addJSONFlags(cmd *cobra.Command, jsonHelp string) {
	cmd.Flags().Bool("json", false, jsonHelp)
	cmd.Flags().Bool("detail", false, detailHelp)
}

func wantDetail(cmd *cobra.Command) bool {
	d, _ := cmd.Flags().GetBool("detail")
	return d
}

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// jobJSON is one experiment.
type jobJSON struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Project     string                 `json:"project"`
	Section     string                 `json:"section"`
	Status      string                 `json:"status"`
	Target      string                 `json:"target"`
	Tasks       backend.TaskCountGroup `json:"tasks"`
	CreatedAt   int64                  `json:"created_at"`
	FinishedAt  *int64                 `json:"finished_at,omitempty"`
	RefreshedAt *int64                 `json:"refreshed_at,omitempty"`
	Stale       bool                   `json:"stale,omitempty"`
	URL         string                 `json:"url,omitempty"`
}

func compactJob(j backend.JobSummary) jobJSON {
	return jobJSON{ID: j.ID, Name: j.Name, Project: j.Project, Section: j.Section, Status: j.Status,
		Target: j.Target, Tasks: j.Tasks, CreatedAt: j.CreatedAt, FinishedAt: j.FinishedAt,
		RefreshedAt: j.RefreshedAt, Stale: j.Stale, URL: j.URL}
}

func compactJobs(jobs []backend.JobSummary) []jobJSON {
	out := make([]jobJSON, len(jobs))
	for i, j := range jobs {
		out[i] = compactJob(j)
	}
	return out
}

// taskJSON is one version. In a list it carries the cell's selected
// version only; `runq show tk…` adds job, project and failure evidence.
type taskJSON struct {
	ID            string             `json:"id"`
	RunName       string             `json:"run_name,omitempty"`
	Version       int                `json:"version,omitempty"`
	JobID         string             `json:"job_id,omitempty"`
	Project       string             `json:"project,omitempty"`
	SupersededBy  string             `json:"superseded_by,omitempty"`
	Status        string             `json:"status"`
	CurrentStep   *int               `json:"current_step,omitempty"`
	Metrics       map[string]float64 `json:"metrics,omitempty"`
	StartedAt     *int64             `json:"started_at,omitempty"`
	FinishedAt    *int64             `json:"finished_at,omitempty"`
	QueueSec      *float64           `json:"queue_seconds,omitempty"`
	SchedulerSec  *float64           `json:"scheduler_seconds,omitempty"`
	ElapsedSec    *float64           `json:"elapsed_seconds,omitempty"`
	ExitCode      *int               `json:"exit_code,omitempty"`
	FailureDetail string             `json:"failure_detail,omitempty"`
	Links         map[string]string  `json:"links,omitempty"`
	RefreshedAt   *int64             `json:"refreshed_at,omitempty"`
	Stale         bool               `json:"stale,omitempty"`
	URL           string             `json:"url,omitempty"`
}

func compactTask(t backend.TaskView) taskJSON {
	return taskJSON{ID: t.ID, RunName: t.RunName, Version: t.Version, Status: t.Status,
		CurrentStep: t.CurrentStep, Metrics: t.Metrics, StartedAt: t.StartedAt, FinishedAt: t.FinishedAt,
		QueueSec: t.QueueSec, SchedulerSec: t.SchedulerSec, ElapsedSec: t.ElapsedSec, ExitCode: t.ExitCode, RefreshedAt: t.RefreshedAt, Stale: t.Stale, URL: t.URL}
}

// compactTasks keeps each cell's selected version: replaced ones are
// history (--detail lists them).
func compactTasks(tasks []backend.TaskView) []taskJSON {
	out := []taskJSON{}
	for _, t := range tasks {
		if t.SupersededBy == "" {
			out = append(out, compactTask(t))
		}
	}
	return out
}

// compactTaskShow is `runq show tk…`: one version, with what explains it.
func compactTaskShow(t backend.TaskView) taskJSON {
	c := compactTask(t)
	c.JobID, c.Project, c.SupersededBy = t.JobID, t.Project, t.SupersededBy
	c.FailureDetail, c.Links = t.FailureDetail, t.Links
	return c
}

type batchJSON struct {
	Seq         int    `json:"seq"`
	SubmittedAt int64  `json:"submitted_at"`
	TaskCount   int    `json:"task_count"`
	DraftID     string `json:"draft_id,omitempty"`
	Note        string `json:"note,omitempty"`
}

// jobDetailJSON is `runq show ex…`.
type jobDetailJSON struct {
	Job        jobJSON           `json:"job"`
	Tasks      []taskJSON        `json:"tasks"`
	MetricKeys []string          `json:"metric_keys,omitempty"`
	Batches    []batchJSON       `json:"batches,omitempty"`
	Pinned     map[string]string `json:"pinned,omitempty"`
	Wait       *waitResult       `json:"wait,omitempty"`
}

func compactJobDetail(d *backend.JobDetail, wait *waitResult) jobDetailJSON {
	out := jobDetailJSON{Job: compactJob(d.Job), Tasks: compactTasks(d.Tasks), MetricKeys: d.MetricKeys,
		Pinned: d.Pinned, Wait: wait}
	for _, b := range d.Batches {
		out.Batches = append(out.Batches, batchJSON{Seq: b.Seq, SubmittedAt: b.SubmittedAt,
			TaskCount: b.TaskCount, DraftID: b.DraftID, Note: b.Note})
	}
	return out
}

// draftJSON is one draft.
type draftJSON struct {
	ID        string   `json:"id"`
	Project   string   `json:"project"`
	Name      string   `json:"name"`
	Note      string   `json:"note,omitempty"`
	Section   string   `json:"section,omitempty"`
	Target    string   `json:"target"`
	Into      string   `json:"into,omitempty"`
	Rerun     []string `json:"rerun,omitempty"`
	Rows      int      `json:"rows"`
	NeedsCode int      `json:"needs_code"`
	Rev       int      `json:"rev"`
	Edited    bool     `json:"edited,omitempty"`
	Status    string   `json:"status"`
	JobID     string   `json:"job_id,omitempty"`
	UpdatedAt int64    `json:"updated_at"`
}

func compactDraft(v backend.DraftView) draftJSON {
	return draftJSON{ID: v.ID, Project: v.Project, Name: v.Name, Note: v.Note, Section: v.Section,
		Target: v.Target, Into: v.Into, Rerun: v.Rerun, Rows: v.Rows, NeedsCode: v.NeedsCode, Rev: v.Rev,
		Edited: v.Edited, Status: v.Status, JobID: v.JobID, UpdatedAt: v.UpdatedAt}
}

func compactDrafts(list []backend.DraftView) []draftJSON {
	out := make([]draftJSON, len(list))
	for i, v := range list {
		out[i] = compactDraft(v)
	}
	return out
}

// appendJSON is an append's counts; --detail lists each cell's class.
type appendJSON struct {
	Into       string `json:"into"`
	Batch      int    `json:"batch"`
	New        int    `json:"new"`
	NewVersion int    `json:"new_version"`
	Skipped    int    `json:"skipped"`
	Rerun      int    `json:"rerun,omitempty"`
}

func compactAppend(a *job.AppendSummary) *appendJSON {
	if a == nil {
		return nil
	}
	return &appendJSON{Into: a.Into, Batch: a.Batch, New: a.New, NewVersion: a.NewVersion, Skipped: a.Skipped, Rerun: a.Rerun}
}

// draftResultJSON is `runq draft` and `runq show dr…`: the plan table and
// what needs a look before submit; preview, YAML and the row model are
// --detail.
type draftResultJSON struct {
	Draft     draftJSON               `json:"draft"`
	Table     string                  `json:"table"`
	Baseline  string                  `json:"baseline,omitempty"`
	Warnings  []string                `json:"warnings,omitempty"`
	Preflight []preflight.CheckResult `json:"preflight,omitempty"` // warnings only
	Discarded []string                `json:"discarded,omitempty"`
	Append    *appendJSON             `json:"append,omitempty"`
}

func compactDraftResult(r *backend.DraftResult) draftResultJSON {
	return draftResultJSON{Draft: compactDraft(r.Draft), Table: r.Table, Baseline: r.Baseline, Warnings: r.Warnings,
		Preflight: preflight.Report{Results: r.Preflight}.Warnings(), Discarded: r.Discarded, Append: compactAppend(r.Append)}
}

type viewSummaryJSON struct {
	Project   string `json:"project"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	InSection string `json:"in_section,omitempty"`
}

func compactViewList(list []store.ViewSummary) []viewSummaryJSON {
	out := make([]viewSummaryJSON, len(list))
	for i, v := range list {
		out[i] = viewSummaryJSON{Project: v.Project, Name: v.Name, Title: v.Title, InSection: v.InSection}
	}
	return out
}

// viewJSON is a rendered view: its table (values and their text), the
// jobs behind it and what needs attention; chart specs, formats and each
// cell's statistics are --detail.
type viewJSON struct {
	Name        string                `json:"name"`
	Title       string                `json:"title"`
	GeneratedAt int64                 `json:"generated_at"`
	Sources     []viewSourceJSON      `json:"sources"`
	Extra       []viewExtraJSON       `json:"extra,omitempty"`
	RowKeys     []string              `json:"row_keys"`
	Columns     []viewColumnJSON      `json:"columns"`
	Rows        []viewRowJSON         `json:"rows"`
	Legend      string                `json:"legend,omitempty"`
	Data        map[string]views.Data `json:"data,omitempty"`
}

type viewSourceJSON struct {
	JobID      string           `json:"job_id"`
	Note       string           `json:"note,omitempty"`
	Status     string           `json:"status"`
	Tasks      int              `json:"tasks"`
	Running    int              `json:"running"`
	Attention  []map[string]any `json:"attention,omitempty"`
	Incomplete bool             `json:"incomplete,omitempty"`
	Warnings   []string         `json:"warnings,omitempty"`
}

type viewExtraJSON struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

type viewColumnJSON struct {
	Name  string `json:"name"`
	Group any    `json:"group,omitempty"`
	At    any    `json:"at,omitempty"`
}

type viewRowJSON struct {
	Key    map[string]any `json:"key"`
	Cells  []viewCellJSON `json:"cells"`
	State  string         `json:"state,omitempty"`
	TaskID string         `json:"task_id,omitempty"`
	JobID  string         `json:"job_id,omitempty"`
	Empty  bool           `json:"empty,omitempty"`
}

type viewCellJSON struct {
	Value     any    `json:"value"`
	Text      string `json:"text"`
	Rank      int    `json:"rank,omitempty"`
	DeltaText string `json:"delta_text,omitempty"`
}

func compactView(r *views.Result) viewJSON {
	out := viewJSON{Name: r.Name, Title: r.Title, GeneratedAt: r.GeneratedAt, RowKeys: r.RowKeys,
		Legend: r.Legend, Data: r.Data, Sources: []viewSourceJSON{}, Columns: []viewColumnJSON{}, Rows: []viewRowJSON{}}
	for _, s := range r.Sources {
		out.Sources = append(out.Sources, viewSourceJSON{JobID: s.JobID, Note: s.Note, Status: s.Status, Tasks: s.Tasks,
			Running: s.Running, Attention: s.Attention, Incomplete: s.Incomplete, Warnings: s.Warnings})
	}
	for _, e := range r.Extra {
		out.Extra = append(out.Extra, viewExtraJSON{Name: e.Name, Text: e.Text})
	}
	for _, c := range r.Columns {
		out.Columns = append(out.Columns, viewColumnJSON{Name: c.Name, Group: c.Group, At: c.At})
	}
	for _, row := range r.Rows {
		cr := viewRowJSON{Key: row.Key, State: row.State, TaskID: row.TaskID, JobID: row.JobID, Empty: row.Empty,
			Cells: make([]viewCellJSON, len(row.Cells))}
		for i, c := range row.Cells {
			cr.Cells[i] = viewCellJSON{Value: c.Value, Text: c.Text, Rank: c.Rank, DeltaText: c.DeltaText}
		}
		out.Rows = append(out.Rows, cr)
	}
	return out
}

// jobViewJSON is `runq view --experiment ex…`; the default specs are
// --spec (as YAML) or --detail.
type jobViewJSON struct {
	JobID string     `json:"job_id"`
	Views []viewJSON `json:"views"`
}

func compactJobView(v *views.JobView) jobViewJSON {
	out := jobViewJSON{JobID: v.JobID, Views: []viewJSON{}}
	for _, r := range v.Views {
		out.Views = append(out.Views, compactView(r))
	}
	return out
}

// printJSONOr prints full with --detail, else compact().
func printJSONOr(cmd *cobra.Command, full any, compact func() any) {
	if wantDetail(cmd) {
		printJSON(full)
		return
	}
	printJSON(compact())
}
