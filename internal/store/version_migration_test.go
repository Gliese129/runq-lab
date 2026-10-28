package store

import (
	"context"
	"testing"
)

// Rows stored before versions were persisted are numbered in their
// current order within each cell, and the cell's counter starts there.
func TestBackfillVersions(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`INSERT INTO jobs (id, project_name, config_json, status, total_tasks, created_at) VALUES ('j', 'p', '{}', 'partial', 2, 1)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct{ id, run, superseded string }{
		{"a1", "A", "a2"}, {"b1", "B", ""}, {"a2", "A", ""}, {"x1", "", "x2"}, {"x2", "", ""},
	} {
		if _, err := s.db.Exec(`INSERT INTO tasks (id, job_id, project_name, command, params_json, status, run_name, superseded_by, enqueued_at)
			VALUES (?, 'j', 'p', 'true', '{}', 'success', NULLIF(?, ''), NULLIF(?, ''), 1)`, r.id, r.run, r.superseded); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"a1": 1, "a2": 2, "b1": 1, "x1": 1, "x2": 2}
	for id, v := range want {
		task, err := s.GetTask(ctx, id)
		if err != nil || task.Version != v {
			t.Errorf("%s version = %+v %v, want %d", id, task, err, v)
		}
	}
	last, err := s.CellLastVersions(ctx, "j")
	if err != nil || last["A"] != 2 || last["B"] != 1 || len(last) != 2 {
		t.Fatalf("counters = %v %v, want A:2 B:1", last, err)
	}
	if err := s.Migrate(); err != nil { // idempotent
		t.Fatal(err)
	}
	if task, _ := s.GetTask(ctx, "a2"); task.Version != 2 {
		t.Fatalf("a2 renumbered to %d", task.Version)
	}
}
