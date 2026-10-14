package store

import (
	"encoding/json"
	"time"
)

// TimeEvidence belongs to an execution. At is a source timestamp;
// ObservedAt is the controller's receipt time and never substitutes for At.
type TimeEvidence struct {
	At         int64  `json:"at"`
	Source     string `json:"source"`
	ObservedAt int64  `json:"observed_at"`
	Estimated  bool   `json:"estimated,omitempty"`
}
type TaskTimes map[string]TimeEvidence

func ParseTaskTimes(raw string) TaskTimes {
	history := TaskTimes{}
	_ = json.Unmarshal([]byte(raw), &history)
	if history == nil {
		history = TaskTimes{}
	}
	return history
}
func CurrentTaskTimes(row TaskRow) map[string]TimeEvidence {
	return ParseTaskTimes(row.TimingJSON)
}
func TimingFields(row TaskRow, events map[string]TimeEvidence) map[string]any {
	times := ParseTaskTimes(row.TimingJSON)
	if times == nil {
		times = map[string]TimeEvidence{}
	}
	changed := false
	for name, e := range events {
		if e.At <= 0 {
			continue
		}
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
	raw, _ := json.Marshal(times)
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
