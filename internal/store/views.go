package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/gliese129/runq-lab/internal/views"
)

type SavedView struct {
	Spec       views.Spec        `json:"spec"`
	Templates  map[string]string `json:"templates"`
	SourcePath string            `json:"source_path"`
	UpdatedAt  int64             `json:"updated_at"`
}
type ViewSummary struct {
	Project    string `json:"project"`
	Name       string `json:"name"`
	Title      string `json:"title"`
	UpdatedAt  int64  `json:"updated_at"`
	SourcePath string `json:"source_path"`
}

func (s *Store) SaveView(ctx context.Context, v SavedView) error {
	if err := v.Spec.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(v.Spec)
	if err != nil {
		return err
	}
	tpl, err := json.Marshal(v.Templates)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldRaw string
	_ = tx.QueryRowContext(ctx, `SELECT spec_json FROM views WHERE project_name=? AND name=?`, v.Spec.Project, v.Spec.Name).Scan(&oldRaw)
	var old views.Spec
	_ = json.Unmarshal([]byte(oldRaw), &old)
	oldSources, _ := json.Marshal(old.Sources)
	newSources, _ := json.Marshal(v.Spec.Sources)
	if string(oldSources) != string(newSources) {
		if _, err = tx.ExecContext(ctx, `DELETE FROM view_source_cache WHERE project_name=? AND view_name=?`, v.Spec.Project, v.Spec.Name); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO views(project_name,name,spec_json,source_path,templates_json,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(project_name,name) DO UPDATE SET spec_json=excluded.spec_json,source_path=CASE WHEN excluded.source_path='' THEN views.source_path ELSE excluded.source_path END,templates_json=excluded.templates_json,updated_at=excluded.updated_at`, v.Spec.Project, v.Spec.Name, string(raw), v.SourcePath, string(tpl), time.Now().Unix())
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) GetView(ctx context.Context, project, name string) (*SavedView, error) {
	var v SavedView
	var raw, tpl string
	err := s.db.QueryRowContext(ctx, `SELECT spec_json,templates_json,source_path,updated_at FROM views WHERE project_name=? AND name=?`, project, name).Scan(&raw, &tpl, &v.SourcePath, &v.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(raw), &v.Spec); err != nil {
		return nil, err
	}
	err = json.Unmarshal([]byte(tpl), &v.Templates)
	return &v, err
}
func (s *Store) ListViews(ctx context.Context, project string) ([]ViewSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT project_name,name,spec_json,updated_at,source_path FROM views WHERE (?='' OR project_name=?) ORDER BY project_name,name`, project, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ViewSummary{}
	for rows.Next() {
		var v ViewSummary
		var raw string
		if err := rows.Scan(&v.Project, &v.Name, &raw, &v.UpdatedAt, &v.SourcePath); err != nil {
			return nil, err
		}
		var spec views.Spec
		if err = json.Unmarshal([]byte(raw), &spec); err != nil {
			return nil, err
		}
		v.Title = spec.Title
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) DeleteView(ctx context.Context, project, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM views WHERE project_name=? AND name=?`, project, name)
	return err
}
func (s *Store) ViewJobs(ctx context.Context, spec views.Spec, override []string) ([]JobRow, error) {
	ids := override
	explicit := len(override) > 0
	note := "*"
	last := 1
	if !explicit && spec.Jobs != nil {
		raw, _ := json.Marshal(spec.Jobs)
		if len(raw) > 0 && raw[0] == '[' {
			if err := json.Unmarshal(raw, &ids); err != nil {
				return nil, &views.Invalid{Errors: []string{"jobs must be IDs or {note,last}"}}
			}
			explicit = true
		} else {
			var q struct {
				Note string `json:"note"`
				Last int    `json:"last"`
			}
			if err := json.Unmarshal(raw, &q); err != nil || q.Last < 1 {
				return nil, &views.Invalid{Errors: []string{"jobs.last must be positive"}}
			}
			last = q.Last
			if q.Note != "" {
				note = q.Note
			}
		}
	}
	out := []JobRow{}
	seen := map[string]bool{}
	if explicit {
		for _, id := range ids {
			j, err := s.GetJob(ctx, id)
			if err != nil {
				return nil, err
			}
			if j == nil || j.ProjectName != spec.Project {
				return nil, &views.Invalid{Errors: []string{fmt.Sprintf("job %q is absent or belongs to another project", id)}}
			}
			if !seen[j.ID] {
				out = append(out, *j)
				seen[j.ID] = true
			}
		}
		return out, nil
	}
	all, err := s.ListJobs(ctx, spec.Project, "")
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].ID > all[j].ID
		}
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	for _, j := range all {
		ok, err := path.Match(note, j.Name)
		if err != nil {
			return nil, &views.Invalid{Errors: []string{"invalid jobs.note glob"}}
		}
		if j.ArchivedAt == nil && ok {
			out = append(out, j)
			if len(out) == last {
				break
			}
		}
	}
	return out, nil
}
func (s *Store) ViewInput(ctx context.Context, spec views.Spec, override []string) (views.Input, error) {
	in := views.Input{Extras: map[string]map[string]any{}}
	jobs, err := s.ViewJobs(ctx, spec, override)
	if err != nil {
		return in, err
	}
	tableJobs := map[string]bool{}
	for _, j := range jobs {
		tableJobs[j.ID] = true
	}
	for _, e := range spec.Extra {
		found := false
		for _, j := range jobs {
			if j.ID == e.Job {
				found = true
			}
		}
		if found {
			continue
		}
		j, err := s.GetJob(ctx, e.Job)
		if err != nil {
			return in, err
		}
		if j == nil || j.ProjectName != spec.Project {
			return in, &views.Invalid{Errors: []string{"extra references absent or foreign job " + e.Job}}
		}
		jobs = append(jobs, *j)
	}
	for _, j := range jobs {
		tasks, err := s.ListTasks(ctx, TaskFilter{JobID: j.ID, LiveOnly: true})
		if err != nil {
			return in, err
		}
		params := map[string]map[string]any{}
		cells := map[string]string{}
		versions := map[string]int{}
		picks, err := s.CellPicks(ctx, j.ID)
		if err != nil {
			return in, err
		}
		pinned := map[string]bool{}
		for _, id := range picks {
			pinned[id] = true
		}
		if all, err := s.JobTasksInOrder(ctx, j.ID); err == nil {
			for _, v := range all {
				versions[v.ID] = v.Version
			}
		}
		status := map[string]string{}
		source := views.Source{JobID: j.ID, Note: j.Name, Status: j.Status, Tasks: len(tasks), Attention: []map[string]any{}, ObservationState: "never"}
		source.ObservedAt, source.ObservationState, err = s.JobObservation(ctx, tasks)
		if err != nil {
			return in, err
		}
		for _, t := range tasks {
			var p map[string]any
			_ = json.Unmarshal([]byte(t.ParamsJSON), &p)
			params[t.ID] = p
			cells[t.ID] = t.CellName()
			status[t.ID] = t.Status
			obs, err := s.SourceObservation(ctx, t.ID)
			if err != nil {
				return in, err
			}
			if obs.LastHeartbeatAt != nil && (source.LastHeartbeatAt == nil || *obs.LastHeartbeatAt > *source.LastHeartbeatAt) {
				source.LastHeartbeatAt = obs.LastHeartbeatAt
			}
			if t.Status == "running" {
				source.Running++
			}
			if t.Status == "failed" || t.Status == "killed" || t.Status == "unknown" || t.Status == "lost" {
				source.Attention = append(source.Attention, map[string]any{"task_id": t.ID, "status": t.Status, "reason": t.FailureDetail})
			}
		}
		records, err := s.ListResultRecords(ctx, j.ID)
		if err != nil {
			return in, err
		}
		for _, r := range records {
			if source.LastDataAt == nil || r.TS > *source.LastDataAt {
				ts := r.TS
				source.LastDataAt = &ts
			}
			if tableJobs[j.ID] {
				taskID, producer := SplitSourceID(r.TaskID)
				v := views.Record{SourceID: r.TaskID, Producer: producer, JobID: r.JobID, Section: r.Section, TS: r.TS, Params: params[taskID], Cell: cells[taskID],
					Status: status[taskID], Version: versions[taskID], Pinned: pinned[taskID]}
				if err := json.Unmarshal([]byte(r.AxesJSON), &v.Axes); err != nil {
					return in, err
				}
				if err := json.Unmarshal([]byte(r.MetricsJSON), &v.Metrics); err != nil {
					return in, err
				}
				v.MetricOrder = ObjectKeys(r.MetricsJSON)
				if r.IndexJSON != "" {
					_ = json.Unmarshal([]byte(r.IndexJSON), &v.Index)
				}
				in.Records = append(in.Records, v)
			}
		}
		warnings, err := s.RecordWarnings(ctx, j.ID)
		if err != nil {
			return in, err
		}
		source.Warnings = warnings
		if len(records) > 50000 {
			source.Warnings = append(source.Warnings, "more than 50000 record identities; review axes cardinality")
		}
		if source.ObservationState != "ok" {
			source.Attention = append(source.Attention, map[string]any{"job_id": j.ID, "reason": "observation " + source.ObservationState})
		}
		in.Sources = append(in.Sources, source)
		vals := map[string]any{"status": j.Status}
		rows, err := s.db.QueryContext(ctx, `SELECT m.key,m.last,m.last_ts,m.task_id FROM metric_summary m JOIN tasks t ON t.id=m.task_id WHERE m.job_id=? AND t.superseded_by IS NULL UNION ALL SELECT key,last,last_ts,source_id FROM consumer_metrics WHERE job_id=? ORDER BY 3,4`, j.ID, j.ID) // selected versions only; consumer_metrics: removed consumers, read-only
		if err != nil {
			return in, err
		}
		per := map[string][]float64{}
		for rows.Next() {
			var k, id string
			var v float64
			var ts int64
			if err := rows.Scan(&k, &v, &ts, &id); err != nil {
				rows.Close()
				return in, err
			}
			vals["metric:"+k] = v
			per[k] = append(per[k], v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return in, err
		}
		for k, vs := range per {
			sum := 0.
			lo, hi := vs[0], vs[0]
			for _, v := range vs {
				sum += v
				lo = min(lo, v)
				hi = max(hi, v)
			}
			for agg, v := range map[string]float64{"sum": sum, "mean": sum / float64(len(vs)), "min": lo, "max": hi} {
				vals["metric:"+k+":"+agg] = v
			}
		}
		for _, e := range spec.Extra {
			if e.Job != j.ID {
				continue
			}
			ex, err := views.Parse(e.Value)
			if err != nil {
				return in, err
			}
			for _, f := range ex.Fields() {
				if (f != "status" && !strings.HasPrefix(f, "metric:")) || (len(per) > 0 && vals[f] == nil) {
					return in, &views.Invalid{Errors: []string{"extra allows status or metric:<key>[:sum|max|min|mean]"}}
				}
			}
		}
		in.Extras[j.ID] = vals
	}
	return in, nil
}
