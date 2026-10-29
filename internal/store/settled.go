package store

import (
	"context"
	"fmt"
	"time"
)

// A terminal status is settled when nothing may move the task back to a
// live one: the wrapper, runq itself or the scheduler reported it, or it was
// a guess (inferred, a submit failure, no source) that nothing corrected
// within SettleGrace. A provisional terminal can still revive on later
// evidence (a status.json read late, a scheduler that lists the job again).
//
// Settled is absorbing — UpdateTaskStatus refuses to move a settled task to
// a live status — so anything that destroys or freezes a version's data
// (clean, checkpoint clean, ingest's final mark) acts on settled versions
// only and needs no lock against status writers.
const SettleGrace = 24 * time.Hour

// settledSources are the provenances whose terminals are never corrected
// to a live status (see remote.acceptCandidate).
var settledSources = []string{"wrapper", "runq", "scheduler"}

// IsSettled applies the settled rule to one status; endedAt is when the
// task ended (else started, else was enqueued — a guess written without a
// finish time still settles once, never at once).
func IsSettled(status, source string, endedAt time.Time, now time.Time) bool {
	if !IsTerminalTaskStatus(status) {
		return false
	}
	for _, s := range settledSources {
		if source == s {
			return true
		}
	}
	return endedAt.Before(now.Add(-SettleGrace))
}

// Settled reports whether the task's terminal status is settled at now.
func (t TaskRow) Settled(now time.Time) bool {
	ended := t.EnqueuedAt
	switch {
	case t.FinishedAt != nil:
		ended = *t.FinishedAt
	case t.StartedAt != nil:
		ended = *t.StartedAt
	}
	return IsSettled(t.Status, t.StatusSource, ended, now)
}

// SettledSQL is the settled rule as a WHERE clause over tasks (one
// argument: the grace cutoff it returns).
func SettledSQL(now time.Time) (string, int64) {
	return fmt.Sprintf("(status IN ('success','failed','killed') AND (COALESCE(status_source,'') IN %s OR COALESCE(finished_at, started_at, enqueued_at, 0) < ?))",
		statusInSQL(settledSources)), now.Add(-SettleGrace).Unix()
}

// UnsettledJobIDs are the jobs with a terminal version not settled yet:
// a guessed outcome that reconciliation should keep checking.
func (s *Store) UnsettledJobIDs(ctx context.Context) (map[string]bool, error) {
	settled, cutoff := SettledSQL(time.Now())
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT job_id FROM tasks
		WHERE status IN ('success','failed','killed') AND NOT `+settled, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
