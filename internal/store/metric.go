package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// IngestMark is one task's incremental-ingest state (spec §8.1.4).
// Zero value = never ingested.
type IngestMark struct {
	Size   int64 // stat size at last pass
	Offset int64 // parsed up to here (complete lines only)
	Final  bool  // terminal pass done: skip this task forever
}

// GetIngestMark returns the task's mark; zero value when absent.
func (s *Store) GetIngestMark(ctx context.Context, taskID string) (IngestMark, error) {
	var m IngestMark
	var final int
	err := s.db.QueryRowContext(ctx,
		`SELECT file_size, parsed_offset, final FROM metrics_ingest WHERE task_id = ?`,
		taskID).Scan(&m.Size, &m.Offset, &final)
	if err == sql.ErrNoRows {
		return IngestMark{}, nil
	}
	if err != nil {
		return IngestMark{}, err
	}
	m.Final = final == 1
	return m, nil
}

// SetIngestMark upserts the task's mark.
func (s *Store) SetIngestMark(ctx context.Context, taskID string, m IngestMark) error {
	final := 0
	if m.Final {
		final = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO metrics_ingest (task_id, file_size, parsed_offset, final)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET
			file_size = excluded.file_size,
			parsed_offset = excluded.parsed_offset,
			final = excluded.final`,
		taskID, m.Size, m.Offset, final)
	return err
}

// unfreezeTaskIngestTx makes terminal SDK streams readable again without
// rewinding their offsets or deleting their projections. The SDK appends a
// retry to the same metrics/results/events files, so those streams are
// task-lifetime history. Resetting an offset to zero would re-import the old
// bytes (and deleting the projections first would make history disappear and
// later reappear). A retry therefore changes only the terminal freeze bit;
// newly appended bytes continue exactly where the previous attempt stopped.
func unfreezeTaskIngestTx(ctx context.Context, tx *sql.Tx, taskID string) error {
	if _, err := tx.ExecContext(ctx,
		`UPDATE metrics_ingest SET final = 0 WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE file_ingest SET final = 0 WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	return nil
}

// BeginTaskRetry atomically records durable pre-effect retry intent and
// unfreezes task-lifetime SDK streams. The caller must reset wrapper evidence
// before publishing status=pending; a crash in between is recovered from the
// submitting/status_source=retry row without launching user work.
func (s *Store) BeginTaskRetry(ctx context.Context, taskID string, currentRetry int, targetGeneration string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks SET
			status = 'submitting', status_source = 'retry',
			retry_count = ?, gpus = NULL, pid = NULL, start_time = NULL,
			started_at = NULL, finished_at = NULL, external_id = NULL,
			native_state = NULL, queue = NULL, failure_detail = NULL,
			target_generation = ?, kill_requested = 0
		WHERE id = ? AND retry_count = ? AND status IN ('failed', 'killed')`,
		currentRetry+1, targetGeneration, taskID, currentRetry)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("task %q changed before retry intent could be recorded", taskID)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE jobs SET
			status = CASE WHEN status = 'paused' THEN 'paused' ELSE 'running' END,
			finished_at = CASE WHEN status = 'paused' THEN finished_at ELSE NULL END
		WHERE id = (SELECT job_id FROM tasks WHERE id = ?)`, taskID); err != nil {
		return err
	}
	if err := unfreezeTaskIngestTx(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// MetricSummaryRow is one (task, key) streaming reduction.
type MetricSummaryRow struct {
	TaskID string
	JobID  string
	Key    string
	Min    float64
	Max    float64
	Last   float64
	LastTS int64
	Count  int64
}

// MergeMetricSummaries upserts delta reductions into metric_summary.
// min/max/count merge associatively; last wins by timestamp — so repeated
// incremental passes compose losslessly.
func (s *Store) MergeMetricSummaries(ctx context.Context, rows []MetricSummaryRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := mergeMetricSummariesTx(ctx, tx, rows); err != nil {
		return err
	}
	return tx.Commit()
}

func mergeMetricSummariesTx(ctx context.Context, tx *sql.Tx, rows []MetricSummaryRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO metric_summary (task_id, job_id, key, min, max, last, last_ts, count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id, key) DO UPDATE SET
			min     = MIN(metric_summary.min, excluded.min),
			max     = MAX(metric_summary.max, excluded.max),
			last    = CASE WHEN excluded.last_ts >= metric_summary.last_ts
			               THEN excluded.last ELSE metric_summary.last END,
			last_ts = MAX(metric_summary.last_ts, excluded.last_ts),
			count   = metric_summary.count + excluded.count`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.ExecContext(ctx, r.TaskID, r.JobID, r.Key, r.Min, r.Max, r.Last, r.LastTS, r.Count); err != nil {
			return err
		}
	}
	return nil
}

// ApplyIngestDelta commits one incremental-reap pass ATOMICALLY: optional
// rebuild delete, summary merges, and the ingest mark
// land in ONE transaction. This is the double-count defense — "counted"
// and "accounted" must be inseparable: a crash between merging a delta
// and advancing the mark would replay the delta and inflate count/sum
// (the exact disease that killed the markless full reap).
func (s *Store) ApplyIngestDelta(ctx context.Context, taskID string, rebuild bool,
	summaries []MetricSummaryRow, mark IngestMark) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if rebuild {
		if _, err := tx.ExecContext(ctx, `DELETE FROM metric_summary WHERE task_id = ?`, taskID); err != nil {
			return err
		}
	}
	if err := mergeMetricSummariesTx(ctx, tx, summaries); err != nil {
		return err
	}
	final := 0
	if mark.Final {
		final = 1
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO metrics_ingest (task_id, file_size, parsed_offset, final)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET
			file_size = excluded.file_size,
			parsed_offset = excluded.parsed_offset,
			final = excluded.final`,
		taskID, mark.Size, mark.Offset, final); err != nil {
		return err
	}
	return tx.Commit()
}

// ListMetricSummaries returns a job's summaries, optionally one key only.
func (s *Store) ListMetricSummaries(ctx context.Context, jobID, key string) ([]MetricSummaryRow, error) {
	query := `SELECT task_id, job_id, key, min, max, last, last_ts, count FROM metric_summary WHERE job_id = ?`
	args := []any{jobID}
	if key != "" {
		query += ` AND key = ?`
		args = append(args, key)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MetricSummaryRow
	for rows.Next() {
		var r MetricSummaryRow
		if err := rows.Scan(&r.TaskID, &r.JobID, &r.Key, &r.Min, &r.Max, &r.Last, &r.LastTS, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TaskCleanStat is the ledger-known size preview for clean's TUI: what
// deletion would free, WITHOUT touching the filesystem.
type TaskCleanStat struct {
	MetricsBytes int64
}

// TaskCleanStats batch-fetches clean previews for the given task IDs
// (chunked IN queries over ingest marks).
func (s *Store) TaskCleanStats(ctx context.Context, taskIDs []string) (map[string]TaskCleanStat, error) {
	out := make(map[string]TaskCleanStat, len(taskIDs))
	const chunk = 500
	for start := 0; start < len(taskIDs); start += chunk {
		ids := taskIDs[start:min(start+chunk, len(taskIDs))]
		ph := strings.Repeat("?,", len(ids)-1) + "?"
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}

		mrows, err := s.db.QueryContext(ctx,
			`SELECT task_id, file_size FROM metrics_ingest WHERE task_id IN (`+ph+`)`, args...)
		if err != nil {
			return nil, err
		}
		for mrows.Next() {
			var id string
			var size int64
			if err := mrows.Scan(&id, &size); err != nil {
				mrows.Close()
				return nil, err
			}
			st := out[id]
			st.MetricsBytes = size
			out[id] = st
		}
		mrows.Close()
		if err := mrows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// MetricKeys is the key-discovery query (spec §5.4 dual-mode): DISTINCT
// over summaries — O(tasks × keys) rows, tiny by construction.
func (s *Store) MetricKeys(ctx context.Context, jobID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT key FROM metric_summary WHERE job_id = ? ORDER BY key`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}
