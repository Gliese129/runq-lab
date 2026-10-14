package store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func seedRecordJob(t *testing.T) (*Store, context.Context) {
	t.Helper()
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO projects(name, config_json) VALUES('p','{}')`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	job := JobRow{ID: "j", ProjectName: "p", ConfigJSON: "{}", Status: "running", TotalTasks: 1, CreatedAt: now}
	task := TaskRow{ID: "t1", JobID: "j", ProjectName: "p", Command: "c", ParamsJSON: "{}", Status: "running", EnqueuedAt: now}
	if err := st.InsertJobWithTasks(ctx, &job, []TaskRow{task}); err != nil {
		t.Fatal(err)
	}
	return st, ctx
}

func rec(ts int64, axes, metrics string) ResultRecordRow {
	return ResultRecordRow{Section: "val", TaskID: "t1", JobID: "j", TS: ts, AxesJSON: axes, MetricsJSON: metrics}
}

// A periodic `if step % 20 == 0: runq.record(...)` keeps one row per
// identity no matter how many steps it writes — the old 10,000-record cap
// can no longer freeze the view.
func TestRecordLatestKeepsOneRowPerIdentity(t *testing.T) {
	st, ctx := seedRecordJob(t)
	var rows []ResultRecordRow
	for step := 0; step <= 12000; step += 1 {
		rows = append(rows, rec(int64(1000+step), fmt.Sprintf(`{"split":"val","step":%d}`, step), fmt.Sprintf(`{"acc":%d}`, step)))
	}
	if err := st.ApplyResultsIngestDelta(ctx, "t1", false, rows, len(rows), FileIngestMark{}); err != nil {
		t.Fatal(err)
	}
	got, err := st.ListResultRecords(ctx, "j")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1 (step is not identity)", len(got))
	}
	var m map[string]float64
	_ = json.Unmarshal([]byte(got[0].MetricsJSON), &m)
	if m["acc"] != 12000 {
		t.Fatalf("latest acc = %v, want 12000", m["acc"])
	}
}

// A larger step wins even when it arrives with an older timestamp; a
// smaller step never overwrites. Keys are frozen by the first record.
func TestRecordLatestStepOrderAndFrozenSchema(t *testing.T) {
	st, ctx := seedRecordJob(t)
	apply := func(r ResultRecordRow) {
		t.Helper()
		if err := st.ApplyResultsIngestDelta(ctx, "t1", false, []ResultRecordRow{r}, 1, FileIngestMark{}); err != nil {
			t.Fatal(err)
		}
	}
	apply(rec(100, `{"split":"val","step":20}`, `{"acc":1,"loss":5}`))
	apply(rec(90, `{"split":"val","step":40}`, `{"acc":2,"loss":4}`))   // later step, older ts → wins
	apply(rec(200, `{"split":"val","step":30}`, `{"acc":9,"loss":9}`))  // earlier step → ignored
	apply(rec(300, `{"split":"val","step":60}`, `{"acc":3,"extra":7}`)) // schema drift
	got, err := st.ListResultRecords(ctx, "j")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1", len(got))
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(got[0].MetricsJSON), &m)
	if m["acc"] != 3.0 || m["loss"] != nil || len(m) != 2 {
		t.Fatalf("metrics = %v, want acc=3, loss=null and no extra key", m)
	}
	warnings, err := st.RecordWarnings(ctx, "j")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) == 0 {
		t.Fatal("schema drift should surface a warning")
	}
}
