package store

import "context"

// CurvePins lists a project's pinned curve keys in pin order.
func (s *Store) CurvePins(ctx context.Context, project string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key FROM curve_pins WHERE project_name=? ORDER BY position, key`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// SetCurvePins replaces a project's pinned curves with keys, in order.
func (s *Store) SetCurvePins(ctx context.Context, project string, keys []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM curve_pins WHERE project_name=?`, project); err != nil {
		return err
	}
	seen := map[string]bool{}
	for i, k := range keys {
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO curve_pins(project_name,key,position) VALUES(?,?,?)`, project, k, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}
