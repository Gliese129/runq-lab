package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
}

// ErrAppendRaced means a cell changed between planning and the append
// (e.g. a failed task was retried meanwhile); planning again fixes it.
var ErrAppendRaced = errors.New("the job changed while appending; preview again")

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

// NextBatchSeq is the seq the next append to jobID gets.
func (s *Store) NextBatchSeq(ctx context.Context, jobID string) (int, error) {
	var seq int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM job_batches WHERE job_id = ?`, jobID).Scan(&seq)
	return seq, err
}

// AppendTasks adds a batch of tasks to an existing job in one transaction:
// the batch record, the task rows (stamped with the batch), the failed
// tasks they rerun (supersede: old task id → new task id), then the job's
// total_tasks and status from its live tasks. It returns the batch seq.
func (s *Store) AppendTasks(ctx context.Context, b JobBatch, tasks []TaskRow, supersede map[string]string) (int, error) {
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
	for old, repl := range supersede {
		res, err := tx.ExecContext(ctx, `UPDATE tasks SET superseded_by = ? WHERE id = ? AND job_id = ? AND status IN ('failed', 'killed') AND superseded_by IS NULL`, repl, old, b.JobID)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return 0, fmt.Errorf("task %s: %w", old, ErrAppendRaced)
		}
	}

	live, err := liveJobTasksTx(ctx, tx, b.JobID)
	if err != nil {
		return 0, err
	}
	next, err := ProjectJobStatus(live)
	if err != nil {
		return 0, err
	}
	var finished any // live work again: the job is no longer finished
	if IsTerminalJobStatus(next) {
		finished = time.Now().Unix()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET total_tasks = ?, status = ?, finished_at = ? WHERE id = ?`, len(live), next, finished, b.JobID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}
	return b.Seq, nil
}

func liveJobTasksTx(ctx context.Context, tx *sql.Tx, jobID string) ([]TaskRow, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM tasks WHERE job_id = ? AND superseded_by IS NULL`, allTaskColumns), jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskRow
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}
