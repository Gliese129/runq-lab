package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gliese129/runq-lab/internal/utils"
)

// Drafts (rc6): an editable, server-side plan that `runq submit dr…` freezes
// into a job. Content is the job config in expanded `tasks:` form; rev
// counts edits. Each project keeps at most MaxOpenDrafts open drafts.
const MaxOpenDrafts = 5

var (
	ErrDraftChanged   = errors.New("draft changed")
	ErrDraftSubmitted = errors.New("draft already submitted")
	ErrDraftNotFound  = errors.New("draft not found")
)

type DraftRow struct {
	ID         string
	Project    string // current project name (joined via the immutable id)
	ProjectID  string
	Rev        int
	Status     string // open | submitted
	JobID      string
	Target     string // resolved at creation; fixed for the draft's life
	ConfigJSON string // job.JobConfig in expanded tasks: form
	SourcePath string
	SourceSHA  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const draftColumns = `d.id, COALESCE(p.name, ''), d.project_id, d.rev, d.status, COALESCE(d.job_id, ''), d.target,
	d.config_json, d.source_path, d.source_sha, d.created_at, d.updated_at`

func scanDraft(sc interface{ Scan(...any) error }) (*DraftRow, error) {
	var d DraftRow
	var created, updated int64
	if err := sc.Scan(&d.ID, &d.Project, &d.ProjectID, &d.Rev, &d.Status, &d.JobID, &d.Target,
		&d.ConfigJSON, &d.SourcePath, &d.SourceSHA, &created, &updated); err != nil {
		return nil, err
	}
	d.CreatedAt, d.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
	return &d, nil
}

// CreateDraft inserts an open draft at rev 1 and discards the project's
// oldest open drafts beyond MaxOpenDrafts. Returns the discarded ids.
func (s *Store) CreateDraft(ctx context.Context, d *DraftRow) ([]string, error) {
	pid, err := s.projectID(ctx, d.Project)
	if err != nil {
		return nil, err
	}
	d.ProjectID = pid
	d.ID = "dr" + utils.GenerateID()
	d.Rev, d.Status = 1, "open"
	now := time.Now()
	d.CreatedAt, d.UpdatedAt = now, now
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO drafts (id, project_id, rev, status, target, config_json, source_path, source_sha, created_at, updated_at)
		VALUES (?, ?, 1, 'open', ?, ?, ?, ?, ?, ?)`,
		d.ID, pid, d.Target, d.ConfigJSON, d.SourcePath, d.SourceSHA, now.Unix(), now.UnixNano()); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM drafts WHERE project_id = ? AND status = 'open' ORDER BY updated_at DESC, id`, pid)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	var discarded []string
	if len(ids) > MaxOpenDrafts {
		discarded = ids[MaxOpenDrafts:]
		for _, id := range discarded {
			if _, err := tx.ExecContext(ctx, `DELETE FROM drafts WHERE id = ?`, id); err != nil {
				return nil, err
			}
		}
	}
	return discarded, tx.Commit()
}

// GetDraft finds a draft by id or unique id prefix.
func (s *Store) GetDraft(ctx context.Context, ref string) (*DraftRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+draftColumns+` FROM drafts d LEFT JOIN projects p ON p.id = d.project_id
		WHERE d.id = ? OR d.id LIKE ? ORDER BY d.id LIMIT 6`, ref, ref+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []*DraftRow
	for rows.Next() {
		d, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		if d.ID == ref {
			return d, nil
		}
		found = append(found, d)
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("draft %q: %w (drafts beyond the newest %d per project are discarded)", ref, ErrDraftNotFound, MaxOpenDrafts)
	case 1:
		return found[0], nil
	}
	var ids []string
	for _, d := range found {
		ids = append(ids, d.ID)
	}
	return nil, fmt.Errorf("draft %q is ambiguous: %s", ref, strings.Join(ids, ", "))
}

// ListDrafts returns open drafts, newest first; project "" = all projects.
func (s *Store) ListDrafts(ctx context.Context, project string) ([]DraftRow, error) {
	q := `SELECT ` + draftColumns + ` FROM drafts d LEFT JOIN projects p ON p.id = d.project_id WHERE d.status = 'open'`
	var args []any
	if project != "" {
		q += ` AND p.name = ?`
		args = append(args, project)
	}
	q += ` ORDER BY d.updated_at DESC, d.id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DraftRow
	for rows.Next() {
		d, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// CountOpenDrafts is the cheap count `runq ps` shows under the job table.
func (s *Store) CountOpenDrafts(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM drafts WHERE status = 'open'`).Scan(&n)
	return n, err
}

// UpdateDraft replaces an open draft's content when rev matches, bumping
// rev. A mismatch is ErrDraftChanged; a submitted draft ErrDraftSubmitted.
func (s *Store) UpdateDraft(ctx context.Context, id string, rev int, configJSON string) (int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE drafts SET config_json = ?, rev = rev + 1, updated_at = ? WHERE id = ? AND rev = ? AND status = 'open'`,
		configJSON, time.Now().UnixNano(), id, rev)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return rev + 1, nil
	}
	return 0, s.draftConflict(ctx, id)
}

func (s *Store) draftConflict(ctx context.Context, id string) error {
	var status string
	var cur int
	var job sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT status, rev, job_id FROM drafts WHERE id = ?`, id).Scan(&status, &cur, &job)
	switch {
	case err == sql.ErrNoRows:
		return fmt.Errorf("draft %s: %w", id, ErrDraftNotFound)
	case err != nil:
		return err
	case status == "submitted":
		return fmt.Errorf("%w as %s", ErrDraftSubmitted, job.String)
	}
	return fmt.Errorf("%w (now rev %d)", ErrDraftChanged, cur)
}

// MarkDraftSubmitted freezes an open draft and records its job.
func (s *Store) MarkDraftSubmitted(ctx context.Context, id, jobID string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE drafts SET status = 'submitted', job_id = ?, updated_at = ? WHERE id = ? AND status = 'open'`,
		jobID, time.Now().UnixNano(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.draftConflict(ctx, id)
	}
	return nil
}

// DiscardDraft deletes an open draft.
func (s *Store) DiscardDraft(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM drafts WHERE id = ? AND status = 'open'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.draftConflict(ctx, id)
	}
	return nil
}
