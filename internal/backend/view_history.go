package backend

import (
	"context"

	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/workspace"
)

// sourceLane resolves a record source (a task id, or a producer source
// "<task>@<producer>") to its lane and task dir; producer sources read their
// results.<producer>.jsonl.
func (m *MultiBackend) sourceLane(ctx context.Context, id string) (*SSHBackend, string, string, error) {
	taskID, producer := store.SplitSourceID(id)
	row, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, "", "", err
	}
	if row == nil {
		return nil, "", "", ErrNotFound
	}
	be, err := m.filesLane(row)
	if err != nil {
		return nil, "", "", err
	}
	lane, ok := be.(*SSHBackend)
	if !ok {
		return nil, "", "", ErrNotSupported
	}
	return lane, row.TaskDir, producer, nil
}

func (m *MultiBackend) SourceHistory(ctx context.Context, id, stream, by string, count int) (*workspace.ExactHistory, error) {
	lane, dir, producer, err := m.sourceLane(ctx, id)
	if err != nil {
		return nil, err
	}
	if producer != "" {
		if stream != "results" {
			return &workspace.ExactHistory{By: by}, nil // producers write results only
		}
		stream = workspace.ProducerResultsStream(producer)
	}
	return workspace.QueryExactHistory(ctx, lane.backend.FS, dir, stream, by, count)
}
func (m *MultiBackend) ViewTasks(ctx context.Context, specProject string, jobs []string) ([]store.TaskRow, error) {
	tasks, err := m.store.ListTasksForJobs(ctx, jobs)
	live := tasks[:0]
	for _, t := range tasks {
		if t.SupersededBy == "" { // a rerun replaced it
			live = append(live, t)
		}
	}
	return live, err
}

func (m *MultiBackend) SourceFramesAt(ctx context.Context, id, by string, ats []float64) (*workspace.ExactHistory, error) {
	lane, dir, producer, err := m.sourceLane(ctx, id)
	if err != nil {
		return nil, err
	}
	return workspace.QueryExactFramesAt(ctx, lane.backend.FS, dir, workspace.ProducerResultsStream(producer), by, ats)
}
