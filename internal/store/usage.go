package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// JobUsage is what a job's directory held when measured (see job_usage).
// TaskCount is the number of task directories the bytes are spread over.
type JobUsage struct {
	JobID      string
	Bytes      int64
	Files      int64
	TaskCount  int
	MeasuredAt time.Time
}

// UpsertJobUsage records a measurement, keeping the larger bytes/files of
// the old and new one (a job's footprint doesn't shrink when its
// checkpoints are cleaned).
func (s *Store) UpsertJobUsage(ctx context.Context, u JobUsage) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO job_usage (job_id, bytes, files, task_count, measured_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(job_id) DO UPDATE SET
		 bytes = MAX(job_usage.bytes, excluded.bytes),
		 files = MAX(job_usage.files, excluded.files),
		 task_count = MAX(job_usage.task_count, excluded.task_count),
		 measured_at = excluded.measured_at`,
		u.JobID, u.Bytes, u.Files, u.TaskCount, u.MeasuredAt.Unix())
	return err
}

// GetJobUsage returns a job's measurement, nil when never measured.
func (s *Store) GetJobUsage(ctx context.Context, jobID string) (*JobUsage, error) {
	var u JobUsage
	var at int64
	err := s.db.QueryRowContext(ctx, `SELECT job_id, bytes, files, task_count, measured_at FROM job_usage WHERE job_id = ?`, jobID).
		Scan(&u.JobID, &u.Bytes, &u.Files, &u.TaskCount, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.MeasuredAt = time.Unix(at, 0)
	return &u, nil
}

// TaskFootprint is the largest per-task usage among a project's measured
// jobs; bytes and files may come from different jobs (each names its own).
type TaskFootprint struct {
	BytesPerTask int64
	BytesJobID   string
	FilesPerTask int64
	FilesJobID   string
	Jobs         int // measured jobs considered
}

// ProjectTaskFootprint returns the project's largest per-task bytes and
// files, ok=false when none of its jobs has been measured.
func (s *Store) ProjectTaskFootprint(ctx context.Context, project string) (TaskFootprint, bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.job_id, u.bytes, u.files, u.task_count
		FROM job_usage u JOIN jobs j ON j.id = u.job_id
		WHERE j.project_name = ? AND u.task_count > 0`, project)
	if err != nil {
		return TaskFootprint{}, false, err
	}
	defer rows.Close()
	var fp TaskFootprint
	for rows.Next() {
		var id string
		var bytes, files int64
		var n int
		if err := rows.Scan(&id, &bytes, &files, &n); err != nil {
			return TaskFootprint{}, false, err
		}
		fp.Jobs++
		if b := bytes / int64(n); b > fp.BytesPerTask || fp.BytesJobID == "" {
			fp.BytesPerTask, fp.BytesJobID = b, id
		}
		if f := files / int64(n); f > fp.FilesPerTask || fp.FilesJobID == "" {
			fp.FilesPerTask, fp.FilesJobID = f, id
		}
	}
	return fp, fp.Jobs > 0, rows.Err()
}
