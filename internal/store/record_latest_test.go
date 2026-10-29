package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// An est keeps its object and the row its index; a metric whose kind,
// unit or better changes later warns, like a key change.
func TestRecordLatestEstKindsAndIndex(t *testing.T) {
	st, ctx := seedRecordJob(t)
	est := `{"pass":{"value":0.5,"stderr":0.04,"n":125,"show":["STDERR"],"unit":"pct","better":"higher"}}`
	r := rec(1, `{"k":4}`, est)
	r.IndexJSON = `["k"]`
	later := rec(2, `{"k":8}`, `{"pass":0.6}`)
	later.IndexJSON = `["k"]`
	if err := st.ApplyResultsIngestDelta(ctx, "t1", false, []ResultRecordRow{r, later}, 2, FileIngestMark{}); err != nil {
		t.Fatal(err)
	}
	got, err := st.ListResultRecords(ctx, "j")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].IndexJSON != `["k"]` || !strings.Contains(got[0].MetricsJSON, `"stderr":0.04`) {
		t.Fatalf("rows = %+v", got)
	}
	warnings, err := st.RecordWarnings(ctx, "j")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || warnings[0] != `t1/val: metric "pass" was est, unit pct, better higher, now number` {
		t.Fatalf("warnings = %q", warnings)
	}
}

func TestCurvePinsKeepOrderPerProject(t *testing.T) {
	st, ctx := seedRecordJob(t)
	if err := st.SetCurvePins(ctx, "p", []string{"train/reward", "eval/pass@4", "train/reward", ""}); err != nil {
		t.Fatal(err)
	}
	got, err := st.CurvePins(ctx, "p")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "train/reward,eval/pass@4" {
		t.Fatalf("pins = %v", got)
	}
	if other, _ := st.CurvePins(ctx, "q"); len(other) != 0 {
		t.Fatalf("another project's pins = %v", other)
	}
}

// A record's metrics keep the order they were written in; the section's
// key check does not care about order.
func TestRecordLatestKeepsMetricOrder(t *testing.T) {
	st, ctx := seedRecordJob(t)
	rows := []ResultRecordRow{
		rec(1, `{"split":"a"}`, `{"准确率":0.5,"交付率":{"value":0.4,"stderr":0.01},"b":1}`),
		rec(2, `{"split":"b"}`, `{"准确率":0.6,"b":2}`), // 交付率 left out: null, last
	}
	if err := st.ApplyResultsIngestDelta(ctx, "t1", false, rows, len(rows), FileIngestMark{}); err != nil {
		t.Fatal(err)
	}
	got, err := st.ListResultRecords(ctx, "j")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("rows = %d", len(got))
	}
	if k := strings.Join(ObjectKeys(got[0].MetricsJSON), ","); k != "准确率,交付率,b" {
		t.Fatalf("first record keys %s (%s)", k, got[0].MetricsJSON)
	}
	if k := strings.Join(ObjectKeys(got[1].MetricsJSON), ","); k != "准确率,b,交付率" {
		t.Fatalf("second record keys %s", k)
	}
	var warning string
	_ = st.DB().QueryRowContext(ctx, `SELECT warning FROM record_schema WHERE source_id='t1'`).Scan(&warning)
	if !strings.Contains(warning, "keys changed") {
		t.Fatalf("a left-out key still warns: %q", warning)
	}
	rows = []ResultRecordRow{rec(3, `{"split":"c"}`, `{"b":3,"交付率":0.1,"准确率":0.2}`)}
	st2, ctx2 := seedRecordJob(t)
	if err := st2.ApplyResultsIngestDelta(ctx2, "t1", false, append([]ResultRecordRow{rec(1, `{"split":"a"}`, `{"准确率":0.5,"交付率":0.4,"b":1}`)}, rows...), 2, FileIngestMark{}); err != nil {
		t.Fatal(err)
	}
	_ = st2.DB().QueryRowContext(ctx2, `SELECT warning FROM record_schema WHERE source_id='t1'`).Scan(&warning)
	if warning != "" {
		t.Fatalf("the same keys in another order are no change: %q", warning)
	}
}
