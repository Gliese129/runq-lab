package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// Cells and versions: a job's cells are its run names, and every task is one
// version of its cell. Each cell has one selected version — the one the
// researcher pinned, else the latest success, else the latest — and every
// other version carries superseded_by = the selected id. Reads that leave
// superseded tasks out (LiveOnly, results, views, job.json, counts) therefore
// see exactly the selected versions.

// CellKey is the cell a task belongs to. A task without a run name (pre-rc6)
// joins the cell of the task its superseded_by chain leads to, else it is a
// cell of its own.
func CellKey(task TaskRow, byID map[string]*TaskRow) string {
	seen := map[string]bool{}
	for t := &task; t != nil && !seen[t.ID]; {
		if t.RunName != "" {
			return "run:" + t.RunName
		}
		seen[t.ID] = true
		next, ok := byID[t.SupersededBy]
		if !ok {
			return "task:" + t.ID
		}
		t = next
	}
	return "task:" + task.ID
}

// CellName is the cell's name as views show it: the run name, or the task
// id for a pre-rc6 task without one.
func (t TaskRow) CellName() string {
	if t.RunName != "" {
		return t.RunName
	}
	return t.ID
}

// versionOrder sorts versions oldest first: by submission, then in the
// order given (callers list tasks in insertion order).
func versionOrder(v []TaskRow) {
	sort.SliceStable(v, func(i, j int) bool { return batchSeqOrFirst(v[i].BatchSeq) < batchSeqOrFirst(v[j].BatchSeq) })
}

// Cell is one cell of a job with its versions, oldest first.
type Cell struct {
	Key      string
	Name     string // the run name ("" for a pre-rc6 task)
	Versions []TaskRow
	Selected string // task id of the selected version
	Pinned   bool   // the researcher chose Selected
}

// GroupCells sorts tasks (in insertion order) into cells, in order of first
// appearance, and picks each cell's selected version. picks maps run name →
// pinned task id.
func GroupCells(tasks []TaskRow, picks map[string]string) []Cell {
	byID := make(map[string]*TaskRow, len(tasks))
	for i := range tasks {
		byID[tasks[i].ID] = &tasks[i]
	}
	ordered := append([]TaskRow(nil), tasks...)
	versionOrder(ordered)
	idx := map[string]int{}
	var cells []Cell
	for _, t := range ordered {
		key := CellKey(t, byID)
		i, ok := idx[key]
		if !ok {
			i = len(cells)
			idx[key] = i
			cells = append(cells, Cell{Key: key, Name: t.RunName})
		}
		if cells[i].Name == "" {
			cells[i].Name = t.RunName
		}
		cells[i].Versions = append(cells[i].Versions, t)
	}
	for i := range cells {
		c := &cells[i]
		if pick := picks[c.Name]; pick != "" && c.Name != "" {
			for _, v := range c.Versions {
				if v.ID == pick {
					c.Selected, c.Pinned = pick, true
				}
			}
		}
		if c.Selected != "" {
			continue
		}
		c.Selected = c.Versions[len(c.Versions)-1].ID
		for j := len(c.Versions) - 1; j >= 0; j-- {
			if c.Versions[j].Status == "success" {
				c.Selected = c.Versions[j].ID
				break
			}
		}
	}
	return cells
}

// CellPicks returns a job's pinned versions: run name → task id.
func (s *Store) CellPicks(ctx context.Context, jobID string) (map[string]string, error) {
	return cellPicks(ctx, s.db, jobID)
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func cellPicks(ctx context.Context, db querier, jobID string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT cell, task_id FROM cell_picks WHERE job_id = ?`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var cell, id string
		if err := rows.Scan(&cell, &id); err != nil {
			return nil, err
		}
		out[cell] = id
	}
	return out, rows.Err()
}

// PinVersion makes taskID the selected version of its cell; an empty taskID
// returns cell to latest. The job is refreshed in the same transaction.
func (s *Store) PinVersion(ctx context.Context, jobID, cell, taskID string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	if taskID == "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM cell_picks WHERE job_id = ? AND cell = ?`, jobID, cell); err != nil {
			return "", err
		}
	} else {
		var run sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT run_name FROM tasks WHERE id = ? AND job_id = ?`, taskID, jobID).Scan(&run)
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("task %s is not a version of job %s", taskID, jobID)
		}
		if err != nil {
			return "", err
		}
		if run.String != cell {
			return "", fmt.Errorf("task %s is a version of cell %q, not %q", taskID, run.String, cell)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO cell_picks (job_id, cell, task_id) VALUES (?, ?, ?)
			ON CONFLICT(job_id, cell) DO UPDATE SET task_id = excluded.task_id`, jobID, cell, taskID); err != nil {
			return "", err
		}
	}
	status, _, err := refreshJobTx(ctx, tx, jobID, false)
	if err != nil {
		return "", err
	}
	return status, tx.Commit()
}

// RefreshJob re-selects every cell's version, then writes the job's status
// and total_tasks (its cell count). keepStatus leaves the status alone (a
// paused job stays paused). It returns the status and all of the job's tasks.
func (s *Store) RefreshJob(ctx context.Context, jobID string, keepStatus bool) (string, []TaskRow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	status, tasks, err := refreshJobTx(ctx, tx, jobID, keepStatus)
	if err != nil {
		return "", nil, err
	}
	return status, tasks, tx.Commit()
}

func refreshJobTx(ctx context.Context, tx *sql.Tx, jobID string, keepStatus bool) (string, []TaskRow, error) {
	tasks, err := jobTasksTx(ctx, tx, jobID)
	if err != nil {
		return "", nil, err
	}
	picks, err := cellPicks(ctx, tx, jobID)
	if err != nil {
		return "", nil, err
	}
	cells := GroupCells(tasks, picks)
	want := make(map[string]string, len(tasks))
	for _, c := range cells {
		for _, v := range c.Versions {
			if v.ID != c.Selected {
				want[v.ID] = c.Selected
			}
		}
	}
	for i := range tasks {
		if tasks[i].SupersededBy == want[tasks[i].ID] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET superseded_by = ? WHERE id = ?`, nullIfEmpty(want[tasks[i].ID]), tasks[i].ID); err != nil {
			return "", nil, err
		}
		tasks[i].SupersededBy = want[tasks[i].ID]
	}

	var current string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, jobID).Scan(&current); err != nil {
		if err == sql.ErrNoRows {
			return "", nil, fmt.Errorf("job %q not found", jobID)
		}
		return "", nil, err
	}
	status := current
	if !keepStatus {
		if status, err = ProjectJobStatus(tasks); err != nil {
			return "", nil, err
		}
	}
	var finished any // live again: no longer finished
	if IsTerminalJobStatus(status) {
		finished = time.Now().Unix()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET total_tasks = ?, status = ?,
		finished_at = CASE WHEN ? IS NULL THEN NULL ELSE COALESCE(finished_at, ?) END WHERE id = ?`,
		len(cells), status, finished, finished, jobID); err != nil {
		return "", nil, err
	}
	return status, tasks, nil
}

func jobTasksTx(ctx context.Context, tx *sql.Tx, jobID string) ([]TaskRow, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM tasks WHERE job_id = ? ORDER BY rowid`, allTaskColumns), jobID)
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

// nextVersionTx hands out the next version number of a cell: its counter
// plus one. Unnamed (pre-rc6) tasks are their own cell: version 1.
func nextVersionTx(ctx context.Context, tx *sql.Tx, jobID, cell string) (int, error) {
	if cell == "" {
		return 1, nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO cell_versions (job_id, cell, last) VALUES (?, ?, 1)
		ON CONFLICT(job_id, cell) DO UPDATE SET last = last + 1`, jobID, cell); err != nil {
		return 0, err
	}
	var v int
	err := tx.QueryRowContext(ctx, `SELECT last FROM cell_versions WHERE job_id = ? AND cell = ?`, jobID, cell).Scan(&v)
	return v, err
}

// CellLastVersions returns the highest version number each of a job's cells
// has handed out (deleted versions included): run name → number.
func (s *Store) CellLastVersions(ctx context.Context, jobID string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT cell, last FROM cell_versions WHERE job_id = ?`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var cell string
		var last int
		if err := rows.Scan(&cell, &last); err != nil {
			return nil, err
		}
		out[cell] = last
	}
	return out, rows.Err()
}

// SyncJobAfterDeleteTx is what deleting some of a job's tasks (clean) leaves
// to do, in the caller's transaction: picks of deleted versions go (the cell
// returns to its default), an unnamed cell whose selected version went keeps
// its remaining versions together, and the job is refreshed like after an
// append or a pick (selected versions, superseded_by, status, total_tasks).
// A job with no tasks left is left alone (clean removes it). Version numbers
// and the cells' counters stay as they are.
func SyncJobAfterDeleteTx(ctx context.Context, tx *sql.Tx, jobID string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM cell_picks WHERE job_id = ? AND task_id NOT IN (SELECT id FROM tasks WHERE job_id = ?)`, jobID, jobID); err != nil {
		return fmt.Errorf("drop picks of deleted versions: %w", err)
	}
	tasks, err := jobTasksTx(ctx, tx, jobID)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return nil
	}
	alive := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		alive[t.ID] = true
	}
	// Unnamed versions find their cell through superseded_by; when it names
	// a deleted task, the first remaining one takes its place.
	heir := map[string]string{}
	for _, t := range tasks {
		gone := t.SupersededBy
		if gone == "" || alive[gone] {
			continue
		}
		next := ""
		if t.RunName == "" {
			if h, ok := heir[gone]; ok {
				next = h
			} else {
				heir[gone] = t.ID
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET superseded_by = ? WHERE id = ?`, nullIfEmpty(next), t.ID); err != nil {
			return err
		}
	}
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, jobID).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	_, _, err = refreshJobTx(ctx, tx, jobID, status == "paused")
	return err
}

// DeleteOrphanCellsSQL drops the picks and version counters of deleted jobs.
var DeleteOrphanCellsSQL = []string{
	`DELETE FROM cell_picks WHERE job_id NOT IN (SELECT id FROM jobs)`,
	`DELETE FROM cell_versions WHERE job_id NOT IN (SELECT id FROM jobs)`,
}

// backfillVersions numbers the versions of tasks stored before version
// numbers were persisted, in their current order within each cell, and
// starts each cell's counter there.
func (s *Store) backfillVersions(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT job_id FROM tasks WHERE version = 0`)
	if err != nil {
		return fmt.Errorf("list jobs to number: %w", err)
	}
	var jobs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, id)
	}
	rows.Close()
	if len(jobs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, jobID := range jobs {
		tasks, err := versionKeysTx(ctx, tx, jobID)
		if err != nil {
			return err
		}
		for _, c := range GroupCells(tasks, nil) {
			last := 0
			for _, v := range c.Versions {
				last = max(last, v.Version)
			}
			for _, v := range c.Versions {
				if v.Version != 0 {
					continue
				}
				last++
				if _, err := tx.ExecContext(ctx, `UPDATE tasks SET version = ? WHERE id = ?`, last, v.ID); err != nil {
					return fmt.Errorf("number version %s: %w", v.ID, err)
				}
			}
			if c.Name == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO cell_versions (job_id, cell, last) VALUES (?, ?, ?)
				ON CONFLICT(job_id, cell) DO UPDATE SET last = MAX(last, excluded.last)`, jobID, c.Name, last); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// versionKeysTx reads just what grouping a job's tasks into cells needs,
// so numbering old rows does not depend on their other columns.
func versionKeysTx(ctx context.Context, tx *sql.Tx, jobID string) ([]TaskRow, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, COALESCE(run_name, ''), COALESCE(batch_seq, 1), COALESCE(superseded_by, ''), COALESCE(status, ''), version
		FROM tasks WHERE job_id = ? ORDER BY rowid`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskRow
	for rows.Next() {
		var t TaskRow
		if err := rows.Scan(&t.ID, &t.RunName, &t.BatchSeq, &t.SupersededBy, &t.Status, &t.Version); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
