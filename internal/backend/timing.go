package backend

import (
	"github.com/gliese129/runq-lab/internal/store"
	"time"
)

func applyTaskTiming(view *TaskView, task store.TaskRow) {
	times := store.CurrentTaskTimes(task)
	view.Times = times
	view.TimeHistory = store.ParseTaskTimes(task.TimingJSON)
	stamp := func(key string) *int64 {
		if e, ok := times[key]; ok {
			v := e.At
			return &v
		}
		return nil
	}
	view.StartedAt = stamp("execution_started_at")
	view.FinishedAt = stamp("execution_finished_at")
	duration := func(start, finish string, live bool) *float64 {
		a, ok := times[start]
		if !ok {
			return nil
		}
		b, ended := times[finish]
		end := b.At
		if !ended {
			if !live {
				return nil
			}
			end = time.Now().Unix()
		}
		if end < a.At {
			return nil
		}
		v := float64(end - a.At)
		return &v
	}
	view.ElapsedSec = duration("execution_started_at", "execution_finished_at", task.Status == "running")
	view.SchedulerSec = duration("scheduler_started_at", "scheduler_finished_at", task.Status == "running")
	if a, ok := times["execution_started_at"]; ok {
		view.TimeEstimated = a.Estimated
	}
	view.QueueSec = duration("scheduler_submitted_at", "scheduler_started_at", false)
	_, hasNativeSubmit := times["scheduler_submitted_at"]
	_, hasNativeStart := times["scheduler_started_at"]
	nativePair := hasNativeSubmit && hasNativeStart
	if view.QueueSec == nil && !nativePair {
		view.QueueSec = duration("submitted_at", "scheduler_started_at", false)
		if view.QueueSec != nil {
			view.QueueEstimated = true
		}
	}
	if view.QueueSec == nil && !nativePair {
		view.QueueSec = duration("submitted_at", "wrapper_started_at", false)
		if view.QueueSec != nil {
			view.QueueEstimated = true
		}
	}
	if view.QueueSec == nil && !nativePair && task.Status == "pending" {
		view.QueueSec = duration("submitted_at", "wrapper_started_at", true)
		if view.QueueSec != nil {
			view.QueueEstimated = true
		}
	}
	if !task.EnqueuedAt.IsZero() {
		end := time.Now().Unix()
		if task.FinishedAt != nil {
			end = task.FinishedAt.Unix()
		}
		if end >= task.EnqueuedAt.Unix() {
			v := float64(end - task.EnqueuedAt.Unix())
			view.TotalSec = &v
		}
	}
}
