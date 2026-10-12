package store

import (
	"encoding/json"
	"strconv"
	"time"
)

// TimeEvidence belongs to an execution attempt. At is a source timestamp;
// ObservedAt is the controller's receipt time and never substitutes for At.
type TimeEvidence struct {
	At         int64  `json:"at"`
	Source     string `json:"source"`
	ObservedAt int64  `json:"observed_at"`
	Attempt    int    `json:"attempt"`
	Estimated  bool   `json:"estimated,omitempty"`
}
type TaskTimeHistory map[string]map[string]TimeEvidence

func ParseTaskTimes(raw string) TaskTimeHistory {
	history := TaskTimeHistory{}
	_ = json.Unmarshal([]byte(raw), &history)
	if history == nil {
		history = TaskTimeHistory{}
	}
	return history
}
func CurrentTaskTimes(row TaskRow) map[string]TimeEvidence {
	return ParseTaskTimes(row.TimingJSON)[strconv.Itoa(row.RetryCount)]
}
func TimingFields(row TaskRow, events map[string]TimeEvidence) map[string]any {
	history := ParseTaskTimes(row.TimingJSON)
	key := strconv.Itoa(row.RetryCount)
	times := history[key]
	if times == nil {
		times = map[string]TimeEvidence{}
	}
	changed := false
	for name, e := range events {
		if e.At <= 0 {
			continue
		}
		e.Attempt = row.RetryCount
		if e.ObservedAt == 0 {
			e.ObservedAt = time.Now().Unix()
		}
		old, ok := times[name]
		if ok && !old.Estimated && e.Estimated {
			continue
		}
		if ok && old.At == e.At && old.Source == e.Source && old.Estimated == e.Estimated {
			continue
		}
		times[name] = e
		changed = true
	}
	if !changed {
		return map[string]any{}
	}
	history[key] = times
	raw, _ := json.Marshal(history)
	return map[string]any{"timing_json": string(raw)}
}
func ControllerTime(row TaskRow, name string, now time.Time) map[string]any {
	return TimingFields(row, map[string]TimeEvidence{name: {At: now.Unix(), Source: "controller", ObservedAt: now.Unix()}})
}

func initializeTaskTiming(row *TaskRow) {
	if row.TimingJSON == "" && !row.EnqueuedAt.IsZero() {
		if value, ok := ControllerTime(*row, "enqueued_at", row.EnqueuedAt)["timing_json"]; ok {
			row.TimingJSON = value.(string)
		}
	}
}
