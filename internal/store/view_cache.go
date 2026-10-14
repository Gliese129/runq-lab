package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/gliese129/runq-lab/internal/views"
)

func (s *Store) ViewSourceCache(ctx context.Context, project, view, source string) (views.Data, int64, error) {
	d := views.Data{State: "never"}
	var ran int64
	var ok int
	var raw sql.NullString
	var ts sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT ran_at,updated_at,ok,json,stderr_tail FROM view_source_cache WHERE project_name=? AND view_name=? AND source=?`, project, view, source).Scan(&ran, &ts, &ok, &raw, &d.Error)
	if err == sql.ErrNoRows {
		return d, 0, nil
	}
	if err != nil {
		return d, ran, err
	}
	if ts.Valid {
		d.UpdatedAt = &ts.Int64
	}
	if raw.Valid {
		if err = json.Unmarshal([]byte(raw.String), &d.Value); err != nil {
			return d, ran, err
		}
	}
	d.State = "ok"
	if ok == 0 {
		d.State = "error"
	}
	return d, ran, nil
}
func (s *Store) SaveViewSourceCache(ctx context.Context, project, view, source string, value any, runErr string) error {
	raw, _ := json.Marshal(value)
	now := time.Now().Unix()
	ok := runErr == ""
	_, err := s.db.ExecContext(ctx, `INSERT INTO view_source_cache(project_name,view_name,source,ran_at,updated_at,ok,json,stderr_tail) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(project_name,view_name,source) DO UPDATE SET ran_at=excluded.ran_at,ok=excluded.ok,stderr_tail=excluded.stderr_tail,json=CASE WHEN excluded.ok=1 THEN excluded.json ELSE view_source_cache.json END,updated_at=CASE WHEN excluded.ok=1 THEN excluded.updated_at ELSE view_source_cache.updated_at END`, project, view, source, now, func() any {
		if ok {
			return now
		}
		return nil
	}(), boolToInt(ok), func() any {
		if ok {
			return string(raw)
		}
		return nil
	}(), runErr)
	return err
}
