package store

import (
	"context"
	"database/sql"
	"time"
)

type Observation struct {
	ObservedAt      *int64   `json:"observed_at"`
	Step            *float64 `json:"step"`
	LastOutputAt    *int64   `json:"last_output_at"`
	LastHeartbeatAt *int64   `json:"last_heartbeat_at"`
}

func (s *Store) ObserveSource(ctx context.Context, id string, step *float64, output, heartbeat *int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO source_observations(source_id,observed_at,step,last_output_at,last_heartbeat_at) VALUES(?,?,?,?,?) ON CONFLICT(source_id) DO UPDATE SET observed_at=excluded.observed_at,step=COALESCE(excluded.step,source_observations.step),last_output_at=COALESCE(excluded.last_output_at,source_observations.last_output_at),last_heartbeat_at=COALESCE(excluded.last_heartbeat_at,source_observations.last_heartbeat_at)`, id, time.Now().Unix(), step, output, heartbeat)
	return err
}
func (s *Store) SourceObservation(ctx context.Context, id string) (Observation, error) {
	o := Observation{}
	err := s.db.QueryRowContext(ctx, `SELECT observed_at,step,last_output_at,last_heartbeat_at FROM source_observations WHERE source_id=?`, id).Scan(&o.ObservedAt, &o.Step, &o.LastOutputAt, &o.LastHeartbeatAt)
	if err == sql.ErrNoRows {
		err = nil
	}
	return o, err
}

// JobObservation combines the owning generations' factual observation receipts.
func (s *Store) JobObservation(ctx context.Context, tasks []TaskRow) (*int64, string, error) {
	var at *int64
	unknown, okCount := 0, 0
	state := "ok"
	for _, t := range tasks {
		o, err := s.SourceObservation(ctx, t.ID)
		if err != nil {
			return nil, "", err
		}
		if IsTerminalTaskStatus(t.Status) {
			mark, err := s.GetFileIngestMark(ctx, t.ID, IngestFileResults)
			if err != nil {
				return nil, "", err
			}
			if mark.Final && o.ObservedAt != nil {
				okCount++
				if at == nil || *o.ObservedAt < *at {
					at = o.ObservedAt
				}
				continue
			}
		}
		resource := "tasks"
		if t.TargetGeneration != "" {
			resource += ":" + t.TargetGeneration
		}
		sync, err := s.GetSyncState(ctx, t.Target, resource)
		if err != nil {
			return nil, "", err
		}
		if sync == nil || sync.LastSuccess == 0 || o.ObservedAt == nil {
			unknown++
			continue
		}
		ts := min(sync.LastSuccess, *o.ObservedAt)
		okCount++
		if at == nil || ts < *at {
			v := ts
			at = &v
		}
		if sync.LastError != "" || time.Now().Unix()-ts > 300 {
			state = "unconfirmed"
		}
	}
	if unknown > 0 {
		at = nil
		if okCount == 0 {
			state = "never"
		} else {
			state = "unconfirmed"
		}
	}
	if len(tasks) == 0 {
		state = "never"
	}
	return at, state, nil
}

func IsTerminalTaskStatus(s string) bool { return s == "success" || s == "failed" || s == "killed" }
