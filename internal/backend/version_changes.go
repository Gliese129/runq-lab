package backend

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/store"
)

// VersionChange is one thing a version changed against its cell's previous
// version: a param (by the cell rules only scheduler ones may differ), the
// command, the target, or a resource override. From/To are empty for the
// command (the templates are shown whole, on the submissions).
type VersionChange struct {
	Key  string `json:"key"`
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// submissionSnap is what a submission ran with, from its stored snapshot.
// command is "" when the snapshot does not say (submissions before the
// template was stored meant "the project's command then", now unknown).
type submissionSnap struct {
	command, target string
	overrides       map[string]string
}

func submissionSnaps(batches []store.JobBatch) map[int]submissionSnap {
	out := make(map[int]submissionSnap, len(batches))
	for _, b := range batches {
		var cfg job.JobConfig
		_ = json.Unmarshal([]byte(b.ConfigJSON), &cfg)
		snap := submissionSnap{command: b.Command, target: b.Target, overrides: map[string]string{}}
		if snap.command == "" {
			snap.command = cfg.Command
		}
		if o := cfg.Overrides; o != nil {
			if o.GPUsPerTask != nil {
				snap.overrides["gpus_per_task"] = strconv.Itoa(*o.GPUsPerTask)
			}
			if o.Timeout != nil {
				snap.overrides["timeout"] = *o.Timeout
			}
			for k, v := range o.Env {
				snap.overrides["env."+k] = v
			}
		}
		out[b.Seq] = snap
	}
	return out
}

// attachVersionChanges fills each version's Changes against the previous
// version of its cell; a cell's first version has none.
func attachVersionChanges(views []TaskView, batches []store.JobBatch) {
	snaps := submissionSnaps(batches)
	cells := map[string][]int{}
	var order []string
	for i, v := range views {
		cell := v.RunName
		if cell == "" {
			cell = v.ID
		}
		if _, ok := cells[cell]; !ok {
			order = append(order, cell)
		}
		cells[cell] = append(cells[cell], i)
	}
	for _, cell := range order {
		idx := cells[cell]
		sort.SliceStable(idx, func(a, b int) bool { return views[idx[a]].Version < views[idx[b]].Version })
		for k := 1; k < len(idx); k++ {
			prev, cur := &views[idx[k-1]], &views[idx[k]]
			cur.Changes = versionChanges(*prev, *cur, snaps[seqOf(*prev)], snaps[seqOf(*cur)])
		}
	}
}

func seqOf(v TaskView) int {
	if v.BatchSeq <= 0 {
		return 1
	}
	return v.BatchSeq
}

func versionChanges(prev, cur TaskView, ps, cs submissionSnap) []VersionChange {
	var out []VersionChange
	out = append(out, diffValues(paramText(prev.Params), paramText(cur.Params))...)
	pt, ct := prev.Target, cur.Target
	if pt == "" {
		pt = ps.target
	}
	if ct == "" {
		ct = cs.target
	}
	if pt != "" && ct != "" && pt != ct {
		out = append(out, VersionChange{Key: "target", From: pt, To: ct})
	}
	if ps.command != "" && cs.command != "" && !job.SameCommand(ps.command, cs.command, job.TaskParams(cur.Params)) {
		out = append(out, VersionChange{Key: "command"})
	}
	return append(out, diffValues(ps.overrides, cs.overrides)...)
}

func paramText(params map[string]any) map[string]string {
	out := make(map[string]string, len(params))
	for k, v := range params {
		out[k] = job.FormatValue(v)
	}
	return out
}

// diffValues lists the keys whose text differs, sorted; a missing value is ∅.
func diffValues(a, b map[string]string) []VersionChange {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	var out []VersionChange
	for _, k := range sorted {
		from, okA := a[k]
		to, okB := b[k]
		if okA && okB && from == to {
			continue
		}
		if !okA {
			from = "∅"
		}
		if !okB {
			to = "∅"
		}
		out = append(out, VersionChange{Key: k, From: from, To: to})
	}
	return out
}

// commandChanged reports, per submission, whether it ran another command
// than the one before it (both known): the two templates rendered for one
// of its versions' params (job.SameCommand).
func commandChanged(batches []store.JobBatch, views []TaskView) map[int]bool {
	snaps := submissionSnaps(batches)
	paramsOf := map[int]job.TaskParams{}
	for _, v := range views {
		if _, ok := paramsOf[seqOf(v)]; !ok {
			paramsOf[seqOf(v)] = job.TaskParams(v.Params)
		}
	}
	out := map[int]bool{}
	for i := 1; i < len(batches); i++ {
		a, b := snaps[batches[i-1].Seq].command, snaps[batches[i].Seq].command
		out[batches[i].Seq] = a != "" && b != "" && !job.SameCommand(a, b, paramsOf[batches[i].Seq])
	}
	return out
}

// paramUse says where each of a version's params went, by its submission's
// snapshot: the scheduler-only params it recorded (else today's answer for
// a submission before they were recorded) and the placeholders of the
// command template it ran.
func (m *MultiBackend) paramUse(ctx context.Context, row store.TaskRow, params map[string]any) map[string]string {
	batches, _ := m.store.ListJobBatches(ctx, row.JobID)
	seq := row.BatchSeq
	if seq <= 0 {
		seq = 1
	}
	var command string
	var sched map[string]bool
	for _, b := range batches {
		if b.Seq != seq {
			continue
		}
		snap := submissionSnaps([]store.JobBatch{b})[b.Seq]
		command = snap.command
		sched = job.SubmittedSchedulerParams([]string{b.ConfigJSON})
	}
	if sched == nil {
		target := row.Target
		sched = m.schedulerParams(ctx, row.ProjectName, target)
	}
	inCommand := map[string]bool{}
	for _, name := range job.Placeholders(command) {
		inCommand[name] = true
	}
	out := make(map[string]string, len(params))
	for k := range params {
		switch {
		case sched[k] && inCommand[k]:
			out[k] = "both"
		case sched[k]:
			out[k] = "scheduler"
		default:
			out[k] = "script"
		}
	}
	return out
}
