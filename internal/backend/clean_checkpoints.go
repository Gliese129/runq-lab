package backend

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/gliese129/runq-lab/internal/diskspace"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/gliese129/runq-lab/internal/workspace"
)

// cleanCheckpoints is `clean --checkpoints`: for the selected (inactive)
// tasks, delete the contents of <task_dir>/checkpoints/ and nothing else —
// logs, results, outputs and DB rows stay. A checkpoint dir redirected via
// RUNQ_CHECKPOINT_DIR is not runq's to delete and is left alone.
//
// Unlike the full clean this touches the target at preview time too: the
// checkpoint bytes and the filesystems' free space ARE the preview. Per
// target filesystem that is one du Exec and one df Exec; the dry run also
// records each terminal job's usage (job_usage) while its checkpoints are
// still there.
func cleanCheckpoints(ctx context.Context, st *store.Store, resolveFS func(store.TaskRow) (rfs.FS, error),
	tasks []store.TaskRow, preview []CleanPreviewItem, dryRun bool) *CleanResult {
	type group struct {
		target string
		fs     rfs.FS
		tasks  []store.TaskRow
	}
	var order []string
	var failures []string
	groups := map[string]*group{}
	for _, t := range tasks {
		if t.TaskDir == "" {
			continue
		}
		if err := utils.SafeDeletePath(t.TaskDir); err != nil {
			slog.Warn("clean: refusing suspicious path", "task", t.ID, "error", err)
			failures = append(failures, fmt.Sprintf("task %s: refused %s: %v", t.ID, t.TaskDir, err))
			continue
		}
		fsys, err := resolveFS(t)
		if err != nil {
			slog.Warn("clean: target unavailable", "task", t.ID, "error", err)
			failures = append(failures, fmt.Sprintf("task %s: target unavailable: %v", t.ID, err))
			continue
		}
		if fsys == nil {
			fsys = rfs.NewLocalFS()
		}
		key := t.Target + "\x00" + t.TargetGeneration
		g := groups[key]
		if g == nil {
			g = &group{target: t.Target, fs: fsys}
			groups[key] = g
			order = append(order, key)
		}
		g.tasks = append(g.tasks, t)
	}

	res := &CleanResult{Failures: failures}
	if dryRun {
		res.Preview = preview
	}
	for _, key := range order {
		g := groups[key]
		ckDirs := make([]string, 0, len(g.tasks))
		var jobOrder []string
		jobs := map[string]*CheckpointJob{}
		jobDir := map[string]string{}
		for _, t := range g.tasks {
			ckDirs = append(ckDirs, workspace.CheckpointsDir(t.TaskDir))
			j := jobs[t.JobID]
			if j == nil {
				j = &CheckpointJob{JobID: t.JobID, Project: t.ProjectName, Target: g.target}
				jobs[t.JobID] = j
				jobOrder = append(jobOrder, t.JobID)
				jobDir[t.JobID] = path.Dir(t.TaskDir)
			}
			j.Tasks++
		}
		measure := ckDirs
		if dryRun {
			for _, id := range jobOrder {
				measure = append(measure, jobDir[id])
			}
		}
		use, merr := diskspace.Measure(ctx, g.fs, measure...)
		for _, t := range g.tasks {
			j := jobs[t.JobID]
			if merr != nil {
				j.Bytes, j.Files = -1, -1
				continue
			}
			u := use[workspace.CheckpointsDir(t.TaskDir)]
			j.Bytes += u.Bytes
			j.Files += u.Files
		}
		dirs := make([]string, 0, len(jobOrder))
		for _, id := range jobOrder {
			dirs = append(dirs, jobDir[id])
		}
		fss, _ := diskspace.Probe(ctx, g.fs, dirs...)
		for _, f := range fss {
			res.Filesystems = append(res.Filesystems, CleanFilesystem{Target: g.target, Filesystem: f})
		}
		for _, id := range jobOrder {
			j := jobs[id]
			if f, ok := diskspace.For(fss, jobDir[id]); ok {
				j.Mount = f.Mount
			}
			res.CheckpointJobs = append(res.CheckpointJobs, *j)
			if dryRun && merr == nil {
				recordJobUsage(ctx, st, id, use[jobDir[id]], jobDir[id])
			}
		}
		if dryRun {
			continue
		}
		cleaned, removed, cleanedJobs, skipped, fails := deleteCheckpointsGuarded(ctx, st, g.fs, g.tasks)
		res.Tasks += cleaned
		res.Skipped = append(res.Skipped, skipped...)
		res.Failures = append(res.Failures, fails...)
		// Freed is what is gone: measured again after the rm, not assumed.
		if merr == nil && len(removed) > 0 {
			if after, aerr := diskspace.Measure(ctx, g.fs, removed...); aerr == nil {
				for _, d := range removed {
					res.FreedBytes += max(0, use[d].Bytes-after[d].Bytes)
				}
			} else {
				res.Failures = append(res.Failures, fmt.Sprintf("target %s: could not measure what was freed: %v", g.target, aerr))
			}
		}
		res.Jobs += cleanedJobs
	}
	return res
}

// deleteCheckpointsGuarded is the act step of a checkpoints clean. Selection
// and the du pass can be long past (an SSH round trip, a confirmation
// prompt), so each batch takes its tasks' locks, re-reads every task and
// applies cleanBlocker (the rule the full clean applies in its DELETE), and
// only then empties the checkpoints of the tasks still inactive and still
// the attempt that was selected. The rest come back as skipped.
// Returns the dirs emptied, the dirs handed to rm, the number of jobs among
// them, the skipped tasks and the failures.
func deleteCheckpointsGuarded(ctx context.Context, st *store.Store, fsys rfs.FS, tasks []store.TaskRow) (int, []string, int, []CleanSkip, []string) {
	const batch = 100
	var (
		cleaned  int
		removed  []string
		skipped  []CleanSkip
		failures []string
	)
	jobs := map[string]bool{}
	for start := 0; start < len(tasks); start += batch {
		chunk := tasks[start:min(start+batch, len(tasks))]
		ids := make([]string, len(chunk))
		for i, t := range chunk {
			ids[i] = t.ID
		}
		_ = st.WithTaskLocks(ids, func() error {
			var dirs []string
			for _, t := range chunk {
				current, err := st.GetTask(ctx, t.ID)
				if err != nil {
					failures = append(failures, fmt.Sprintf("task %s: could not re-check its status, checkpoints kept: %v", t.ID, err))
					continue
				}
				if why := cleanBlocker(t, current); why != "" {
					status := ""
					if current != nil {
						status = current.Status
					}
					slog.Info("clean: skipping checkpoints of a task no longer cleanable", "task", t.ID, "reason", why)
					skipped = append(skipped, CleanSkip{TaskID: t.ID, JobID: t.JobID, Status: status, Reason: why})
					continue
				}
				dirs = append(dirs, workspace.CheckpointsDir(t.TaskDir))
				jobs[t.JobID] = true
			}
			if len(dirs) == 0 {
				return nil
			}
			n, fails := deleteCheckpointContents(ctx, fsys, dirs)
			cleaned += n
			removed = append(removed, dirs...)
			failures = append(failures, fails...)
			return nil
		})
	}
	return cleaned, removed, len(jobs), skipped, failures
}

// recordJobUsage stores a terminal job's measured directory (the dry run
// of a checkpoints clean is the last chance to see it with checkpoints).
func recordJobUsage(ctx context.Context, st *store.Store, jobID string, u diskspace.Usage, dir string) {
	if u.Bytes == 0 && u.Files == 0 {
		return // not on disk
	}
	jr, err := st.GetJob(ctx, jobID)
	if err != nil || jr == nil || !store.IsTerminalJobStatus(jr.Status) {
		return
	}
	rows, err := st.ListTasks(ctx, store.TaskFilter{JobID: jobID})
	if err != nil {
		return
	}
	n := 0
	for _, t := range rows {
		if t.TaskDir != "" && path.Dir(t.TaskDir) == dir {
			n++
		}
	}
	if n == 0 {
		return
	}
	_ = st.UpsertJobUsage(ctx, store.JobUsage{JobID: jobID, Bytes: u.Bytes, Files: u.Files, TaskCount: n, MeasuredAt: time.Now()})
}

// deleteCheckpointContents empties each existing checkpoints dir (dotfiles
// included, the dir itself kept), 100 dirs per Exec. Returns how many
// dirs the shell reported emptied and the batches that failed.
func deleteCheckpointContents(ctx context.Context, fsys rfs.FS, dirs []string) (int, []string) {
	const batch = 100
	cleaned := 0
	var failures []string
	for start := 0; start < len(dirs); start += batch {
		var sb strings.Builder
		for _, d := range dirs[start:min(start+batch, len(dirs))] {
			q := utils.ShellQuote(d)
			sb.WriteString("if [ -d " + q + " ]; then find " + q + " -mindepth 1 -maxdepth 1 -exec rm -rf -- {} + && echo ok; fi\n")
		}
		out, _, code, err := fsys.Exec(ctx, "sh", "-c", sb.String())
		if err != nil || code != 0 {
			slog.Warn("clean: checkpoint removal failed", "dirs", len(dirs), "exit", code, "err", err)
			failures = append(failures, fmt.Sprintf("checkpoint rm failed (exit %d): %v", code, err))
		}
		cleaned += strings.Count(string(out), "ok\n")
	}
	return cleaned, failures
}
