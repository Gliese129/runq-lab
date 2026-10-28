package backend

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"time"

	"github.com/gliese129/runq-lab/internal/ingest"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/workspace"
)

// Job files: the job-level half of the SDK file contract.
//
//	<job_dir>/job.json                     runq → scripts: the job and its tasks
//	<task_dir>/results.<producer>.jsonl    scripts → runq: results per task
//
// job_dir is the parent of the tasks' task_dir. A scoring script reads
// job.json with runq.job() and writes results.<producer>.jsonl through
// job.tasks[i].record(); this lane publishes the first and ingests the second.
//
// Cost: deciding whether job.json changed is store-only; the target sees a
// write only when it did. Producer files are found with one listing exec
// per jobFilesChunk job dirs: every pass while tasks run, every
// idleProducerScanInterval otherwise. Background passes cover recent jobs
// (unfinished, or created/finished within recentJobWindow); a forced pass
// (--fresh, dashboard refresh) covers every unarchived job of the target.

const (
	jobSnapshotVersion = 2
	recentJobWindow    = 14 * 24 * time.Hour
	jobFilesChunk      = 100

	idleProducerScanInterval = 5 * time.Minute
)

// JobSnapshot is <job_dir>/job.json: every version of every cell, each
// with its number within its cell and whether it is the one the cell shows
// (version 2; version 1 listed the selected versions only).
type JobSnapshot struct {
	Version          int               `json:"version"`
	Job              JobSnapshotJob    `json:"job"`
	Tasks            []JobSnapshotTask `json:"tasks"`
	UpdatedAt        int64             `json:"updated_at"`
	ObservedAt       *int64            `json:"observed_at"`
	ObservationState string            `json:"observation_state"`
}

type JobSnapshotJob struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Project   string `json:"project"`
	Status    string `json:"status"`
	Target    string `json:"target"`
	CreatedAt int64  `json:"created_at"`
}

type JobSnapshotTask struct {
	ID            string         `json:"id"`
	RunName       string         `json:"run_name"`
	Status        string         `json:"status"`
	Params        map[string]any `json:"params"`
	TaskDir       string         `json:"task_dir"`
	OutputDir     string         `json:"output_dir"`
	CheckpointDir string         `json:"checkpoint_dir"`
	StartedAt     *int64         `json:"started_at"`
	FinishedAt    *int64         `json:"finished_at"`
	LastOutputAt  *int64         `json:"last_output_at"`
	BatchSeq      int            `json:"batch_seq"`
	Version       int            `json:"version"`
	Selected      bool           `json:"selected"`
	Target        string         `json:"target"`
}

// jobSnapshot builds a job's job.json from the store; dir is the job's
// directory on target, "" when no task there recorded a workspace (nothing
// to publish into). An experiment that ran on several targets gets the same
// job.json in its directory on each.
func jobSnapshot(ctx context.Context, st *store.Store, j store.JobRow, target string) (snap JobSnapshot, dir string, err error) {
	tasks, err := st.JobTasksInOrder(ctx, j.ID)
	if err != nil {
		return snap, "", err
	}
	picks, err := st.CellPicks(ctx, j.ID)
	if err != nil {
		return snap, "", err
	}
	version := map[string]int{}
	for _, c := range store.GroupCells(tasks, picks) {
		for i, v := range c.Versions {
			version[v.ID] = i + 1
		}
	}
	var selected []store.TaskRow
	snap = JobSnapshot{Version: jobSnapshotVersion, Tasks: []JobSnapshotTask{},
		Job: JobSnapshotJob{ID: j.ID, Name: j.Name, Project: j.ProjectName, Status: j.Status, Target: j.Target, CreatedAt: j.CreatedAt.Unix()}}
	for _, t := range tasks {
		if t.SupersededBy == "" {
			selected = append(selected, t)
		}
		if t.TaskDir == "" {
			continue
		}
		taskTarget := t.Target
		if taskTarget == "" {
			taskTarget = j.Target
		}
		if dir == "" && taskTarget == target {
			dir = path.Dir(t.TaskDir)
		}
		params := map[string]any{}
		_ = json.Unmarshal([]byte(t.ParamsJSON), &params)
		obs, err := st.SourceObservation(ctx, t.ID)
		if err != nil {
			return snap, "", err
		}
		snap.Tasks = append(snap.Tasks, JobSnapshotTask{
			ID: t.ID, RunName: t.RunName, Status: t.Status, Params: params, TaskDir: t.TaskDir,
			OutputDir: workspace.OutputsDir(t.TaskDir), CheckpointDir: workspace.CheckpointsDir(t.TaskDir),
			StartedAt: unixPtr(t.StartedAt), FinishedAt: unixPtr(t.FinishedAt), LastOutputAt: obs.LastOutputAt, BatchSeq: t.BatchSeq,
			Version: version[t.ID], Selected: t.SupersededBy == "", Target: taskTarget,
		})
	}
	snap.ObservedAt, snap.ObservationState, err = st.JobObservation(ctx, selected)
	return snap, dir, err
}

// hash covers everything but the two clocks, so a pass that only re-observed
// the same state writes nothing.
func (s JobSnapshot) hash() string {
	s.UpdatedAt, s.ObservedAt = 0, nil
	raw, _ := json.Marshal(s)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// publishJobSnapshot writes the job's job.json when its content changed
// since the last publish.
func (b *SSHBackend) publishJobSnapshot(ctx context.Context, jobID string) error {
	j, err := b.store.GetJob(ctx, jobID)
	if err != nil || j == nil {
		return err
	}
	_, err = b.publishJob(ctx, *j)
	return err
}

func (b *SSHBackend) publishJob(ctx context.Context, j store.JobRow) (JobSnapshot, error) {
	snap, dir, err := jobSnapshot(ctx, b.store, j, b.targetName)
	if err != nil || dir == "" {
		return snap, err
	}
	h := snap.hash() + "@" + b.targetName // one stored hash per job: a second target's copy rewrites
	if prev, err := b.store.JobSnapshotHash(ctx, j.ID); err != nil || prev == h {
		return snap, err
	}
	snap.UpdatedAt = time.Now().Unix()
	raw, _ := json.Marshal(snap)
	if err := atomicRemote(ctx, rfs.WithContext(b.backend.FS, ctx), path.Join(dir, "job.json"), raw); err != nil {
		return snap, err
	}
	return snap, b.store.SetJobSnapshotHash(ctx, j.ID, h)
}

// reconcileJobFiles is one job-files pass: publish changed job.json files,
// then (scan) ingest grown producer results. all widens the pass from recent
// jobs to every unarchived job (forced refresh). Only the active lane of a
// target runs it; retiring lanes share the target's jobs.
func (b *SSHBackend) reconcileJobFiles(ctx context.Context, all, scan bool) {
	if b.scope.IsRetiring() {
		return
	}
	jobs, err := b.store.ListJobs(ctx, "", b.targetName)
	if err != nil {
		b.logger.Warn("job files: list jobs", "error", err)
		return
	}
	cutoff := time.Now().Add(-recentJobWindow)
	dirs := map[string]map[string]string{} // job dir → task dir → task id
	order := []string{}
	for _, j := range jobs {
		recent := !store.IsTerminalJobStatus(j.Status) || j.CreatedAt.After(cutoff) || (j.FinishedAt != nil && j.FinishedAt.After(cutoff))
		if j.ArchivedAt != nil || !(all || recent) {
			continue
		}
		snap, err := b.publishJob(ctx, j)
		if err != nil {
			b.logger.Debug("job files: publish job.json", "job", j.ID, "error", err)
		}
		for _, t := range snap.Tasks {
			if t.Target != b.targetName {
				continue // another target's version: not on this filesystem
			}
			dir := path.Dir(t.TaskDir)
			if dirs[dir] == nil {
				dirs[dir] = map[string]string{}
				order = append(order, dir)
			}
			dirs[dir][t.TaskDir] = t.ID
		}
	}
	if !scan {
		return
	}
	fsys := rfs.WithContext(b.backend.FS, ctx)
	for start := 0; start < len(order); start += jobFilesChunk {
		files, err := listProducerFiles(ctx, fsys, order[start:min(start+jobFilesChunk, len(order))])
		if err != nil {
			b.logger.Debug("job files: list producer results", "error", err)
			continue
		}
		for _, f := range files {
			taskID := dirs[path.Dir(f.TaskDir)][f.TaskDir]
			if taskID == "" {
				continue // not a version of this job (a stray dir)
			}
			err := b.store.WithTaskLock(taskID, func() error {
				row, err := b.store.GetTask(ctx, taskID)
				if err != nil || row == nil {
					return err
				}
				_, err = ingest.ReapProducerResults(ctx, b.store, ingest.Target{TaskID: row.ID, JobID: row.JobID, Dir: row.TaskDir, FS: fsys}, f.Producer, f.Size)
				return err
			})
			if err != nil {
				b.logger.Debug("job files: ingest producer results", "task", taskID, "producer", f.Producer, "error", err)
			}
		}
	}
}

type producerFile struct {
	TaskDir  string
	Producer string
	Size     int64
}

// listProducerScript prints [[task_dir, producer, size], ...] for every
// <job_dir>/<task>/results.<producer>.jsonl under the given job dirs, in one
// exec on the machine that owns the files.
const listProducerScript = `import json, os, re, sys
pat = re.compile(r"^results\.([A-Za-z0-9_.-]+)\.jsonl$")
out = []
for root in sys.argv[1:]:
    try:
        tasks = list(os.scandir(root))
    except OSError:
        continue
    for task in tasks:
        try:
            if not task.is_dir():
                continue
            for f in os.scandir(task.path):
                m = pat.match(f.name)
                if m:
                    out.append([task.path, m.group(1), f.stat().st_size])
        except OSError:
            continue
print(json.dumps(out))
`

func listProducerFiles(ctx context.Context, fsys rfs.FS, jobDirs []string) ([]producerFile, error) {
	stdout, stderr, code, err := fsys.Exec(ctx, "python3", append([]string{"-c", listProducerScript}, jobDirs...)...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("list producer results: %s", stderr)
	}
	var rows [][]any
	if err := json.Unmarshal(stdout, &rows); err != nil {
		return nil, err
	}
	out := make([]producerFile, 0, len(rows))
	for _, r := range rows {
		dir, _ := r[0].(string)
		producer, _ := r[1].(string)
		size, _ := r[2].(float64)
		if dir != "" && ingest.ValidProducer(producer) {
			out = append(out, producerFile{TaskDir: path.Clean(dir), Producer: producer, Size: int64(size)})
		}
	}
	return out, nil
}

// atomicRemote publishes name through a temp file and a rename, so readers
// see the old content or the new, never a partial write.
func atomicRemote(ctx context.Context, fsys rfs.FS, name string, data []byte) error {
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	tmp := name + ".tmp." + hex.EncodeToString(nonce)
	if err := fsys.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	_, stderr, code, err := fsys.Exec(ctx, "mv", "-f", tmp, name)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("publish %s: %s", name, stderr)
	}
	return nil
}

func unixPtr(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	v := t.Unix()
	return &v
}
