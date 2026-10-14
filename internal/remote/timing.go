package remote

import (
	"context"
	"github.com/gliese129/runq-lab/internal/store"
	"time"
)

func observationTimeFields(tk store.TaskRow, sf statusFile, pr ProbeResult) map[string]any {
	events := map[string]store.TimeEvidence{}
	add := func(name string, at int64, source string, estimated bool) {
		if at > 0 {
			events[name] = store.TimeEvidence{At: at, Source: source, ObservedAt: time.Now().Unix(), Estimated: estimated}
		}
	}
	if sf.TimeVersion == 1 {
		add("wrapper_started_at", sf.WrapperStartedAt, "wrapper", false)
		add("execution_started_at", sf.ExecutionStartedAt, "wrapper", false)
		add("execution_finished_at", sf.ExecutionFinishedAt, "wrapper", false)
	} else if sf.TimeVersion == 0 {
		// The old wrapper timestamp is near the command entrance, but cannot
		// distinguish setup/cd/sidecar overhead. Keep its uncertainty explicit.
		add("execution_started_at", sf.StartedAt, "legacy_wrapper", true)
		if sf.StartedAt > 0 {
			add("execution_finished_at", sf.FinishedAt, "legacy_wrapper", true)
		}
	}
	add("scheduler_submitted_at", pr.SubmittedAt, "scheduler", false)
	add("scheduler_started_at", pr.StartedAt, "scheduler", false)
	add("scheduler_finished_at", pr.FinishedAt, "scheduler", false)
	return store.TimingFields(tk, events)
}
func (b *Backend) applyTimeOnly(ctx context.Context, tk store.TaskRow, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return b.persistDecision(ctx, tk, Decision{Status: tk.Status, Source: tk.StatusSource}, fields)
}
