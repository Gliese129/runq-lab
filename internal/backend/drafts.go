package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/preflight"
	"github.com/gliese129/runq-lab/internal/remote"
	"github.com/gliese129/runq-lab/internal/store"
	"gopkg.in/yaml.v3"
)

// Drafts (rc6): `runq draft job.yaml` plans and validates without side
// effects (no sync, no scheduler, no repo writes; preflight runs and only
// warns) and keeps
// the plan server-side as dr…; `runq submit dr…` freezes it into a job.
// Humans and agents edit the same draft (rev-checked).

// Drafter is the draft service; the CLI reaches it through the proxy.
type Drafter interface {
	CreateDraft(ctx context.Context, req DraftRequest) (*DraftResult, error)
	ListDrafts(ctx context.Context, project string) ([]DraftView, error)
	GetDraft(ctx context.Context, ref string) (*DraftResult, error)
	DraftYAML(ctx context.Context, ref string) ([]byte, error)
	EditDraft(ctx context.Context, ref, yamlText string) (*DraftResult, error)
	SubmitDraft(ctx context.Context, ref string, opts DraftSubmitOptions) (*DraftSubmitResult, error)
	DiscardDraft(ctx context.Context, ref string) error
}

type DraftRequest struct {
	JobYAML    string `json:"job_yaml"`
	SourcePath string `json:"source_path,omitempty"`
	Project    string `json:"project,omitempty"` // overrides job.yaml project:
	Section    string `json:"section,omitempty"` // overrides job.yaml section:
	Name       string `json:"name,omitempty"`    // overrides job.yaml name:
	Target     string `json:"target,omitempty"`  // -t
	// Into makes the draft an append to that job (id or unique prefix);
	// Rerun names succeeded cells to run again. RetiredRerunFailed is the
	// old rerun_failed, refused with a pointer to the new default.
	Into               string   `json:"into,omitempty"`
	Rerun              []string `json:"rerun,omitempty"`
	RetiredRerunFailed bool     `json:"rerun_failed,omitempty"`
}

// DraftSubmitOptions: Rev > 0 must match the draft's rev; Into and Rerun
// set (or confirm) the draft's append. (A pre-rc6 client's no_preflight is
// ignored: preflight only warns.)
type DraftSubmitOptions struct {
	Rev                int      `json:"rev,omitempty"`
	Into               string   `json:"into,omitempty"`
	Rerun              []string `json:"rerun,omitempty"`
	RetiredRerunFailed bool     `json:"rerun_failed,omitempty"`
}

type DraftView struct {
	ID          string   `json:"id"`
	Project     string   `json:"project"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Section     string   `json:"section,omitempty"`
	Target      string   `json:"target"`
	Into        string   `json:"into,omitempty"`
	Rerun       []string `json:"rerun,omitempty"`
	Rows        int      `json:"rows"`
	NeedsCode   int      `json:"needs_code"`
	Rev         int      `json:"rev"`
	Edited      bool     `json:"edited"`
	Status      string   `json:"status"`
	JobID       string   `json:"job_id,omitempty"`
	SourcePath  string   `json:"source_path,omitempty"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}

type DraftResult struct {
	Draft    DraftView `json:"draft"`
	Table    string    `json:"table"` // plan table (run × varying params)
	Rows     []job.Row `json:"rows"`  // the same plan, structured (dashboard)
	Baseline string    `json:"baseline"`
	Preview  string    `json:"preview,omitempty"` // full rendered preview (create/edit, GetDraftPreview)
	// YAML is the editable form (`runq show dr… --yaml`) of this same rev,
	// so a page never pairs a table with another rev's text.
	YAML     string   `json:"yaml"`
	Warnings []string `json:"warnings,omitempty"`
	// Preflight is every check's result for this plan (warnings only, never
	// blocking).
	Preflight []preflight.CheckResult `json:"preflight,omitempty"`
	Discarded []string                `json:"discarded,omitempty"`
	// Append is set for a draft with into: what each row would do now.
	Append *job.AppendSummary `json:"append,omitempty"`
}

type DraftSubmitResult struct {
	JobID            string `json:"job_id"`
	TotalTasks       int    `json:"total_tasks"`
	DraftID          string `json:"draft_id"`
	AlreadySubmitted bool   `json:"already_submitted,omitempty"`
	// Append is set when the draft was appended to an existing job.
	Append *job.AppendSummary `json:"append,omitempty"`
	// PreflightWarnings are the submit's preflight warnings (it went
	// through anyway).
	PreflightWarnings []preflight.CheckResult `json:"preflight_warnings,omitempty"`
}

var draftLocks sync.Map // draft id → *sync.Mutex: submit, edit and discard take turns

// lockDraft serializes the writes to one draft, so a submit never freezes
// a rev an edit replaced underneath it.
func lockDraft(id string) func() {
	mu, _ := draftLocks.LoadOrStore(id, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	return mu.(*sync.Mutex).Unlock
}

func (m *MultiBackend) CreateDraft(ctx context.Context, req DraftRequest) (*DraftResult, error) {
	cfg, warns, err := parseDraftYAML(req.JobYAML)
	if err != nil {
		return nil, err
	}
	if req.Project != "" {
		cfg.Project = req.Project
	}
	if req.Section != "" {
		cfg.Section = req.Section
	}
	if req.Name != "" {
		cfg.Name = req.Name
	}
	if req.Into != "" {
		cfg.Into = req.Into
	}
	cfg.Rerun = job.MergeNames(cfg.Rerun, req.Rerun)
	cfg.RetiredRerunFailed = cfg.RetiredRerunFailed || req.RetiredRerunFailed
	if strings.TrimSpace(cfg.Project) == "" && cfg.Into == "" {
		return nil, fmt.Errorf("draft needs an explicit project: set project: in the job file or pass -p <project>")
	}
	cfg.SourcePath = req.SourcePath
	out, frozen, err := m.planDraft(ctx, cfg, req.Target)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(frozen)
	sum := sha256.Sum256([]byte(req.JobYAML))
	row := &store.DraftRow{Project: frozen.Project, Target: out.Target, ConfigJSON: string(raw),
		SourcePath: req.SourcePath, SourceSHA: hex.EncodeToString(sum[:])}
	discarded, err := m.store.CreateDraft(ctx, row)
	if err != nil {
		return nil, err
	}
	res, err := m.draftResult(ctx, row)
	if err != nil {
		return nil, err
	}
	res.Preview, res.Warnings, res.Discarded, res.Preflight = out.Preview, warns, discarded, out.Preflight.Results
	if out.Append != nil {
		res.Append = out.Append
	}
	return res, nil
}

// planDraft validates cfg exactly as submit would (target, section, rows,
// templates, choices, types; preflight warns) without side effects, and returns
// the frozen form: every row explicit under tasks:, sweep and fixed_params
// folded in.
func (m *MultiBackend) planDraft(ctx context.Context, cfg job.JobConfig, target string) (PreviewResult, job.JobConfig, error) {
	if len(m.snapshot()) == 0 { // nothing could run it: say what to do first
		return PreviewResult{}, job.JobConfig{}, noTargetConfiguredError()
	}
	if err := cfg.CheckAppendFields(); err != nil {
		return PreviewResult{}, job.JobConfig{}, err
	}
	if cfg.Into != "" {
		if _, err := m.resolveInto(ctx, &cfg, target); err != nil {
			return PreviewResult{}, job.JobConfig{}, err
		}
	}
	name, err := m.registry.Resolve(ctx, cfg.Project)
	if err != nil {
		return PreviewResult{}, job.JobConfig{}, err
	}
	cfg.Project = name
	rows, err := job.Rows(&cfg)
	if err != nil {
		return PreviewResult{}, job.JobConfig{}, err
	}
	out, err := m.PreviewSubmitForTarget(ctx, target, cfg)
	if err != nil {
		return PreviewResult{}, job.JobConfig{}, err
	}
	frozen := cfg
	frozen.Sweep, frozen.FixedParams, frozen.Tasks = nil, nil, make([]job.TaskRow, len(rows))
	for i, r := range rows {
		frozen.Tasks[i] = job.TaskRow{Run: r.Run, NeedsCode: r.NeedsCode, Params: r.Params}
	}
	if frozen.Baseline == "" && len(rows) > 0 {
		frozen.Baseline = rows[0].Run
	}
	frozen.Draft, frozen.Rev, frozen.FromJob, frozen.SectionID = "", 0, "", ""
	return out, frozen, nil
}

func parseDraftYAML(text string) (job.JobConfig, []string, error) {
	var cfg job.JobConfig
	if err := yaml.Unmarshal([]byte(text), &cfg); err != nil {
		return cfg, nil, fmt.Errorf("parse job yaml: %w", err)
	}
	return cfg, cfg.NormalizeLegacy(), nil
}

func (m *MultiBackend) draftResult(ctx context.Context, d *store.DraftRow) (*DraftResult, error) {
	if d.Project == "" {
		if fresh, err := m.store.GetDraft(ctx, d.ID); err == nil {
			d = fresh
		}
	}
	var cfg job.JobConfig
	if err := json.Unmarshal([]byte(d.ConfigJSON), &cfg); err != nil {
		return nil, fmt.Errorf("draft %s: %w", d.ID, err)
	}
	rows, err := job.Rows(&cfg)
	if err != nil {
		return nil, err
	}
	view := draftView(d, cfg, rows)
	var order []string
	if proj, err := m.registry.GetCached(ctx, d.Project); err == nil {
		for _, p := range proj.Params {
			order = append(order, p.Name)
		}
	}
	text, err := m.draftYAML(ctx, d, cfg, rows)
	if err != nil {
		return nil, err
	}
	res := &DraftResult{Draft: view, Table: job.PlanTable(rows, cfg.Baseline, order), Rows: rows, YAML: string(text)}
	if len(rows) > 0 {
		res.Baseline = rows[job.BaselineIndex(rows, cfg.Baseline)].Run
	}
	if cfg.Into != "" && d.Status == "open" {
		if s, err := m.storedAppendSummary(ctx, cfg, rows, d.Target); err == nil {
			res.Append = s
		}
	}
	return res, nil
}

func draftView(d *store.DraftRow, cfg job.JobConfig, rows []job.Row) DraftView {
	v := DraftView{ID: d.ID, Project: d.Project, Name: cfg.Name, Description: cfg.Description, Section: cfg.Section,
		Target: d.Target, Into: cfg.Into, Rerun: cfg.Rerun, Rows: len(rows), Rev: d.Rev, Edited: d.Rev > 1, Status: d.Status, JobID: d.JobID,
		SourcePath: d.SourcePath, CreatedAt: d.CreatedAt.Unix(), UpdatedAt: d.UpdatedAt.Unix()}
	for _, r := range rows {
		if len(r.NeedsCode) > 0 {
			v.NeedsCode++
		}
	}
	return v
}

func (m *MultiBackend) ListDrafts(ctx context.Context, project string) ([]DraftView, error) {
	if project != "" {
		name, err := m.registry.Resolve(ctx, project)
		if err != nil {
			return nil, err
		}
		project = name
	}
	list, err := m.store.ListDrafts(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]DraftView, 0, len(list))
	for i := range list {
		var cfg job.JobConfig
		_ = json.Unmarshal([]byte(list[i].ConfigJSON), &cfg)
		out = append(out, draftView(&list[i], cfg, rowsOrNil(&cfg)))
	}
	return out, nil
}

func rowsOrNil(cfg *job.JobConfig) []job.Row {
	rows, _ := job.Rows(cfg)
	return rows
}

func (m *MultiBackend) GetDraft(ctx context.Context, ref string) (*DraftResult, error) {
	d, err := m.store.GetDraft(ctx, ref)
	if err != nil {
		return nil, err
	}
	return m.draftResult(ctx, d)
}

// GetDraftPreview is GetDraft plus the plan's full preview and preflight,
// re-planned now (side-effect free) — what the draft page opens with. An
// open draft only: a submitted one is history.
func (m *MultiBackend) GetDraftPreview(ctx context.Context, ref string) (*DraftResult, error) {
	d, err := m.store.GetDraft(ctx, ref)
	if err != nil {
		return nil, err
	}
	res, err := m.draftResult(ctx, d)
	if err != nil || d.Status != "open" {
		return res, err
	}
	var cfg job.JobConfig
	if err := json.Unmarshal([]byte(d.ConfigJSON), &cfg); err != nil {
		return nil, err
	}
	out, _, err := m.planDraft(ctx, cfg, d.Target)
	if err != nil { // the plan no longer validates (project or target changed)
		res.Warnings = append(res.Warnings, "the plan does not validate now: "+err.Error())
		return res, nil
	}
	res.Preview, res.Preflight = out.Preview, out.Preflight.Results
	return res, nil
}

// DraftYAML is the draft's editable form: the export layout headed by
// draft/rev, so `runq draft edit -f` knows what it edits.
func (m *MultiBackend) DraftYAML(ctx context.Context, ref string) ([]byte, error) {
	d, err := m.store.GetDraft(ctx, ref)
	if err != nil {
		return nil, err
	}
	var cfg job.JobConfig
	if err := json.Unmarshal([]byte(d.ConfigJSON), &cfg); err != nil {
		return nil, err
	}
	rows, err := job.Rows(&cfg)
	if err != nil {
		return nil, err
	}
	return m.draftYAML(ctx, d, cfg, rows)
}

func (m *MultiBackend) draftYAML(ctx context.Context, d *store.DraftRow, cfg job.JobConfig, rows []job.Row) ([]byte, error) {
	in := job.ExportInput{DraftID: d.ID, Rev: d.Rev, Into: cfg.Into, Rerun: cfg.Rerun, Project: d.Project, Name: cfg.Name, Description: cfg.Description,
		Section: cfg.Section, Target: cfg.Target, Command: cfg.Command, Overrides: cfg.Overrides,
		Baseline: cfg.Baseline, Rows: rows}
	if proj, err := m.registry.GetCached(ctx, d.Project); err == nil {
		for _, p := range proj.Params {
			in.ParamOrder = append(in.ParamOrder, p.Name)
		}
	}
	return job.ExportYAML(in)
}

// EditDraft replaces the draft's plan with yamlText (its editable form).
// The rev in the text must be current; project and target stay fixed.
func (m *MultiBackend) EditDraft(ctx context.Context, ref, yamlText string) (*DraftResult, error) {
	d, err := m.store.GetDraft(ctx, ref)
	if err != nil {
		return nil, err
	}
	cfg, warns, err := parseDraftYAML(yamlText)
	if err != nil {
		return nil, err
	}
	if cfg.Draft != "" && cfg.Draft != d.ID {
		return nil, fmt.Errorf("the file is draft %s, not %s", cfg.Draft, d.ID)
	}
	if cfg.Rev == 0 {
		return nil, fmt.Errorf("the file has no rev: line; start from: runq show %s --yaml", d.ID)
	}
	if cfg.Project == "" {
		cfg.Project = d.Project
	}
	if name, err := m.registry.Resolve(ctx, cfg.Project); err != nil || name != d.Project {
		return nil, fmt.Errorf("a draft keeps its project (%s); start a new draft for another project", d.Project)
	}
	if cfg.Target != "" && cfg.Target != d.Target {
		return nil, fmt.Errorf("a draft keeps its target (%s); start a new draft for another target", d.Target)
	}
	out, frozen, err := m.planDraft(ctx, cfg, d.Target)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(frozen)
	unlock := lockDraft(d.ID)
	_, err = m.store.UpdateDraft(ctx, d.ID, cfg.Rev, string(raw))
	unlock()
	if err != nil {
		return nil, err
	}
	d, err = m.store.GetDraft(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	res, err := m.draftResult(ctx, d)
	if err != nil {
		return nil, err
	}
	res.Preview, res.Warnings, res.Preflight = out.Preview, warns, out.Preflight.Results
	return res, nil
}

// SubmitDraft freezes an open draft into a new job. Idempotent: submitting
// a submitted draft (again, or concurrently) returns the same job. rev > 0
// must match the draft's current rev.
func (m *MultiBackend) SubmitDraft(ctx context.Context, ref string, opts DraftSubmitOptions) (*DraftSubmitResult, error) {
	d, err := m.store.GetDraft(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer lockDraft(d.ID)()
	if d, err = m.store.GetDraft(ctx, d.ID); err != nil {
		return nil, err
	}
	if d.Status == "submitted" {
		total := 0
		if j, err := m.store.GetJob(ctx, d.JobID); err == nil && j != nil {
			total = j.TotalTasks
		}
		return &DraftSubmitResult{JobID: d.JobID, TotalTasks: total, DraftID: d.ID, AlreadySubmitted: true}, nil
	}
	// A submit that landed its job but lost the draft mark (the caller went
	// away in between) is finished here, not submitted twice.
	if b, err := m.store.DraftBatch(ctx, d.ID); err != nil {
		return nil, err
	} else if b != nil {
		if err := m.store.MarkDraftSubmitted(ctx, d.ID, b.JobID); err != nil && !errors.Is(err, store.ErrDraftSubmitted) {
			return nil, err
		}
		return &DraftSubmitResult{JobID: b.JobID, TotalTasks: b.TaskCount, DraftID: d.ID, AlreadySubmitted: true}, nil
	}
	if opts.Rev > 0 && opts.Rev != d.Rev {
		return nil, fmt.Errorf("%w (now rev %d)", store.ErrDraftChanged, d.Rev)
	}
	var cfg job.JobConfig
	if err := json.Unmarshal([]byte(d.ConfigJSON), &cfg); err != nil {
		return nil, err
	}
	if opts.Into != "" {
		j, err := m.store.ResolveJobRef(ctx, opts.Into)
		if err != nil {
			return nil, fmt.Errorf("--into: %w", err)
		}
		if cfg.Into != "" && cfg.Into != j.ID {
			return nil, fmt.Errorf("draft %s appends to %s, not %s; edit its into: line to change it", d.ID, cfg.Into, j.ID)
		}
		cfg.Into = j.ID
	}
	cfg.Rerun = job.MergeNames(cfg.Rerun, opts.Rerun)
	cfg.RetiredRerunFailed = cfg.RetiredRerunFailed || opts.RetiredRerunFailed
	if err := cfg.CheckAppendFields(); err != nil {
		return nil, err
	}
	cfg.Project, cfg.DraftID = d.Project, d.ID
	var appended job.AppendSummary
	var report preflight.Report
	sub := SubmitOptions{Target: d.Target, Preflight: &report}
	if cfg.Into != "" {
		sub.Append = &appended
	}
	jobID, n, err := m.SubmitJob(ctx, cfg, sub)
	if err != nil {
		return nil, err
	}
	// The job exists now: record that even if the caller has gone away.
	if err := m.store.MarkDraftSubmitted(context.WithoutCancel(ctx), d.ID, jobID); err != nil && !errors.Is(err, store.ErrDraftSubmitted) {
		return nil, err
	}
	out := &DraftSubmitResult{JobID: jobID, TotalTasks: n, DraftID: d.ID, PreflightWarnings: report.Warnings()}
	if sub.Append != nil {
		out.Append = sub.Append
	}
	return out, nil
}

func (m *MultiBackend) DiscardDraft(ctx context.Context, ref string) error {
	d, err := m.store.GetDraft(ctx, ref)
	if err != nil {
		return err
	}
	defer lockDraft(d.ID)()
	return m.store.DiscardDraft(ctx, d.ID)
}

// DraftTarget is the forward guard's ownership lookup.
func (m *MultiBackend) DraftTarget(ctx context.Context, ref string) (string, error) {
	d, err := m.store.GetDraft(ctx, ref)
	if err != nil {
		return "", err
	}
	return d.Target, nil
}

// OpenDraftCount backs the "N open drafts" line under `runq ps`.
func (m *MultiBackend) OpenDraftCount(ctx context.Context) (int, error) {
	return m.store.CountOpenDrafts(ctx)
}

var _ Drafter = (*MultiBackend)(nil)

// storedAppendSummary classifies a draft's rows against its job from the
// ledger alone (no target round trip) — what `show dr…` and the draft
// page show. Submit classifies again under the job's append lock.
func (m *MultiBackend) storedAppendSummary(ctx context.Context, cfg job.JobConfig, rows []job.Row, target string) (*job.AppendSummary, error) {
	tasks, err := m.store.ListTasks(ctx, store.TaskFilter{JobID: cfg.Into})
	if err != nil {
		return nil, err
	}
	picks, err := m.store.CellPicks(ctx, cfg.Into)
	if err != nil {
		return nil, err
	}
	batches, err := m.store.ListJobBatches(ctx, cfg.Into)
	if err != nil {
		return nil, err
	}
	seq, err := m.store.NextBatchSeq(ctx, cfg.Into)
	if err != nil {
		return nil, err
	}
	exclude := map[string]bool{}
	projectCmd := ""
	if proj, err := m.registry.GetCached(ctx, cfg.Project); err == nil {
		projectCmd = proj.CmdTemplate
		for _, p := range proj.Params {
			if p.Scope == "scheduler" {
				exclude[p.Name] = true
			}
		}
	}
	if be, ok := m.get(target); ok {
		if sp, ok := be.(interface{ SchedulerParamRefs() []string }); ok {
			for _, name := range sp.SchedulerParamRefs() {
				exclude[name] = true
			}
		}
	}
	command := cfg.Command
	if command == "" {
		command = projectCmd
	}
	cells, err := job.ClassifyCells(rows, remote.ExistingCells(tasks, picks, batches, projectCmd), exclude, cfg.Rerun, command)
	if err != nil {
		return nil, err
	}
	s := job.SummarizeCells(cfg.Into, seq, cells)
	return &s, nil
}
