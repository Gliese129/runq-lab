package backend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/store"
	"gopkg.in/yaml.v3"
)

// sweepRunName is the run name sweepYAML gives the row with these params.
func sweepRunName(t *testing.T, params string) string {
	t.Helper()
	var cfg job.JobConfig
	if err := yaml.Unmarshal([]byte(sweepYAML), &cfg); err != nil {
		t.Fatal(err)
	}
	rows, err := job.Rows(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	var want job.TaskParams
	_ = json.Unmarshal([]byte(params), &want)
	for _, r := range rows {
		if job.SameValue(r.Params["kl"], want["kl"]) && job.SameValue(r.Params["seed"], want["seed"]) {
			return r.Run
		}
	}
	t.Fatalf("no sweep row has %s", params)
	return ""
}

// seedJob stores a finished job with one task per status, on lane "a".
func seedJob(t *testing.T, st *store.Store, id string, statuses ...string) []store.TaskRow {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	if err := st.InsertJob(ctx, &store.JobRow{ID: id, ProjectName: "kl", Name: "kl sweep", ConfigJSON: "{}", Status: "partial", Target: "a", CreatedAt: now, TotalTasks: len(statuses)}); err != nil {
		t.Fatal(err)
	}
	var rows []store.TaskRow
	dir := t.TempDir()
	for i, s := range statuses {
		r := store.TaskRow{ID: id + "t" + string(rune('a'+i)), JobID: id, ProjectName: "kl", Command: "x", Target: "a",
			ParamsJSON: `{"kl":` + []string{"0", "0.1"}[i%2] + `,"lr":0.000001,"seed":` + []string{"1", "2"}[i/2] + `}`,
			Status:     s, EnqueuedAt: now, TaskDir: filepath.Join(dir, id+string(rune('a'+i)))}
		r.RunName = sweepRunName(t, r.ParamsJSON)
		if err := st.InsertTask(ctx, &r); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, r)
	}
	if _, err := st.DB().Exec(`UPDATE jobs SET finished_at = ? WHERE id = ?`, now.Unix(), id); err != nil {
		t.Fatal(err)
	}
	return rows
}

// A draft with --into takes its project and target from the job, shows
// what each row would do, and passes the canonical job id to the lane.
func TestDraftIntoJob(t *testing.T) {
	ctx := context.Background()
	m, lane, st := newDraftBackend(t)
	seedJob(t, st, "jb11112222", "success", "failed", "success")
	noProject := strings.Replace(sweepYAML, "project: kl\n", "", 1)

	res, err := m.CreateDraft(ctx, DraftRequest{JobYAML: noProject + "note: retry the failed seed\n", Into: "jb1111"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Draft.Project != "kl" || res.Draft.Target != "a" || res.Draft.Into != "jb11112222" || res.Draft.Note != "retry the failed seed" {
		t.Fatalf("draft = %+v", res.Draft)
	}
	a := res.Append
	if a == nil || a.New != 1 || a.Skipped != 2 || a.NewVersion != 1 || a.Batch != 2 {
		t.Fatalf("append = %+v", a)
	}
	if y, _ := m.DraftYAML(ctx, res.Draft.ID); !strings.Contains(string(y), "into: jb11112222") || !strings.Contains(string(y), "note: retry the failed seed") {
		t.Errorf("draft yaml lacks into: or note:\n%s", y)
	}

	// Another target is the submission's choice now; here the project does
	// not list "b", so the usual target rules refuse it.
	if _, err := m.CreateDraft(ctx, DraftRequest{JobYAML: noProject, Into: "jb1111", Target: "b"}); err == nil || !strings.Contains(err.Error(), `unknown target "b"`) {
		t.Errorf("other target: %v", err)
	}
	if _, err := m.CreateDraft(ctx, DraftRequest{JobYAML: sweepYAML, Rerun: []string{"base"}}); err == nil {
		t.Error("rerun without into must be refused")
	}
	if _, err := m.CreateDraft(ctx, DraftRequest{JobYAML: noProject, Into: "jb1111", RetiredRerunFailed: true}); err == nil || !strings.Contains(err.Error(), "new version by default") {
		t.Errorf("retired rerun_failed: %v", err)
	}

	seedJob(t, st, "jb99990000", "success")
	if _, err := m.SubmitDraft(ctx, res.Draft.ID, DraftSubmitOptions{Into: "jb9999"}); err == nil || !strings.Contains(err.Error(), "appends to jb11112222") {
		t.Errorf("conflicting --into: %v", err)
	}
	base := sweepRunName(t, `{"kl":0,"seed":1}`)
	if _, err := m.SubmitDraft(ctx, res.Draft.ID, DraftSubmitOptions{Rerun: []string{base}}); err != nil {
		t.Fatal(err)
	}
	if lane.received.Into != "jb11112222" || len(lane.received.Rerun) != 1 || lane.received.Rerun[0] != base {
		t.Fatalf("lane got into=%q rerun=%v", lane.received.Into, lane.received.Rerun)
	}
}

func TestGetJobAndTaskByPrefix(t *testing.T) {
	ctx := context.Background()
	m, _, st := newDraftBackend(t)
	rows := seedJob(t, st, "jbaaaa0001", "success")
	seedJob(t, st, "jbaaab0002", "success")
	if d, err := m.GetJob(ctx, "jbaaaa0001"[:9]); err != nil || d.Job.ID != "jbaaaa0001" || len(d.Batches) != 1 {
		t.Fatalf("prefix: %+v %v", d, err)
	}
	var amb *store.AmbiguousRefError
	if _, err := m.GetJob(ctx, "jbaaa"); !errors.As(err, &amb) || len(amb.IDs) != 2 {
		t.Fatalf("ambiguous: %v", err)
	}
	if _, err := m.GetJob(ctx, "jbzz"); !IsNotFound(err) {
		t.Fatalf("missing: %v", err)
	}
	if v, err := m.GetTask(ctx, rows[0].ID[:len(rows[0].ID)-1]); err != nil || v.ID != rows[0].ID {
		t.Fatalf("task prefix: %v", err)
	}
	if err := st.ObserveSource(ctx, rows[0].ID, nil, ptr64(1700000000), nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := m.GetTask(ctx, rows[0].ID); v.LastOutputAt == nil || *v.LastOutputAt != 1700000000 {
		t.Fatalf("last_output_at = %v", v.LastOutputAt)
	}
}

func ptr64(v int64) *int64 { return &v }

// Each submission carries its own note and overrides on the job detail:
// what a cell's later version changed and why.
func TestJobDetailSubmissionNote(t *testing.T) {
	ctx := context.Background()
	m, _, st := newDraftBackend(t)
	rows := seedJob(t, st, "jbnote0001", "failed")
	retry := store.TaskRow{ID: "tknote2", JobID: "jbnote0001", ProjectName: "kl", RunName: rows[0].RunName, Target: "a", Command: "x", ParamsJSON: rows[0].ParamsJSON, Status: "pending", EnqueuedAt: time.Now()}
	if _, err := st.AppendTasks(ctx, store.JobBatch{JobID: "jbnote0001", Name: "fix", Target: "a", SubmittedAt: time.Now(), DraftID: "drnote",
		ConfigJSON: `{"note":"OOM: timeout 2h → 4h","overrides":{"timeout":"4h"}}`}, []store.TaskRow{retry}); err != nil {
		t.Fatal(err)
	}
	d, err := m.GetJob(ctx, "jbnote0001")
	if err != nil || len(d.Batches) != 2 {
		t.Fatalf("detail: %+v %v", d, err)
	}
	b := d.Batches[1]
	if b.Note != "OOM: timeout 2h → 4h" || b.Overrides == nil || b.Overrides.Timeout == nil || *b.Overrides.Timeout != "4h" || b.DraftID != "drnote" {
		t.Fatalf("submission 2 = %+v", b)
	}
	if d.Batches[0].Note != "" || d.Batches[0].Overrides != nil {
		t.Fatalf("submission 1 = %+v", d.Batches[0])
	}
}

// A finished job's activity is read once; an append (the job reopens)
// reads it again.
func TestJobActivityCachedWhenFinished(t *testing.T) {
	ctx := context.Background()
	m, _, st := newDraftBackend(t)
	rows := seedJob(t, st, "jbactv0001", "success")
	if err := os.MkdirAll(rows[0].TaskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(rows[0].TaskDir, "activity.tsv")
	writeActivityTSV(t, path, 5, 3)
	first, err := m.JobActivity(ctx, "jbactv0001")
	if err != nil || len(first.Tasks[0].Points) == 0 {
		t.Fatalf("first read: %+v %v", first, err)
	}
	_ = os.Remove(path)
	if again, _ := m.JobActivity(ctx, "jbactv0001"); len(again.Tasks[0].Points) != len(first.Tasks[0].Points) {
		t.Fatal("finished job activity was read again")
	}
	if err := st.UpdateJobStatus(ctx, "jbactv0001", "running"); err != nil {
		t.Fatal(err)
	}
	if live, _ := m.JobActivity(ctx, "jbactv0001"); len(live.Tasks[0].Points) != 0 {
		t.Fatal("a reopened job served stale activity")
	}
}

func TestJobSummaryCountsLiveTasks(t *testing.T) {
	s := BuildJobSummary(store.JobRow{ID: "jb1"}, []store.TaskRow{
		{ID: "a", Status: "success"}, {ID: "b", Status: "failed", SupersededBy: "c"}, {ID: "c", Status: "running"}})
	if s.Tasks.Total != 2 || s.Tasks.Failed != 0 || s.Tasks.Running != 1 {
		t.Fatalf("counts = %+v", s.Tasks)
	}
}

// Export writes each cell's selected version; a row whose version ran
// another command than the file's says so in a comment.
func TestExportWritesSelectedVersions(t *testing.T) {
	ctx := context.Background()
	m, _, st := newDraftBackend(t)
	rows := seedJob(t, st, "jbexport1", "failed")
	if _, err := st.DB().Exec(`UPDATE job_batches SET command = 'python first.py', config_json = '{"command":"python first.py"}' WHERE job_id = 'jbexport1'`); err != nil {
		t.Fatal(err)
	}
	retry := store.TaskRow{ID: "tkretry", JobID: "jbexport1", ProjectName: "kl", RunName: rows[0].RunName, Target: "a", Command: "x", ParamsJSON: rows[0].ParamsJSON, Status: "success", EnqueuedAt: time.Now()}
	second := store.TaskRow{ID: "tksecond", JobID: "jbexport1", ProjectName: "kl", RunName: "second", Target: "a", Command: "x", ParamsJSON: `{"kl":0,"lr":0.000001,"seed":9}`, Status: "pending", EnqueuedAt: time.Now()}
	if _, err := st.AppendTasks(ctx, store.JobBatch{JobID: "jbexport1", Name: "more", Command: "python second.py", Target: "a", ConfigJSON: `{"command":"python second.py"}`, SubmittedAt: time.Now()},
		[]store.TaskRow{retry, second}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`UPDATE job_batches SET command = 'python first.py' WHERE job_id = 'jbexport1' AND seq = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTasks(ctx, store.JobBatch{JobID: "jbexport1", Name: "fix", Command: "python first.py", Target: "a", ConfigJSON: `{"command":"python first.py"}`, SubmittedAt: time.Now()}, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := m.ExportJob(ctx, "jbexport1")
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	if strings.Count(out, "- {cell:") != 2 || !strings.Contains(out, "command: python first.py") || !strings.Contains(out, "# command: python second.py") {
		t.Fatalf("export:\n%s", out)
	}
}
