package store

import (
	"bytes"
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// RecordIdentity is canonical JSON, preserving scalar types and excluding step.
func RecordIdentity(r ResultRecordRow) (section, identity string, step *float64, axes, metrics map[string]any, err error) {
	if err = json.Unmarshal([]byte(r.AxesJSON), &axes); err != nil {
		return
	}
	if err = json.Unmarshal([]byte(r.MetricsJSON), &metrics); err != nil {
		return
	}
	if axes == nil {
		axes = map[string]any{}
	}
	if metrics == nil {
		metrics = map[string]any{}
	}
	if v, ok := axes["step"].(float64); ok {
		step = &v
	}
	id := make(map[string]any, len(axes))
	for k, v := range axes {
		if k != "step" {
			id[k] = v
		}
	}
	raw, _ := json.Marshal(id)
	identity = string(raw)
	section = r.Section
	if section == "" {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		_ = enc.Encode([][]string{recordKeys(id), recordKeys(metrics)})
		raw = []byte(strings.TrimSuffix(buf.String(), "\n"))
		section = fmt.Sprintf("auto-%x", sha1.Sum(raw))[:13]
	}
	return
}
func recordKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func upsertLatest(ctx context.Context, tx *sql.Tx, r ResultRecordRow) error {
	section, _, step, axes, metrics, err := RecordIdentity(r)
	if err != nil {
		return err
	}
	identityAxes := make(map[string]any)
	for k, v := range axes {
		if k != "step" {
			identityAxes[k] = v
		}
	}
	ak, mk := recordKeys(identityAxes), recordKeys(metrics)
	aJSON, _ := json.Marshal(ak)
	mJSON, _ := json.Marshal(mk)
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO record_schema(source_id,job_id,section,axes_json,metrics_json) VALUES(?,?,?,?,?)`, r.TaskID, r.JobID, section, string(aJSON), string(mJSON)); err != nil {
		return err
	}
	var frozenA, frozenM string
	if err = tx.QueryRowContext(ctx, `SELECT axes_json,metrics_json FROM record_schema WHERE source_id=? AND section=?`, r.TaskID, section).Scan(&frozenA, &frozenM); err != nil {
		return err
	}
	var fa, fm []string
	_ = json.Unmarshal([]byte(frozenA), &fa)
	_ = json.Unmarshal([]byte(frozenM), &fm)
	if !reflect.DeepEqual(fa, ak) || !reflect.DeepEqual(fm, mk) {
		if _, err = tx.ExecContext(ctx, `UPDATE record_schema SET warning='section keys changed; missing keys are null and extra keys ignored' WHERE source_id=? AND section=?`, r.TaskID, section); err != nil {
			return err
		}
	}
	id := map[string]any{}
	outMetrics := map[string]any{}
	for _, k := range fa {
		id[k] = axes[k]
	}
	for _, k := range fm {
		outMetrics[k] = metrics[k]
	}
	key, _ := json.Marshal(id)
	if v, ok := axes["step"]; ok {
		id["step"] = v
	}
	aJSON, _ = json.Marshal(id)
	mJSON, _ = json.Marshal(outMetrics)
	_, err = tx.ExecContext(ctx, `INSERT INTO record_latest(task_id,job_id,section,key_json,step,ts,axes_json,metrics_json) VALUES(?,?,?,?,?,?,?,?)
 ON CONFLICT(task_id,section,key_json) DO UPDATE SET step=excluded.step,ts=excluded.ts,axes_json=excluded.axes_json,metrics_json=excluded.metrics_json
 WHERE (excluded.step IS NOT NULL AND record_latest.step IS NOT NULL AND excluded.step>record_latest.step)
 OR ((excluded.step IS NULL OR record_latest.step IS NULL OR excluded.step=record_latest.step) AND excluded.ts>=record_latest.ts)`, r.TaskID, r.JobID, section, string(key), step, r.TS, string(aJSON), string(mJSON))
	return err
}

// RecordWarnings lists a job's record-schema warnings from its selected
// versions (a replaced version's warnings no longer apply).
func (s *Store) RecordWarnings(ctx context.Context, jobID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_id,section,warning FROM record_schema WHERE job_id=? AND warning!=''
 AND NOT EXISTS (SELECT 1 FROM tasks t WHERE t.job_id=record_schema.job_id AND t.superseded_by IS NOT NULL
  AND (record_schema.source_id=t.id OR record_schema.source_id LIKE t.id || '@%'))`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id, section, w string
		if err := rows.Scan(&id, &section, &w); err != nil {
			return nil, err
		}
		out = append(out, id+"/"+section+": "+w)
	}
	return out, rows.Err()
}
