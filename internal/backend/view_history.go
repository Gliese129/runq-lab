package backend

import (
	"context"

	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/workspace"
)

func (m *MultiBackend) SourceHistory(ctx context.Context, id, stream, by string, count int) (*workspace.ExactHistory, error) {
	if len(id) >= 2 && id[:2] == "cs" {
		lane, c, err := m.consumerLane(ctx, id)
		if err != nil {
			return nil, err
		}
		return workspace.QueryExactHistory(ctx, lane.backend.FS, c.Dir, stream, by, count)
	}
	row, err := m.store.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrNotFound
	}
	be, err := m.resolveTaskRow(row)
	if err != nil {
		return nil, err
	}
	lane, ok := be.(*SSHBackend)
	if !ok {
		return nil, ErrNotSupported
	}
	return workspace.QueryExactHistory(ctx, lane.backend.FS, row.TaskDir, stream, by, count)
}
func (m *MultiBackend) ViewTasks(ctx context.Context, specProject string, jobs []string) ([]store.TaskRow, error) {
	return m.store.ListTasksForJobs(ctx, jobs)
}

func (m *MultiBackend) SourceFramesAt(ctx context.Context, id, by string, ats []float64) (*workspace.ExactHistory, error) {
	if len(id) >= 2 && id[:2] == "cs" {
		lane, c, err := m.consumerLane(ctx, id)
		if err != nil {
			return nil, err
		}
		return workspace.QueryExactFramesAt(ctx, lane.backend.FS, c.Dir, by, ats)
	}
	row, err := m.store.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrNotFound
	}
	be, err := m.resolveTaskRow(row)
	if err != nil {
		return nil, err
	}
	lane, ok := be.(*SSHBackend)
	if !ok {
		return nil, ErrNotSupported
	}
	return workspace.QueryExactFramesAt(ctx, lane.backend.FS, row.TaskDir, by, ats)
}
