package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"sync"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/submitplan"
)

// appendTarget is the existing job an append (`--into jb…`) adds to.
type appendTarget struct {
	job     *store.JobRow
	root    string // the experiment's directory on this lane's target ("" = none yet)
	tasks   []store.TaskRow
	picks   map[string]string // pinned versions: cell → task id
	last    map[string]int    // highest version number each cell handed out
	batches []store.JobBatch
	first   store.JobBatch // its name is the W&B group of every submission
	seq     int            // the submission number this append gets
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
	// New versions live next to the experiment's earlier ones on this
	// target; a submission to a target it has not run on gets a fresh
	// directory there (named by the experiment id, like the first).
	t := &appendTarget{job: j, tasks: tasks}
	for _, task := range tasks {
		onLane := task.Target == b.Cfg.Name || task.Target == "" && j.Target == b.Cfg.Name
		if task.TaskDir != "" && onLane {
			t.root = path.Dir(task.TaskDir)
			break
		}
	}
	if t.picks, err = b.Store.CellPicks(ctx, j.ID); err != nil {
		return nil, err
	}
	if t.last, err = b.Store.CellLastVersions(ctx, j.ID); err != nil {
		return nil, err
	}
	if t.batches, err = b.Store.ListJobBatches(ctx, j.ID); err != nil {
		return nil, err
	}
	if len(t.batches) > 0 {
		t.first = t.batches[0]
		t.seq = t.batches[len(t.batches)-1].Seq + 1
	} else {
		t.first, t.seq = store.JobBatch{Name: j.Name}, 2
	}
	return t, nil
}

// ExistingCells describes a job's cells by their selected versions.
// projectCmd stands in for a submission that ran the project's command.
func ExistingCells(tasks []store.TaskRow, picks map[string]string, batches []store.JobBatch, projectCmd string) []job.ExistingCell {
	commands := make(map[int]string, len(batches))
	for _, b := range batches {
		cmd := b.Command
		if cmd == "" {
			var cfg job.JobConfig
			_ = json.Unmarshal([]byte(b.ConfigJSON), &cfg)
			if cmd = cfg.Command; cmd == "" {
				cmd = projectCmd
			}
		}
		commands[b.Seq] = cmd
	}
	cells := store.GroupCells(tasks, picks)
	out := make([]job.ExistingCell, 0, len(cells))
	for _, c := range cells {
		if c.Name == "" {
			continue // a pre-rc6 task without a name: no row can name it
		}
		e := job.ExistingCell{Name: c.Name}
		for _, v := range c.Versions {
			if !store.IsTerminalTaskStatus(v.Status) {
				e.InFlight = true
			}
			if v.ID == c.Selected {
				e.TaskID, e.Status, e.Command = v.ID, v.Status, commands[v.BatchSeq]
				_ = json.Unmarshal([]byte(v.ParamsJSON), &e.Params)
			}
		}
		out = append(out, e)
	}
	return out
}

// classifyAppend sorts the plan's rows against the job's cells and keeps only
// the rows to submit: new cells and new versions of existing ones.
func classifyAppend(plan *submitplan.Plan, t *appendTarget, rerun []string, projectCmd string) (job.AppendSummary, error) {
	exclude := make(map[string]bool, len(plan.SchedulerParams))
	for _, name := range plan.SchedulerParams {
		exclude[name] = true
	}
	rows := make([]job.Row, len(plan.Tasks))
	for i, task := range plan.Tasks {
		rows[i] = job.Row{Run: task.RunName, Params: task.Params}
	}
	existing := ExistingCells(t.tasks, t.picks, t.batches, projectCmd)
	cells, err := job.ClassifyCells(rows, existing, exclude, rerun, plan.CommandTemplate)
	if err != nil {
		return job.AppendSummary{}, fmt.Errorf("--into %s: %w", t.job.ID, err)
	}
	kept := plan.Tasks[:0]
	for i, task := range plan.Tasks {
		if cells[i].Class == job.CellSkipped {
			continue
		}
		// The number the store will give it (store.nextVersionTx; one
		// append per job at a time): never a deleted version's number.
		if n := t.last[task.RunName]; n > 0 && task.Env != nil {
			task.Env["RUNQ_VERSION"] = strconv.Itoa(n + 1) // W&B names it "<cell> #N"
		}
		kept = append(kept, task)
	}
	plan.Tasks = kept
	return job.SummarizeCells(t.job.ID, t.seq, cells), nil
}
