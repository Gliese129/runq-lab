package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// JobBatch is one submission into a job: seq 1 is the first submit, every
// `runq submit dr… --into jb…` adds the next.
type JobBatch struct {
	JobID       string    `json:"job_id"`
	Seq         int       `json:"seq"`
	SubmittedAt time.Time `json:"-"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Command     string    `json:"command"`
	Target      string    `json:"target"`
	TaskCount   int       `json:"task_count"`
	ConfigJSON  string    `json:"-"`
	DraftID     string    `json:"draft_id,omitempty"`
	// Expect is what an append was planned against: each existing
	// version's status (task id → status). AppendTasks refuses the append
	// when one changed since (a version revived, finished or was cleaned),
	// since its new / new_version / skipped verdicts may no longer hold.
	Expect map[string]string `json:"-"`
}

// ErrAppendStale means the experiment changed between planning an append
// and writing it.
var ErrAppendStale = errors.New("the experiment changed since this append was planned")

func batchSeqOrFirst(seq int) int {
	if seq <= 0 {
		return 1
	}
	return seq
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertBatch(ctx context.Context, db execer, b JobBatch) error {
	_, err := db.ExecContext(ctx, `INSERT INTO job_batches (job_id, seq, submitted_at, name, description, command, target, task_count, config_json, draft_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.JobID, b.Seq, b.SubmittedAt.Unix(), b.Name, b.Description, b.Command, targetOrDefault(b.Target), b.TaskCount, b.ConfigJSON, nullIfEmpty(b.DraftID))
	return err
}

// firstBatch is the seq-1 record of a freshly inserted job.
func firstBatch(j *JobRow) JobBatch {
	var cfg struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal([]byte(j.ConfigJSON), &cfg)
	return JobBatch{JobID: j.ID, Seq: 1, SubmittedAt: j.CreatedAt, Name: j.Name, Description: j.Description,
		Command: cfg.Command, Target: j.Target, TaskCount: j.TotalTasks, ConfigJSON: j.ConfigJSON, DraftID: j.DraftID}
}

// backfillFirstBatches gives every job without batches its seq-1 record.
func (s *Store) backfillFirstBatches(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO job_batches (job_id, seq, submitted_at, name, description, command, target, task_count, config_json, draft_id)
		SELECT id, 1, created_at, COALESCE(note, ''), COALESCE(description, ''), COALESCE(json_extract(config_json, '$.command'), ''),
		       COALESCE(target, 'local'), total_tasks, config_json, draft_id
		FROM jobs WHERE NOT EXISTS (SELECT 1 FROM job_batches b WHERE b.job_id = jobs.id)`)
	if err != nil {
		return fmt.Errorf("backfill job batches: %w", err)
	}
	return nil
}

// ListJobBatches returns a job's batches in order.
func (s *Store) ListJobBatches(ctx context.Context, jobID string) ([]JobBatch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT job_id, seq, submitted_at, name, description, command, target, task_count, config_json, draft_id
		FROM job_batches WHERE job_id = ? ORDER BY seq`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobBatch
	for rows.Next() {
		var b JobBatch
		var at int64
		var draft sql.NullString
		if err := rows.Scan(&b.JobID, &b.Seq, &at, &b.Name, &b.Description, &b.Command, &b.Target, &b.TaskCount, &b.ConfigJSON, &draft); err != nil {
			return nil, err
		}
		b.SubmittedAt, b.DraftID = time.Unix(at, 0), draft.String
		out = append(out, b)
	}
	return out, rows.Err()
}

// DraftBatch is the batch a draft was submitted as, or nil. It catches a
// submit whose job landed but whose draft was never marked submitted.
func (s *Store) DraftBatch(ctx context.Context, draftID string) (*JobBatch, error) {
	var b JobBatch
	var at int64
	err := s.db.QueryRowContext(ctx, `SELECT job_id, seq, submitted_at, task_count, config_json FROM job_batches WHERE draft_id = ? ORDER BY job_id, seq LIMIT 1`, draftID).
		Scan(&b.JobID, &b.Seq, &at, &b.TaskCount, &b.ConfigJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.SubmittedAt, b.DraftID = time.Unix(at, 0), draftID
	return &b, nil
}

// NextBatchSeq is the seq the next append to jobID gets.
func (s *Store) NextBatchSeq(ctx context.Context, jobID string) (int, error) {
	var seq int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM job_batches WHERE job_id = ?`, jobID).Scan(&seq)
	return seq, err
}

// AppendTasks adds a batch of tasks to an existing job in one transaction:
// the batch record, the task rows (stamped with the batch; a task whose run
// name the job already has is a new version of that cell), then the job is
// refreshed (selected versions, status, total_tasks). It returns the seq.
func (s *Store) AppendTasks(ctx context.Context, b JobBatch, tasks []TaskRow) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var status string
	var archived sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT status, archived_at FROM jobs WHERE id = ?`, b.JobID).Scan(&status, &archived); err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("job %q not found", b.JobID)
		}
		return 0, err
	}
	if archived.Valid {
		return 0, fmt.Errorf("job %s is archived; unarchive it before appending", b.JobID)
	}
	if status == "paused" {
		return 0, fmt.Errorf("job %s is paused; resume it before appending", b.JobID)
	}
	if err := checkAppendExpect(ctx, tx, b); err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM job_batches WHERE job_id = ?`, b.JobID).Scan(&b.Seq); err != nil {
		return 0, err
	}
	b.TaskCount = len(tasks)
	if err := insertBatch(ctx, tx, b); err != nil {
		return 0, fmt.Errorf("insert batch: %w", err)
	}
	for i := range tasks {
		tasks[i].BatchSeq = b.Seq
		if err := s.InsertTaskTx(ctx, tx, &tasks[i]); err != nil {
			return 0, fmt.Errorf("insert task %s: %w", tasks[i].ID, err)
		}
	}
	if _, _, err := refreshJobTx(ctx, tx, b.JobID, false); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}
	return b.Seq, nil
}

func checkAppendExpect(ctx context.Context, tx *sql.Tx, b JobBatch) error {
	if len(b.Expect) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, status, COALESCE(run_name, '') FROM tasks WHERE job_id = ?`, b.JobID)
	if err != nil {
		return err
	}
	defer rows.Close()
	now := map[string]string{}
	names := map[string]string{}
	for rows.Next() {
		var id, status, name string
		if err := rows.Scan(&id, &status, &name); err != nil {
			return err
		}
		now[id], names[id] = status, name
	}
	if err := rows.Err(); err != nil {
		return err
	}
	ids := make([]string, 0, len(b.Expect))
	for id := range b.Expect {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		status, ok := now[id]
		switch {
		case !ok:
			return fmt.Errorf("%w: version %s is gone; plan the append again", ErrAppendStale, id)
		case status != b.Expect[id]:
			return fmt.Errorf("%w: cell %q version %s is %s now (was %s); plan the append again", ErrAppendStale, names[id], id, status, b.Expect[id])
		}
	}
	return nil
}
