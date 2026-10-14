package backend

import (
	"github.com/gliese129/runq-lab/internal/store"
	"testing"
	"time"
)

func TestTaskTimingSeparatesQueueResourceAndScript(t *testing.T) {
	row := store.TaskRow{Status: "success", EnqueuedAt: time.Unix(100, 0)}
	fields := store.TimingFields(row, map[string]store.TimeEvidence{
		"scheduler_submitted_at": {At: 110, Source: "scheduler"},
		"scheduler_started_at":   {At: 710, Source: "scheduler"},
		"scheduler_finished_at":  {At: 722, Source: "scheduler"},
		"wrapper_started_at":     {At: 711, Source: "wrapper"},
		"execution_started_at":   {At: 720, Source: "wrapper"},
		"execution_finished_at":  {At: 722, Source: "wrapper"},
	})
	row.TimingJSON = fields["timing_json"].(string)
	var view TaskView
	applyTaskTiming(&view, row)
	if view.QueueSec == nil || *view.QueueSec != 600 || view.SchedulerSec == nil || *view.SchedulerSec != 12 || view.ElapsedSec == nil || *view.ElapsedSec != 2 {
		t.Fatalf("incorrect durations: %+v", view)
	}
}

func TestTaskTimingUnknownZeroAndClockSkew(t *testing.T) {
	old := time.Unix(100, 0)
	row := store.TaskRow{Status: "failed", StartedAt: &old, FinishedAt: &old}
	var view TaskView
	applyTaskTiming(&view, row)
	if view.ElapsedSec != nil {
		t.Fatal("legacy dispatch timestamp became execution time")
	}
	for _, finish := range []int64{100, 99} {
		row.TimingJSON = store.TimingFields(store.TaskRow{}, map[string]store.TimeEvidence{
			"execution_started_at": {At: 100, Source: "wrapper"}, "execution_finished_at": {At: finish, Source: "wrapper"},
		})["timing_json"].(string)
		view = TaskView{}
		applyTaskTiming(&view, row)
		if finish == 100 && (view.ElapsedSec == nil || *view.ElapsedSec != 0) {
			t.Fatal("valid zero lost")
		}
		if finish == 99 && view.ElapsedSec != nil {
			t.Fatal("negative interval clamped to zero")
		}
	}
}

func TestRestoreKeepsDispatchBaselineSeparateFromExecution(t *testing.T) {
	row := store.TaskRow{TimingJSON: `{"dispatch_started_at":{"at":100,"source":"controller"},"execution_started_at":{"at":200,"source":"wrapper"}}`}
	task := TaskRowToSchedulerTask(&row)
	if task.StartedAt == nil || task.StartedAt.Unix() != 100 {
		t.Fatal("recovery changed internal dispatch baseline")
	}
}
