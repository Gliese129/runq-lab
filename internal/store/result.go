package store

import (
	"context"
	"database/sql"
	"strings"
)

// IngestFileResults identifies results.jsonl in file_ingest marks. (The
// "events" file id is gone with lifecycle-event ingest; old rows are inert.)
const IngestFileResults = "results"

// IngestFileLinks identifies links.json (SDK runq.link) in file_ingest marks.
const IngestFileLinks = "links"

// ResultRecordRow maps one line of results.jsonl:
//
//	{"ts":1700000000,"axes":{"model":"a","step":2000},"metrics":{"math":24.8}}
//
// axes/metrics stay opaque JSON here — key classification (identity/x/label)
// is the results endpoint's assembly concern, not storage's.
type ResultRecordRow struct {
	Section string

	TaskID      string
	JobID       string
	TS          int64
	AxesJSON    string // compact JSON object
	MetricsJSON string // compact JSON object
}

// FileIngestMark is one (task, file) incremental-ingest state — same
// semantics as IngestMark plus the results cap's overflow tally.
type FileIngestMark struct {
	Size    int64
	Offset  int64
	Final   bool
	Dropped int64
}

// GetFileIngestMark returns the (task, file) mark; zero value when absent.
func (s *Store) GetFileIngestMark(ctx context.Context, taskID, file string) (FileIngestMark, error) {
	var m FileIngestMark
	var final int
	err := s.db.QueryRowContext(ctx,
		`SELECT file_size, parsed_offset, final, dropped_count
		 FROM file_ingest WHERE task_id = ? AND file = ?`,
		taskID, file).Scan(&m.Size, &m.Offset, &final, &m.Dropped)
	if err == sql.ErrNoRows {
		return FileIngestMark{}, nil
	}
	if err != nil {
		return FileIngestMark{}, err
	}
	m.Final = final == 1
	return m, nil
}

// SetFileIngestMark upserts the (task, file) mark. Used by the missing-file
// freeze path; the reap's normal delta commits go through the Apply*Delta
// transactions instead.
func (s *Store) SetFileIngestMark(ctx context.Context, taskID, file string, m FileIngestMark) error {
	_, err := s.db.ExecContext(ctx, upsertFileIngestSQL,
		taskID, file, m.Size, m.Offset, boolToInt(m.Final), m.Dropped)
	return err
}

const upsertFileIngestSQL = `
	INSERT INTO file_ingest (task_id, file, file_size, parsed_offset, final, dropped_count)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(task_id, file) DO UPDATE SET
		file_size     = excluded.file_size,
		parsed_offset = excluded.parsed_offset,
		final         = excluded.final,
		dropped_count = excluded.dropped_count`

// ApplyResultsIngestDelta commits latest records and the read offset together.
func (s *Store) ApplyResultsIngestDelta(ctx context.Context, taskID string, rebuild bool,
	rows []ResultRecordRow, parsedTotal int, mark FileIngestMark) error {
	return s.applyResultsDelta(ctx, taskID, taskID, IngestFileResults, rebuild, rows, mark)
}

// ApplyProducerResultsDelta is ApplyResultsIngestDelta for
// <task_dir>/results.<producer>.jsonl: rows carry the producer's source id
// (ProducerSourceID) and the mark is the task's "results.<producer>" file.
func (s *Store) ApplyProducerResultsDelta(ctx context.Context, taskID, producer string, rebuild bool,
	rows []ResultRecordRow, mark FileIngestMark) error {
	return s.applyResultsDelta(ctx, ProducerSourceID(taskID, producer), taskID, ProducerResultsFile(producer), rebuild, rows, mark)
}

func (s *Store) applyResultsDelta(ctx context.Context, sourceID, taskID, file string, rebuild bool,
	rows []ResultRecordRow, mark FileIngestMark) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if rebuild {
		if _, err := tx.ExecContext(ctx, `DELETE FROM record_latest WHERE task_id=?`, sourceID); err != nil {
			return err
		}
	}
	for _, r := range rows {
		if err := upsertLatest(ctx, tx, r); err != nil {
			return err
		}
	}
	mark.Dropped = 0
	if _, err := tx.ExecContext(ctx, upsertFileIngestSQL,
		taskID, file, mark.Size, mark.Offset, boolToInt(mark.Final), mark.Dropped); err != nil {
		return err
	}
	return tx.Commit()
}

// ProducerSourceID is the record source of a task's results.<producer>.jsonl
// (a scoring script's results for that task): "<task_id>@<producer>".
// Records from the task itself keep the bare task id.
func ProducerSourceID(taskID, producer string) string { return taskID + "@" + producer }

// SplitSourceID undoes ProducerSourceID; producer is "" for a task's own
// records (and for ids of the removed consumers, cs…).
func SplitSourceID(id string) (taskID, producer string) {
	if i := strings.IndexByte(id, '@'); i >= 0 {
		return id[:i], id[i+1:]
	}
	return id, ""
}

// ProducerResultsFile is the file_ingest name of results.<producer>.jsonl.
func ProducerResultsFile(producer string) string { return IngestFileResults + "." + producer }

// SumResultsDropped totals the results-cap overflow across a job's tasks —
// the honest input for the results endpoint's skipped/truncated fields.
func (s *Store) SumResultsDropped(ctx context.Context, jobID string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(fi.dropped_count), 0)
		FROM file_ingest fi
		JOIN tasks t ON t.id = fi.task_id
		WHERE t.job_id = ? AND fi.file = ?`, jobID, IngestFileResults).Scan(&n)
	return n, err
}

// ListResultRecords returns a job's result records ordered by task then
// record order — the raw material for the results endpoint's columnar
// assembly (RQ2-1 c3).
func (s *Store) ListResultRecords(ctx context.Context, jobID string) ([]ResultRecordRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, job_id, ts, axes_json, metrics_json, section
		 FROM record_latest WHERE job_id = ?
		 ORDER BY task_id, section, key_json`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ResultRecordRow
	for rows.Next() {
		var r ResultRecordRow
		if err := rows.Scan(&r.TaskID, &r.JobID, &r.TS, &r.AxesJSON, &r.MetricsJSON, &r.Section); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ApplyLinksIngest replaces a task's links with the given set and advances
// the links.json mark, atomically.
func (s *Store) ApplyLinksIngest(ctx context.Context, taskID string, links map[string]string, mark FileIngestMark) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM task_links WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	for name, url := range links {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO task_links (task_id, name, url) VALUES (?, ?, ?)`, taskID, name, url); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, upsertFileIngestSQL,
		taskID, IngestFileLinks, mark.Size, mark.Offset, boolToInt(mark.Final), mark.Dropped); err != nil {
		return err
	}
	return tx.Commit()
}

// TaskLinks batch-fetches links for the given task IDs: task → name → url.
func (s *Store) TaskLinks(ctx context.Context, taskIDs []string) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	const chunk = 500
	for start := 0; start < len(taskIDs); start += chunk {
		ids := taskIDs[start:min(start+chunk, len(taskIDs))]
		ph := strings.Repeat("?,", len(ids)-1) + "?"
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx,
			`SELECT task_id, name, url FROM task_links WHERE task_id IN (`+ph+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, name, url string
			if err := rows.Scan(&id, &name, &url); err != nil {
				rows.Close()
				return nil, err
			}
			if out[id] == nil {
				out[id] = map[string]string{}
			}
			out[id][name] = url
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ProducerSources lists the producer record sources (ProducerSourceID) of a
// job's live tasks: every results.<producer>.jsonl ingest has seen.
func (s *Store) ProducerSources(ctx context.Context, jobID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT fi.task_id, fi.file FROM file_ingest fi JOIN tasks t ON t.id = fi.task_id
		WHERE t.job_id = ? AND t.superseded_by IS NULL AND fi.file LIKE 'results.%'
		ORDER BY fi.task_id, fi.file`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var taskID, file string
		if err := rows.Scan(&taskID, &file); err != nil {
			return nil, err
		}
		out = append(out, ProducerSourceID(taskID, strings.TrimPrefix(file, IngestFileResults+".")))
	}
	return out, rows.Err()
}
