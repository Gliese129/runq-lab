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
		if err = addSchemaWarning(ctx, tx, r.TaskID, section, "section keys changed; missing keys are null and extra keys ignored"); err != nil {
			return err
		}
	}
	if err = checkKinds(ctx, tx, r.TaskID, section, metrics); err != nil {
		return err
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
	index := r.IndexJSON
	if index == "" {
		index = "[]"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO record_latest(task_id,job_id,section,key_json,step,ts,axes_json,metrics_json,index_json) VALUES(?,?,?,?,?,?,?,?,?)
 ON CONFLICT(task_id,section,key_json) DO UPDATE SET step=excluded.step,ts=excluded.ts,axes_json=excluded.axes_json,metrics_json=excluded.metrics_json,index_json=excluded.index_json
 WHERE (excluded.step IS NOT NULL AND record_latest.step IS NOT NULL AND excluded.step>record_latest.step)
 OR ((excluded.step IS NULL OR record_latest.step IS NULL OR excluded.step=record_latest.step) AND excluded.ts>=record_latest.ts)`, r.TaskID, r.JobID, section, string(key), step, r.TS, string(aJSON), string(mJSON), index)
	return err
}

// MetricKind is how a section's metric was first written: a plain number or
// a runq.stats estimate, with the estimate's unit and better direction.
type MetricKind struct {
	Kind   string `json:"kind"` // "number" | "est"
	Unit   string `json:"unit,omitempty"`
	Better string `json:"better,omitempty"`
}

func (k MetricKind) String() string {
	parts := []string{k.Kind}
	if k.Unit != "" {
		parts = append(parts, "unit "+k.Unit)
	}
	if k.Better != "" {
		parts = append(parts, "better "+k.Better)
	}
	return strings.Join(parts, ", ")
}

// KindOf classifies one metric value; ok is false for null.
func KindOf(v any) (MetricKind, bool) {
	switch x := v.(type) {
	case float64:
		return MetricKind{Kind: "number"}, true
	case map[string]any:
		if _, est := x["value"]; est {
			unit, _ := x["unit"].(string)
			better, _ := x["better"].(string)
			return MetricKind{Kind: "est", Unit: unit, Better: better}, true
		}
	}
	return MetricKind{}, false
}

// checkKinds freezes each metric's first kind in the section schema and
// warns when a later row writes it differently (like the key warning).
func checkKinds(ctx context.Context, tx *sql.Tx, sourceID, section string, metrics map[string]any) error {
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT kinds_json FROM record_schema WHERE source_id=? AND section=?`, sourceID, section).Scan(&raw); err != nil {
		return err
	}
	frozen := map[string]MetricKind{}
	_ = json.Unmarshal([]byte(raw), &frozen)
	changed := false
	for _, name := range recordKeys(metrics) {
		k, ok := KindOf(metrics[name])
		if !ok {
			continue
		}
		prev, seen := frozen[name]
		if !seen {
			frozen[name] = k
			changed = true
			continue
		}
		if prev != k {
			if err := addSchemaWarning(ctx, tx, sourceID, section, fmt.Sprintf("metric %q was %s, now %s", name, prev, k)); err != nil {
				return err
			}
		}
	}
	if !changed {
		return nil
	}
	out, _ := json.Marshal(frozen)
	_, err := tx.ExecContext(ctx, `UPDATE record_schema SET kinds_json=? WHERE source_id=? AND section=?`, string(out), sourceID, section)
	return err
}

// addSchemaWarning appends one warning to a section schema (each once).
func addSchemaWarning(ctx context.Context, tx *sql.Tx, sourceID, section, warning string) error {
	var cur string
	if err := tx.QueryRowContext(ctx, `SELECT warning FROM record_schema WHERE source_id=? AND section=?`, sourceID, section).Scan(&cur); err != nil {
		return err
	}
	for _, w := range strings.Split(cur, "; ") {
		if w == warning {
			return nil
		}
	}
	if cur != "" {
		warning = cur + "; " + warning
	}
	_, err := tx.ExecContext(ctx, `UPDATE record_schema SET warning=? WHERE source_id=? AND section=?`, warning, sourceID, section)
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
