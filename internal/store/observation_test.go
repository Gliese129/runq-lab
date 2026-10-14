package store

import (
	"context"
	"testing"
	"time"
)

// Tasks settled before the observation ledger existed have a final ingest
// mark but no receipt; they must read as observed, not "never".
func TestJobObservationSettledTaskWithoutReceipt(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO projects(name, config_json) VALUES('p','{}')`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	finished := now.Add(-time.Hour).Truncate(time.Second)
	job := JobRow{ID: "j", ProjectName: "p", ConfigJSON: "{}", Status: "done", TotalTasks: 1, CreatedAt: now}
	task := TaskRow{ID: "t1", JobID: "j", ProjectName: "p", Command: "c", ParamsJSON: "{}", Status: "success", EnqueuedAt: now, FinishedAt: &finished}
	if err := st.InsertJobWithTasks(ctx, &job, []TaskRow{task}); err != nil {
		t.Fatal(err)
	}
	tasks, err := st.ListTasks(ctx, TaskFilter{JobID: "j"})
	if err != nil {
		t.Fatal(err)
	}

	if _, state, err := st.JobObservation(ctx, tasks); err != nil || state != "never" {
		t.Fatalf("before final ingest: state=%q err=%v, want never", state, err)
	}
	if err := st.SetFileIngestMark(ctx, "t1", IngestFileResults, FileIngestMark{Final: true}); err != nil {
		t.Fatal(err)
	}
	at, state, err := st.JobObservation(ctx, tasks)
	if err != nil {
		t.Fatal(err)
	}
	if state != "ok" || at == nil || *at != finished.Unix() {
		t.Fatalf("state=%q at=%v, want ok at finish time %d", state, at, finished.Unix())
	}
}
