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
	"github.com/gliese129/runq-lab/internal/store"
	"gopkg.in/yaml.v3"
)

// Drafts (rc6): `runq draft job.yaml` plans and validates without side
// effects (no sync, no preflight, no scheduler, no repo writes) and keeps
// the plan server-side as dr…; `runq submit dr…` freezes it into a job.
// Humans and agents edit the same draft (rev-checked).

// Drafter is the draft service; the CLI reaches it through the proxy.
type Drafter interface {
	CreateDraft(ctx context.Context, req DraftRequest) (*DraftResult, error)
	ListDrafts(ctx context.Context, project string) ([]DraftView, error)
	GetDraft(ctx context.Context, ref string) (*DraftResult, error)
	DraftYAML(ctx context.Context, ref string) ([]byte, error)
	EditDraft(ctx context.Context, ref, yamlText string) (*DraftResult, error)
	SubmitDraft(ctx context.Context, ref string, rev int, skipPreflight bool) (*DraftSubmitResult, error)
	DiscardDraft(ctx context.Context, ref string) error
}

type DraftRequest struct {
	JobYAML    string `json:"job_yaml"`
	SourcePath string `json:"source_path,omitempty"`
	Project    string `json:"project,omitempty"` // overrides job.yaml project:
	Section    string `json:"section,omitempty"` // overrides job.yaml section:
	Target     string `json:"target,omitempty"`  // -t
}

type DraftView struct {
	ID          string `json:"id"`
	Project     string `json:"project"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Section     string `json:"section,omitempty"`
	Target      string `json:"target"`
	Rows        int    `json:"rows"`
	NeedsCode   int    `json:"needs_code"`
	Rev         int    `json:"rev"`
	Edited      bool   `json:"edited"`
	Status      string `json:"status"`
	JobID       string `json:"job_id,omitempty"`
	SourcePath  string `json:"source_path,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type DraftResult struct {
	Draft     DraftView `json:"draft"`
	Table     string    `json:"table"`             // plan table (run × varying params)
	Preview   string    `json:"preview,omitempty"` // full rendered preview (create/edit)
	Warnings  []string  `json:"warnings,omitempty"`
	Discarded []string  `json:"discarded,omitempty"`
}

type DraftSubmitResult struct {
	JobID            string `json:"job_id"`
	TotalTasks       int    `json:"total_tasks"`
	DraftID          string `json:"draft_id"`
	AlreadySubmitted bool   `json:"already_submitted,omitempty"`
}

var draftLocks sync.Map // draft id → *sync.Mutex: one submit at a time

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
	if strings.TrimSpace(cfg.Project) == "" {
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
	res.Preview, res.Warnings, res.Discarded = out.Preview, warns, discarded
	return res, nil
}

// planDraft validates cfg exactly as submit would (target, section, rows,
// templates, choices, types) without preflight or side effects, and returns
// the frozen form: every row explicit under tasks:, sweep and fixed_params
// folded in.
func (m *MultiBackend) planDraft(ctx context.Context, cfg job.JobConfig, target string) (PreviewResult, job.JobConfig, error) {
	name, err := m.registry.Resolve(ctx, cfg.Project)
	if err != nil {
		return PreviewResult{}, job.JobConfig{}, err
	}
	cfg.Project = name
	rows, err := job.Rows(&cfg)
	if err != nil {
		return PreviewResult{}, job.JobConfig{}, err
	}
	out, err := m.PreviewSubmitForTarget(ctx, target, cfg, true)
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
	return &DraftResult{Draft: view, Table: job.PlanTable(rows, cfg.Baseline, order)}, nil
}

func draftView(d *store.DraftRow, cfg job.JobConfig, rows []job.Row) DraftView {
	v := DraftView{ID: d.ID, Project: d.Project, Name: cfg.Name, Description: cfg.Description, Section: cfg.Section,
		Target: d.Target, Rows: len(rows), Rev: d.Rev, Edited: d.Rev > 1, Status: d.Status, JobID: d.JobID,
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
	in := job.ExportInput{DraftID: d.ID, Rev: d.Rev, Project: d.Project, Name: cfg.Name, Description: cfg.Description,
		Section: cfg.Section, Target: cfg.Target, Command: cfg.Command, Overrides: cfg.Overrides,
		Consumers: cfg.Consumers, Baseline: cfg.Baseline, Rows: rows}
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
		return nil, fmt.Errorf("the file has no rev: line; start from: runq draft show %s --yaml", d.ID)
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
	if _, err := m.store.UpdateDraft(ctx, d.ID, cfg.Rev, string(raw)); err != nil {
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
	res.Preview, res.Warnings = out.Preview, warns
	return res, nil
}

// SubmitDraft freezes an open draft into a new job. Idempotent: submitting
// a submitted draft (again, or concurrently) returns the same job. rev > 0
// must match the draft's current rev.
func (m *MultiBackend) SubmitDraft(ctx context.Context, ref string, rev int, skipPreflight bool) (*DraftSubmitResult, error) {
	d, err := m.store.GetDraft(ctx, ref)
	if err != nil {
		return nil, err
	}
	mu, _ := draftLocks.LoadOrStore(d.ID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
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
	if rev > 0 && rev != d.Rev {
		return nil, fmt.Errorf("%w (now rev %d)", store.ErrDraftChanged, d.Rev)
	}
	var cfg job.JobConfig
	if err := json.Unmarshal([]byte(d.ConfigJSON), &cfg); err != nil {
		return nil, err
	}
	cfg.Project, cfg.DraftID = d.Project, d.ID
	jobID, n, err := m.SubmitJob(ctx, cfg, SubmitOptions{SkipPreflight: skipPreflight, Target: d.Target})
	if err != nil {
		return nil, err
	}
	if err := m.store.MarkDraftSubmitted(ctx, d.ID, jobID); err != nil && !errors.Is(err, store.ErrDraftSubmitted) {
		return nil, err
	}
	return &DraftSubmitResult{JobID: jobID, TotalTasks: n, DraftID: d.ID}, nil
}

func (m *MultiBackend) DiscardDraft(ctx context.Context, ref string) error {
	d, err := m.store.GetDraft(ctx, ref)
	if err != nil {
		return err
	}
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
