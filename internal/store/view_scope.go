package store

import (
	"context"
	"encoding/json"

	"github.com/gliese129/runq-lab/internal/views"
)

// ViewScope is the one query context a saved view reads: its table, its
// curves and its animation frames all build on it, so their scopes cannot
// drift. It holds the experiments in scope (the spec's jobs, or the
// caller's ?job= override), each cell's selected version (a version with
// superseded_by set is history and stays out), and the spec whose filter,
// producer and section narrow what is read.
type ViewScope struct {
	Spec views.Spec
	Jobs []JobRow
	// Versions are the selected versions of each job in Jobs, by job id.
	Versions map[string][]ViewVersion
}

// ViewVersion is a cell's selected version with what a record needs to
// name it: params, cell, status, version number, pin.
type ViewVersion struct {
	TaskRow
	Params  map[string]any
	Version int
	Pinned  bool
}

// ViewSource is one record source of a version: the version's own
// results (Producer "") or a producer's results.<producer>.jsonl.
type ViewSource struct {
	ID       string
	Producer string
	Version  ViewVersion
}

// ViewScope resolves a view's scope; override (?job=) replaces the spec's
// jobs, exactly as the table reads them.
func (s *Store) ViewScope(ctx context.Context, spec views.Spec, override []string) (*ViewScope, error) {
	jobs, err := s.ViewJobs(ctx, spec, override)
	if err != nil {
		return nil, err
	}
	sc := &ViewScope{Spec: spec, Jobs: jobs, Versions: map[string][]ViewVersion{}}
	for _, j := range jobs {
		if sc.Versions[j.ID], err = s.viewVersions(ctx, j.ID); err != nil {
			return nil, err
		}
	}
	return sc, nil
}

// viewVersions lists a job's selected versions (superseded ones are history).
func (s *Store) viewVersions(ctx context.Context, jobID string) ([]ViewVersion, error) {
	tasks, err := s.ListTasks(ctx, TaskFilter{JobID: jobID, LiveOnly: true})
	if err != nil {
		return nil, err
	}
	picks, err := s.CellPicks(ctx, jobID)
	if err != nil {
		return nil, err
	}
	pinned := map[string]bool{}
	for _, id := range picks {
		pinned[id] = true
	}
	numbers := map[string]int{}
	if all, err := s.JobTasksInOrder(ctx, jobID); err == nil {
		for _, v := range all {
			numbers[v.ID] = v.Version
		}
	}
	out := make([]ViewVersion, 0, len(tasks))
	for _, t := range tasks {
		v := ViewVersion{TaskRow: t, Version: numbers[t.ID], Pinned: pinned[t.ID]}
		_ = json.Unmarshal([]byte(t.ParamsJSON), &v.Params)
		out = append(out, v)
	}
	return out, nil
}

// Keeps reports whether the view's filter keeps this version. Only the
// keys a version decides on its own count (param.<name>, cell); filters on
// result axes apply per record when the rows render.
func (sc *ViewScope) Keeps(v ViewVersion) bool {
	return sc.Spec.KeepsCell(v.Params, v.CellName())
}

// Shown lists the versions in scope that the filter keeps, job by job:
// what curves read.
func (sc *ViewScope) Shown() []ViewVersion {
	out := []ViewVersion{}
	for _, j := range sc.Jobs {
		for _, v := range sc.Versions[j.ID] {
			if sc.Keeps(v) {
				out = append(out, v)
			}
		}
	}
	return out
}

// ViewSources lists the record sources of the versions the filter keeps,
// limited to the spec's producer when it names one: what animation frames
// read.
func (s *Store) ViewSources(ctx context.Context, sc *ViewScope) ([]ViewSource, error) {
	out := []ViewSource{}
	want := func(producer string) bool { return sc.Spec.Producer == nil || *sc.Spec.Producer == producer }
	for _, j := range sc.Jobs {
		byID := map[string]ViewVersion{}
		for _, v := range sc.Versions[j.ID] {
			if !sc.Keeps(v) {
				continue
			}
			byID[v.ID] = v
			if want("") {
				out = append(out, ViewSource{ID: v.ID, Version: v})
			}
		}
		// Results scripts wrote per version with runq.job() count too.
		producers, err := s.ProducerSources(ctx, j.ID)
		if err != nil {
			return nil, err
		}
		for _, id := range producers {
			taskID, producer := SplitSourceID(id)
			if v, ok := byID[taskID]; ok && want(producer) {
				out = append(out, ViewSource{ID: id, Producer: producer, Version: v})
			}
		}
	}
	return out, nil
}

// Record stamps a record of this source with its version's identity, as
// the table's records carry it.
func (src ViewSource) Record(section string, ts int64, axes, metrics map[string]any) views.Record {
	return src.Version.record(src.ID, src.Producer, section, ts, axes, metrics)
}

func (v ViewVersion) record(sourceID, producer, section string, ts int64, axes, metrics map[string]any) views.Record {
	return views.Record{SourceID: sourceID, Producer: producer, JobID: v.JobID, Section: section, TS: ts, Axes: axes, Metrics: metrics,
		Params: v.Params, Cell: v.CellName(), Status: v.Status, Version: v.Version, Pinned: v.Pinned}
}
