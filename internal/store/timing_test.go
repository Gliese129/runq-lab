package store

import "testing"

func TestTimeEvidenceIsIdempotentAndKeepsStrongerSource(t *testing.T) {
	row := TaskRow{}
	row.TimingJSON = TimingFields(row, map[string]TimeEvidence{"execution_started_at": {At: 100, Source: "wrapper", ObservedAt: 200}})["timing_json"].(string)
	if len(TimingFields(row, map[string]TimeEvidence{"execution_started_at": {At: 100, Source: "wrapper", ObservedAt: 300}})) != 0 {
		t.Fatal("repeat observation changed receipt")
	}
	if len(TimingFields(row, map[string]TimeEvidence{"execution_started_at": {At: 101, Source: "legacy_wrapper", Estimated: true}})) != 0 {
		t.Fatal("estimate displaced precise evidence")
	}
}
