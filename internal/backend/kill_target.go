package backend

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gliese129/runq-lab/internal/store"
)

// ErrNotOnTarget: a target-scoped action named an experiment or version
// that has nothing on that target (409 invalid_state on the wire).
var ErrNotOnTarget = errors.New("not on that target")

// TargetKiller kills only the versions of an experiment that run on one
// target (`runq kill ex… -t <target>`). Experiments may span targets: each
// submission picks its own.
type TargetKiller interface {
	KillJobOnTarget(ctx context.Context, jobID, target string) error
}

// JobTargets lists the targets an experiment's versions run on, sorted. A
// version without a recorded target runs on the experiment's.
func JobTargets(jobTarget string, taskTargets []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	if len(taskTargets) == 0 {
		add(jobTarget)
	}
	for _, t := range taskTargets {
		if t == "" {
			t = jobTarget
		}
		add(t)
	}
	sort.Strings(out)
	return out
}

// NotOnTargetError says which targets an experiment does run on.
func NotOnTargetError(what, id, target string, on []string) error {
	where := "no target"
	if len(on) > 0 {
		where = strings.Join(on, ", ")
	}
	return fmt.Errorf("%s %s has no versions on target %q (it runs on %s): %w", what, id, target, where, ErrNotOnTarget)
}

// KillJobOnTarget is KillJob confined to one target's lanes: the
// experiment's versions on other targets keep running. Each lane's KillJob
// only acts on the tasks that lane owns.
func (m *MultiBackend) KillJobOnTarget(ctx context.Context, jobID, target string) error {
	if target == "" {
		return m.KillJob(ctx, jobID)
	}
	m.controlMu.Lock()
	defer m.controlMu.Unlock()
	m.routingMu.RLock()
	defer m.routingMu.RUnlock()
	j, err := m.store.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	if j == nil {
		return fmt.Errorf("job %q: %w", jobID, ErrNotFound)
	}
	tasks, err := m.store.ListTasks(ctx, store.TaskFilter{JobID: j.ID})
	if err != nil {
		return fmt.Errorf("list versions of %s: %w", j.ID, err)
	}
	taskTargets := make([]string, len(tasks))
	for i, t := range tasks {
		taskTargets[i] = t.Target
	}
	on := JobTargets(j.Target, taskTargets)
	found := false
	for _, t := range on {
		found = found || t == target
	}
	if !found {
		return NotOnTargetError("experiment", j.ID, target, on)
	}
	owners := m.liveLanes(target)
	if len(owners) == 0 {
		return fmt.Errorf("unknown target %q", target)
	}
	var killErrs []error
	for i, be := range owners {
		if err := be.KillJob(ctx, j.ID); err != nil {
			killErrs = append(killErrs, fmt.Errorf("kill job %s on %s owner lane %d: %w", j.ID, target, i+1, err))
		}
	}
	return errors.Join(killErrs...)
}
