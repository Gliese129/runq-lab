package remote

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/store"
)

// A big job whose queue has just drained: the bare `qstat` prints nothing, so
// the batch probe falls back to per-task probing. Finished cells come first in
// submission order and the last few, already finished on the cluster but not yet
// settled here, come last. See TestEmptyQueueSettlesUnfinishedTasks.
type drainedJob struct {
	st    *store.Store
	root  string
	jobID string
	mu    sync.Mutex
	// ext ids probed one by one, per pass
	probed []string
}

func newDrainedJob(t *testing.T, finished int, settle []string) (*drainedJob, *Backend) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	d := &drainedJob{st: st, root: t.TempDir()}
	jobID, _ := seedTask(t, st, filepath.Join(d.root, "t1"), "success", SourceWrapper)
	d.jobID = jobID
	base := time.Now().Add(-time.Hour)
	add := func(id, ext, status, source string, at time.Time, wrapper string) {
		dir := filepath.Join(d.root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if wrapper != "" {
			if err := os.WriteFile(filepath.Join(dir, statusFileName), []byte(`{"status":"`+wrapper+`","exit_code":0}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.InsertTask(context.Background(), &store.TaskRow{
			ID: id, JobID: jobID, ProjectName: "p", Command: "c", ParamsJSON: "{}",
			Status: status, StatusSource: source, ExternalID: ext, TaskDir: dir, EnqueuedAt: at,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range finished {
		add(fmt.Sprintf("final%03d", i), fmt.Sprintf("1%03d", i), "success", SourceWrapper, base.Add(time.Duration(i)*time.Second), "success")
	}
	for i, id := range settle {
		add(id, fmt.Sprintf("2%03d", i), "running", SourceWrapper, time.Now(), "success")
	}
	cfg := &config.TargetConfig{
		StatusTemplate:     "qstat",
		StatusParser:       []string{"awk -v id={{ext_id}} '$1==id{print $5}'"},
		StatusListTemplate: "qstat",
		StatusListParser:   []string{"awk 'NR>2'"},
	}
	b := &Backend{Cfg: cfg, Store: st, FS: newTestFSFromRunner(func(_ context.Context, cmd string) (string, error) {
		if strings.Contains(cmd, "awk -v id=") {
			fields := strings.Fields(cmd[strings.Index(cmd, "awk -v id="):])
			d.mu.Lock()
			d.probed = append(d.probed, strings.Trim(strings.TrimPrefix(fields[2], "id="), "'"))
			d.mu.Unlock()
			return "gone", nil
		}
		return "", nil // an empty queue: bare qstat and the list parser print nothing
	})}
	return d, b
}

func (d *drainedJob) status(t *testing.T, id string) string {
	t.Helper()
	row, err := d.st.GetTask(context.Background(), id)
	if err != nil || row == nil {
		t.Fatalf("task %s: %+v %v", id, row, err)
	}
	return row.Status + "/" + row.StatusSource
}

func TestEmptyQueueSettlesUnfinishedTasks(t *testing.T) {
	settle := []string{"last1", "last2", "last3", "last4"}
	d, b := newDrainedJob(t, 60, settle)
	if err := b.SchedulerProbe(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	for _, id := range settle {
		if got := d.status(t, id); got != "success/"+SourceWrapper {
			t.Fatalf("%s = %s, want settled from its status.json", id, got)
		}
	}
	// One remote command per unfinished task plus the ration for timing repair.
	// Before: one per task, 64 commands at 0.75s each against a 30s budget.
	if want := len(settle) + hardFinalTimingProbesPerPass; len(d.probed) > want {
		t.Fatalf("per-task scheduler commands = %d, want at most %d: %v", len(d.probed), want, d.probed)
	}
	if !strings.HasPrefix(d.probed[0], "2") {
		t.Fatalf("an unfinished task should be probed first, got %v", d.probed)
	}
}

func TestTimingRepairIsRationedAndNotRepeated(t *testing.T) {
	d, b := newDrainedJob(t, 10, nil)
	// One task nothing can settle keeps the job in every pass.
	dir := filepath.Join(d.root, "open")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := d.st.InsertTask(context.Background(), &store.TaskRow{
		ID: "open", JobID: d.jobID, ProjectName: "p", Command: "c", ParamsJSON: "{}",
		Status: "running", StatusSource: SourceWrapper, TaskDir: dir, EnqueuedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for pass := 1; pass <= 6; pass++ {
		d.mu.Lock()
		d.probed = nil
		d.mu.Unlock()
		if err := b.SchedulerProbe(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
		repairs := 0
		for _, ext := range d.probed {
			if ext == "" {
				continue
			}
			seen[ext]++
			repairs++
		}
		if repairs > hardFinalTimingProbesPerPass {
			t.Fatalf("pass %d spent %d probes on timing repair", pass, repairs)
		}
	}
	// t1 (seedTask) and the 10 finished cells: each tried exactly once in time.
	if len(seen) != 11 {
		t.Fatalf("repairs reached %d tasks, want 11: %v", len(seen), seen)
	}
	for ext, n := range seen {
		if n != 1 {
			t.Fatalf("task %s probed %d times", ext, n)
		}
	}
}

func TestOrderForReconcilePutsOpenVerdictsFirst(t *testing.T) {
	tasks := []store.TaskRow{
		{ID: "final-a", Status: "success", StatusSource: SourceWrapper},
		{ID: "guess", Status: "failed", StatusSource: SourceInferred},
		{ID: "final-b", Status: "killed", StatusSource: SourceRunq},
		{ID: "live", Status: "running", StatusSource: SourceScheduler},
	}
	var got []string
	for _, tk := range orderForReconcile(tasks) {
		got = append(got, tk.ID)
	}
	if want := "guess live final-a final-b"; strings.Join(got, " ") != want {
		t.Fatalf("order = %v, want %s", got, want)
	}
}

// A kill must not wait out a whole scheduler walk: it takes its turn between two
// tasks, and the pass notices the row changed under it.
func TestKillDoesNotWaitForWholePass(t *testing.T) {
	d, b := newDrainedJob(t, 0, nil)
	for i := range 8 {
		id := fmt.Sprintf("live%d", i)
		dir := filepath.Join(d.root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := d.st.InsertTask(context.Background(), &store.TaskRow{
			ID: id, JobID: d.jobID, ProjectName: "p", Command: "c", ParamsJSON: "{}",
			Status: "running", StatusSource: SourceScheduler, ExternalID: fmt.Sprintf("7%d", i), TaskDir: dir, EnqueuedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	var once sync.Once
	killDone := make(chan time.Time, 1)
	b.Cfg.KillTemplate = "qdel {{ext_id}}"
	b.Cfg.StatusListTemplate = ""
	b.FS = newTestFSFromRunner(func(_ context.Context, cmd string) (string, error) {
		switch {
		case strings.HasPrefix(cmd, "qdel"):
			return "", nil
		case strings.Contains(cmd, "awk -v id="):
			once.Do(func() {
				go func() {
					if _, err := b.Kill(context.Background(), "live7"); err != nil {
						t.Errorf("kill: %v", err)
					}
					killDone <- time.Now()
				}()
			})
			time.Sleep(60 * time.Millisecond)
			return "running", nil
		}
		return "", nil
	})
	if err := b.SchedulerProbe(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	passDone := time.Now()
	killedAt := <-killDone
	if !killedAt.Before(passDone) {
		t.Fatalf("kill finished %v after the pass: it waited for the whole walk", killedAt.Sub(passDone))
	}
	if got := d.status(t, "live7"); !strings.HasPrefix(got, "killed/") {
		t.Fatalf("live7 = %s: the pass overwrote the kill", got)
	}
}
