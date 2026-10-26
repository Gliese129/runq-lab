package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"sync"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/submitplan"
)

// appendTarget is the existing job an append (`--into jb…`) adds to.
type appendTarget struct {
	job   *store.JobRow
	root  string // the job directory: new tasks live next to the old ones
	tasks []store.TaskRow
	first store.JobBatch // its name is the W&B group of every batch
	seq   int            // the batch number this append gets
}

var appendLocks sync.Map // job id → *sync.Mutex: one append per job at a time

func lockAppend(jobID string) func() {
	mu, _ := appendLocks.LoadOrStore(jobID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	return mu.(*sync.Mutex).Unlock
}

// loadAppendTarget checks the job can take an append from this lane.
func (b *Backend) loadAppendTarget(ctx context.Context, jobID, project string) (*appendTarget, error) {
	j, err := b.Store.GetJob(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if j == nil {
		return nil, fmt.Errorf("--into %s: job not found", jobID)
	}
	if j.ProjectName != project {
		return nil, fmt.Errorf("--into %s: the job belongs to project %q, not %q", j.ID, j.ProjectName, project)
	}
	if j.Target != b.Cfg.Name {
		return nil, fmt.Errorf("--into %s: the job runs on target %s, not %s; an append uses the job's target", j.ID, j.Target, b.Cfg.Name)
	}
	if j.ArchivedAt != nil {
		return nil, fmt.Errorf("--into %s: the job is archived; unarchive it before appending", j.ID)
	}
	if j.Status == "paused" {
		return nil, fmt.Errorf("--into %s: the job is paused; resume it before appending", j.ID)
	}
	tasks, err := b.Store.ListTasks(ctx, store.TaskFilter{JobID: j.ID})
	if err != nil {
		return nil, err
	}
	t := &appendTarget{job: j, tasks: tasks}
	for _, task := range tasks {
		if task.TaskDir != "" {
			t.root = path.Dir(task.TaskDir)
			break
		}
	}
	if t.root == "" {
		return nil, fmt.Errorf("--into %s: the job has no task directory to append to", j.ID)
	}
	batches, err := b.Store.ListJobBatches(ctx, j.ID)
	if err != nil {
		return nil, err
	}
	if len(batches) > 0 {
		t.first = batches[0]
		t.seq = batches[len(batches)-1].Seq + 1
	} else {
		t.first, t.seq = store.JobBatch{Name: j.Name}, 2
	}
	return t, nil
}

// AppendSummaryFor sorts rows against a job's tasks (live ones count;
// every run name is taken). exclude holds the scheduler-only params.
func AppendSummaryFor(tasks []store.TaskRow, rows []job.Row, exclude map[string]bool, rerunFailed bool, jobID string, seq int) job.AppendSummary {
	existing := make([]job.ExistingCell, 0, len(tasks))
	taken := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		if task.RunName != "" {
			taken[task.RunName] = true
		}
		if task.SupersededBy != "" {
			continue
		}
		var p job.TaskParams
		_ = json.Unmarshal([]byte(task.ParamsJSON), &p)
		existing = append(existing, job.ExistingCell{TaskID: task.ID, RunName: task.RunName, Status: task.Status, Params: p})
	}
	cells := job.ClassifyCells(rows, existing, exclude, rerunFailed)
	return job.SummarizeCells(jobID, seq, cells, taken)
}

// classifyAppend sorts the plan's rows against the job's live tasks, keeps
// only the tasks to submit (renaming new cells whose run name is taken) and
// returns the summary plus the failed tasks the reruns supersede.
func classifyAppend(plan *submitplan.Plan, t *appendTarget, rerunFailed bool, overrides *job.Overrides) (job.AppendSummary, map[string]string) {
	exclude := make(map[string]bool, len(plan.SchedulerParams))
	for _, name := range plan.SchedulerParams {
		exclude[name] = true
	}
	rows := make([]job.Row, len(plan.Tasks))
	for i, task := range plan.Tasks {
		rows[i] = job.Row{Run: task.RunName, Params: task.Params}
	}
	summary := AppendSummaryFor(t.tasks, rows, exclude, rerunFailed, t.job.ID, t.seq)
	cells := summary.Cells
	summary.Differs = appendDiffers(plan.CommandTemplate, overrides, t.first)

	supersede := map[string]string{}
	kept := plan.Tasks[:0]
	for i, task := range plan.Tasks {
		c := cells[i]
		if c.Class != job.CellNew && c.Class != job.CellRerunFailed {
			continue
		}
		if c.Run != task.RunName {
			task.RunName = c.Run
			if task.Env != nil {
				task.Env["RUNQ_RUN_NAME"] = c.Run
			}
		}
		if c.Class == job.CellRerunFailed {
			supersede[c.TaskID] = task.TaskID
		}
		kept = append(kept, task)
	}
	plan.Tasks = kept
	return summary, supersede
}

// appendDiffers compares the batch with the job's first one.
func appendDiffers(command string, overrides *job.Overrides, first store.JobBatch) job.AppendDiffers {
	var cfg job.JobConfig
	_ = json.Unmarshal([]byte(first.ConfigJSON), &cfg)
	firstCommand := first.Command
	if firstCommand == "" {
		firstCommand = cfg.Command
	}
	return job.AppendDiffers{
		Command:   firstCommand != "" && command != firstCommand,
		Overrides: !reflect.DeepEqual(normalizeOverrides(overrides), normalizeOverrides(cfg.Overrides)),
	}
}

func normalizeOverrides(o *job.Overrides) any {
	if o == nil {
		return nil
	}
	raw, _ := json.Marshal(o)
	if string(raw) == "{}" || string(raw) == "null" {
		return nil
	}
	var v any
	_ = json.Unmarshal(raw, &v)
	return v
}
