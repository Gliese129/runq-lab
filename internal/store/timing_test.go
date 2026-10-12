package store

import "testing"

func TestTimeHistoryPreservesAttemptsAndStrongerEvidence(t *testing.T) {
	row := TaskRow{}
	row.TimingJSON = TimingFields(row, map[string]TimeEvidence{"execution_started_at": {At: 100, Source: "wrapper", ObservedAt: 200}})["timing_json"].(string)
	if len(TimingFields(row, map[string]TimeEvidence{"execution_started_at": {At: 100, Source: "wrapper", ObservedAt: 300}})) != 0 {
		t.Fatal("repeat observation changed receipt")
	}
	if len(TimingFields(row, map[string]TimeEvidence{"execution_started_at": {At: 101, Source: "legacy_wrapper", Estimated: true}})) != 0 {
		t.Fatal("estimate displaced precise evidence")
	}
	row.RetryCount = 1
	row.TimingJSON = TimingFields(row, map[string]TimeEvidence{"execution_started_at": {At: 400, Source: "wrapper"}})["timing_json"].(string)
	history := ParseTaskTimes(row.TimingJSON)
	if history["0"]["execution_started_at"].At != 100 || history["1"]["execution_started_at"].Attempt != 1 {
		t.Fatal("attempt history lost")
	}
}
