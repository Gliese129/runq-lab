package views

import (
	"strings"
	"testing"
	"time"
)

func TestTextHeaderIsReadable(t *testing.T) {
	gen := time.Date(2026, 10, 14, 12, 0, 0, 0, time.Local).Unix()
	observed := gen - 120
	r := &Result{
		Title: "eval", GeneratedAt: gen, RowKeys: []string{},
		Sources: []Source{{
			JobID: "jb1", Note: "n", Status: "running", ObservationState: "ok", ObservedAt: &observed,
			Attention: []map[string]any{
				{"task_id": "tk1", "status": "failed", "reason": "exit 1"},
				{"consumer_id": "cs1", "status": "lost", "reason": "heartbeat expired", "host": "r7i3n5", "pid": 42},
				{"job_id": "jb1", "reason": "observation unconfirmed"},
			},
		}},
		Extra: []ExtraCell{{Name: "generated", Text: "2176/8280", Agg: "sum"}},
	}
	out, err := Export(r, "text", false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"eval · 2026-10-14 12:00:00\n",
		"observation: ok (",
		"  ! tk1 failed: exit 1\n",
		"  ! cs1 lost: heartbeat expired (host r7i3n5, pid 42)\n",
		"  ! jb1: observation unconfirmed\n",
		"generated[sum] = 2176/8280",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "{") || strings.Contains(out, "(ok)") {
		t.Errorf("raw JSON or duplicated state in:\n%s", out)
	}
}
