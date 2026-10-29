package backend

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/views"
)

type jobFilesFixture struct {
	st      *store.Store
	lane    *SSHBackend
	jobDir  string
	taskDir string
}

// newJobFilesFixture: one settled job "job" with a live task "task" and a
// superseded task "old", on a local-FS lane.
func newJobFilesFixture(t *testing.T) jobFilesFixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	target := config.TargetConfig{Name: "local", Workspace: t.TempDir(), SubmitTemplate: "true", MaxInflight: 1}
	lane, err := NewSSHBackend(SSHBackendConfig{Target: target, Store: st, GlobalCfg: &config.GlobalConfig{}, FS: rfs.NewLocalFS()})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.InsertJob(ctx, &store.JobRow{
		ID: "job", ProjectName: "project", Name: "sweep", ConfigJSON: "{}", Status: "done",
		TotalTasks: 1, Target: target.Name, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	jobDir := filepath.Join(target.Workspace, "job")
	started, finished := time.Unix(100, 0), time.Unix(200, 0)
	for _, id := range []string{"old", "task"} {
		dir := filepath.Join(jobDir, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		status := "success"
		if id == "old" {
			status = "failed"
		}
		if err := st.InsertTask(ctx, &store.TaskRow{
			ID: id, RunName: "R1", JobID: "job", ProjectName: "project", Command: "true",
			ParamsJSON: `{"lr":0.1}`, Status: status, Target: target.Name, BatchSeq: 1,
			TargetGeneration: lane.Generation(), TaskDir: dir, EnqueuedAt: time.Now(),
			StartedAt: &started, FinishedAt: &finished,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.DB().ExecContext(ctx, `UPDATE tasks SET superseded_by = 'task' WHERE id = 'old'`); err != nil {
		t.Fatal(err)
	}
	return jobFilesFixture{st: st, lane: lane, jobDir: jobDir, taskDir: filepath.Join(jobDir, "task")}
}

func readSnapshot(t *testing.T, dir string) JobSnapshot {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "job.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snap JobSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	return snap
}

func TestJobFilesPublishJobJSON(t *testing.T) {
	ctx := context.Background()
	f := newJobFilesFixture(t)
	if err := f.lane.publishJobSnapshot(ctx, "job"); err != nil {
		t.Fatal(err)
	}
	snap := readSnapshot(t, f.jobDir)
	if snap.Version != 2 || snap.Job.ID != "job" || snap.Job.Name != "sweep" || snap.Job.Project != "project" || snap.Job.Target != "local" || snap.Job.Status != "done" {
		t.Fatalf("job header: %+v", snap.Job)
	}
	if len(snap.Tasks) != 2 || snap.Tasks[0].ID != "old" || snap.Tasks[0].Selected || snap.Tasks[0].Version != 1 || !snap.Tasks[1].Selected || snap.Tasks[1].Version != 2 {
		t.Fatalf("every version, numbered, the selected one marked: %+v", snap.Tasks)
	}
	got := snap.Tasks[1]
	if got.ID != "task" || got.RunName != "R1" || got.Status != "success" || got.Params["lr"] != 0.1 ||
		got.TaskDir != f.taskDir || got.OutputDir != filepath.Join(f.taskDir, "outputs") ||
		got.CheckpointDir != filepath.Join(f.taskDir, "checkpoints") || got.BatchSeq != 1 ||
		got.StartedAt == nil || *got.StartedAt != 100 || got.FinishedAt == nil || *got.FinishedAt != 200 {
		t.Fatalf("task entry: %+v", got)
	}
	if snap.UpdatedAt == 0 || snap.ObservationState == "" {
		t.Fatalf("clocks: %+v", snap)
	}
	if left, _ := filepath.Glob(filepath.Join(f.jobDir, "job.json.tmp.*")); len(left) != 0 {
		t.Fatalf("temp files left behind: %v", left)
	}

	// An unchanged job is not rewritten; a change is.
	if err := os.WriteFile(filepath.Join(f.jobDir, "job.json"), []byte("sentinel"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.lane.reconcileJobFiles(ctx, false, true)
	if raw, _ := os.ReadFile(filepath.Join(f.jobDir, "job.json")); string(raw) != "sentinel" {
		t.Fatalf("unchanged job rewritten: %s", raw)
	}
	if _, err := f.st.RenameJob(ctx, "job", "sweep v2"); err != nil {
		t.Fatal(err)
	}
	f.lane.reconcileJobFiles(ctx, false, true)
	if snap := readSnapshot(t, f.jobDir); snap.Job.Name != "sweep v2" {
		t.Fatalf("changed job not republished: %+v", snap.Job)
	}
}

func TestJobFilesSkipOldJobsUnlessForced(t *testing.T) {
	ctx := context.Background()
	f := newJobFilesFixture(t)
	if _, err := f.st.DB().ExecContext(ctx, `UPDATE jobs SET created_at = ?, finished_at = ? WHERE id = 'job'`,
		time.Now().Add(-60*24*time.Hour).Unix(), time.Now().Add(-59*24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	f.lane.reconcileJobFiles(ctx, false, true)
	if _, err := os.Stat(filepath.Join(f.jobDir, "job.json")); !os.IsNotExist(err) {
		t.Fatalf("background pass touched an old settled job: %v", err)
	}
	f.lane.reconcileJobFiles(ctx, true, true)
	if snap := readSnapshot(t, f.jobDir); snap.Job.ID != "job" {
		t.Fatalf("forced pass did not publish: %+v", snap)
	}
}

// A scoring script's results.<producer>.jsonl lands as its own record source,
// also after the task settled, and keeps landing as the file grows.
func TestJobFilesIngestProducerResults(t *testing.T) {
	ctx := context.Background()
	f := newJobFilesFixture(t)
	write := func(name, body string) {
		t.Helper()
		fh, err := os.OpenFile(filepath.Join(f.taskDir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fh.WriteString(body); err != nil {
			t.Fatal(err)
		}
		_ = fh.Close()
	}
	// The task settled long ago: its own marks are frozen.
	if err := f.st.SetFileIngestMark(ctx, "task", store.IngestFileResults, store.FileIngestMark{Final: true}); err != nil {
		t.Fatal(err)
	}
	write("results.score_math.jsonl", `{"ts":10,"section":"math","axes":{"split":"test","step":100},"metrics":{"acc":0.5}}`+"\n")
	write("results.bad name.jsonl", `{"ts":10,"axes":{},"metrics":{"x":1}}`+"\n")
	if err := os.WriteFile(filepath.Join(f.jobDir, "old", "results.score_math.jsonl"), []byte(`{"ts":9,"axes":{},"metrics":{"acc":0.1}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.lane.reconcileJobFiles(ctx, false, true)

	records, err := f.st.ListResultRecords(ctx, "job")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].TaskID != "task@score_math" || records[0].Section != "math" {
		t.Fatalf("records = %+v (want one task@score_math record; a replaced version's and invalid names left out)", records)
	}

	// The file grows (an unterminated tail waits); the latest per identity wins.
	write("results.score_math.jsonl", `{"ts":20,"section":"math","axes":{"split":"test","step":200},"metrics":{"acc":0.75}}`+"\n"+`{"ts":30,"section":"math",`)
	f.lane.reconcileJobFiles(ctx, false, true)
	in, err := f.st.ViewInput(ctx, views.Spec{Project: "project"}, []string{"job"})
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Records) != 1 {
		t.Fatalf("view records = %+v", in.Records)
	}
	r := in.Records[0]
	if r.SourceID != "task@score_math" || r.Producer != "score_math" || r.Metrics["acc"] != 0.75 || r.Params["lr"] != 0.1 {
		t.Fatalf("view record = %+v", r)
	}
	sources, err := f.st.ProducerSources(ctx, "job")
	if err != nil || len(sources) != 1 || sources[0] != "task@score_math" {
		t.Fatalf("producer sources = %v, %v", sources, err)
	}
}

func newJobFilesLane(t *testing.T, st *store.Store, name, workspace string) *SSHBackend {
	t.Helper()
	target := config.TargetConfig{Name: name, Workspace: workspace, SubmitTemplate: "true", MaxInflight: 1}
	lane, err := NewSSHBackend(SSHBackendConfig{Target: target, Store: st, GlobalCfg: &config.GlobalConfig{}, FS: rfs.NewLocalFS()})
	if err != nil {
		t.Fatal(err)
	}
	return lane
}

// A version without a recorded location falls back to generation
// ownership: the active lane neither publishes job.json for a retiring
// generation's version nor ingests its producer files; the retiring lane
// does both, into its own copy.
func TestJobFilesFollowLaneGeneration(t *testing.T) {
	ctx := context.Background()
	f := newJobFilesFixture(t)
	retiring := newJobFilesLane(t, f.st, "local", t.TempDir())
	retiring.MarkRetiring()
	if retiring.Generation() == f.lane.Generation() {
		t.Fatal("fixture: generations must differ")
	}
	if _, err := f.st.DB().ExecContext(ctx, `UPDATE tasks SET target_generation = ?`, retiring.Generation()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.lane.GetTask(ctx, "task"); err == nil {
		t.Fatal("active lane point read should refuse a retiring generation's task")
	}
	if err := os.WriteFile(filepath.Join(f.taskDir, "results.scorer.jsonl"),
		[]byte(`{"ts":1,"section":"eval","axes":{},"metrics":{"acc":0.9}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	f.lane.reconcileJobFiles(ctx, true, true)
	if _, err := os.Stat(filepath.Join(f.jobDir, "job.json")); !os.IsNotExist(err) {
		t.Fatalf("active lane published job.json for another generation's versions: %v", err)
	}
	if rows, err := f.st.ListResultRecords(ctx, "job"); err != nil || len(rows) != 0 {
		t.Fatalf("active lane ingested another generation's producer results: %+v, %v", rows, err)
	}

	retiring.reconcileJobFiles(ctx, true, true)
	snap := readSnapshot(t, f.jobDir)
	if snap.PublishedFor != (JobSnapshotCopy{Target: "local", Generation: retiring.Generation(), Location: retiring.scope.Location}) {
		t.Fatalf("published_for = %+v", snap.PublishedFor)
	}
	for _, task := range snap.Tasks {
		if task.Generation != retiring.Generation() {
			t.Fatalf("task generation = %+v", task)
		}
	}
	if rows, err := f.st.ListResultRecords(ctx, "job"); err != nil || len(rows) != 1 || rows[0].TaskID != "task@scorer" {
		t.Fatalf("retiring lane did not ingest its version's producer results: %+v, %v", rows, err)
	}
}

// An experiment on two targets has a job.json copy on each; each copy is
// rewritten only when its own content changes, not every pass.
func TestJobFilesCopyPerTarget(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a := newJobFilesLane(t, st, "a", t.TempDir())
	b := newJobFilesLane(t, st, "b", t.TempDir())
	if err := st.InsertJob(ctx, &store.JobRow{ID: "job", ProjectName: "project", Name: "sweep", ConfigJSON: "{}",
		Status: "done", TotalTasks: 2, Target: "a", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	dirs := map[string]string{}
	for _, lane := range []*SSHBackend{a, b} {
		name := lane.targetName
		dirs[name] = filepath.Join(lane.backend.Cfg.Workspace, "job")
		taskDir := filepath.Join(dirs[name], "tk"+name)
		if err := os.MkdirAll(taskDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := st.InsertTask(ctx, &store.TaskRow{ID: "tk" + name, RunName: "R" + name, JobID: "job", ProjectName: "project",
			Command: "true", ParamsJSON: "{}", Status: "success", Target: name, BatchSeq: 1,
			TargetGeneration: lane.Generation(), TaskDir: taskDir, EnqueuedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	pass := func() {
		a.reconcileJobFiles(ctx, true, false)
		b.reconcileJobFiles(ctx, true, false)
	}
	pass()
	for name, dir := range dirs {
		if snap := readSnapshot(t, dir); snap.PublishedFor.Target != name || len(snap.Tasks) != 2 {
			t.Fatalf("copy on %s: %+v", name, snap)
		}
		if err := os.WriteFile(filepath.Join(dir, "job.json"), []byte("sentinel"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pass()
	for name, dir := range dirs {
		if raw, _ := os.ReadFile(filepath.Join(dir, "job.json")); string(raw) != "sentinel" {
			t.Fatalf("unchanged copy on %s rewritten", name)
		}
	}
	if _, err := st.RenameJob(ctx, "job", "sweep v2"); err != nil {
		t.Fatal(err)
	}
	pass()
	for name, dir := range dirs {
		if snap := readSnapshot(t, dir); snap.Job.Name != "sweep v2" {
			t.Fatalf("changed copy on %s not republished: %+v", name, snap.Job)
		}
	}
}

// A config edit that moves no file (max_inflight) makes a new generation;
// the old one retires and stops scanning. A version on the same filesystem
// is still read: the active lane ingests a scorer's late results for it.
func TestJobFilesFollowLocationAcrossGenerations(t *testing.T) {
	ctx := context.Background()
	f := newJobFilesFixture(t)
	if _, err := f.st.DB().ExecContext(ctx, `UPDATE tasks SET target_generation = 'retired-gen', location = ?`, f.lane.scope.Location); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.taskDir, "results.scorer.jsonl"),
		[]byte(`{"ts":1,"section":"eval","axes":{},"metrics":{"acc":0.9}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.lane.reconcileJobFiles(ctx, true, true)
	if rows, err := f.st.ListResultRecords(ctx, "job"); err != nil || len(rows) != 1 {
		t.Fatalf("late scorer results of a retired generation's version: %+v, %v", rows, err)
	}
	if snap := readSnapshot(t, f.jobDir); snap.PublishedFor.Location != f.lane.scope.Location || len(snap.Tasks) == 0 || snap.Tasks[0].Location != f.lane.scope.Location {
		t.Fatalf("job.json = %+v", snap)
	}
}

// A synthesized local lane takes the generation the reconciler compares
// (its user config's), and its filesystem is this controller's disk.
func TestLaneGenerationAndLocationOverride(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	target := config.TargetConfig{Name: "local", Workspace: t.TempDir(), SubmitTemplate: "true", MaxInflight: 1}
	lane, err := NewSSHBackend(SSHBackendConfig{Target: target, Store: st, GlobalCfg: &config.GlobalConfig{}, FS: rfs.NewLocalFS(),
		ControllerID: "c1", Generation: "user-config-gen"})
	if err != nil {
		t.Fatal(err)
	}
	if lane.Generation() != "user-config-gen" || lane.Location() != "local:c1" {
		t.Fatalf("generation %q location %q", lane.Generation(), lane.Location())
	}
}
