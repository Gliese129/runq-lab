package backend

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/store"
	"gopkg.in/yaml.v3"
)

// seedSecondSubmission adds submission 2 (target b, python b.py, timeout
// 3h) to a job seeded by seedJob, with one new version of cell rows[i].
func seedSecondSubmission(t *testing.T, st *store.Store, rows []store.TaskRow, i int, status string) {
	t.Helper()
	ctx := context.Background()
	jobID := rows[0].JobID
	if _, err := st.DB().Exec(`UPDATE job_batches SET command = 'python t.py {{args}}' WHERE job_id = ? AND seq = 1`, jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO job_batches (job_id, seq, submitted_at, name, command, target, task_count, config_json)
		VALUES (?, 2, 1, 'kl sweep', 'python b.py', 'b', 1, '{"target":"b","command":"python b.py","overrides":{"timeout":"3h"}}')`, jobID); err != nil {
		t.Fatal(err)
	}
	v := rows[i]
	v.ID, v.BatchSeq, v.Target, v.Status, v.EnqueuedAt = jobID+"v2", 2, "b", status, time.Now()
	if err := st.InsertTask(ctx, &v); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.RefreshJob(ctx, jobID, false); err != nil {
		t.Fatal(err)
	}
}

func exportDoc(t *testing.T, m *MultiBackend, ref string) (string, map[string]any) {
	t.Helper()
	raw, err := m.ExportJob(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return string(raw), doc
}

func cellLine(raw, cell string) string {
	for _, line := range strings.Split(raw, "\n") {
		if strings.Contains(line, "{cell: "+cell+",") || strings.Contains(line, "{cell: "+cell+"}") || strings.Contains(line, "{cell: '"+cell+"'") {
			return line
		}
	}
	return ""
}

// The file's command, target and overrides all come from the latest
// submission; a row from the earlier one names its own in a comment.
func TestExportTakesOneSubmissionPerRow(t *testing.T) {
	m, _, st := newDraftBackend(t)
	rows := seedJob(t, st, "ex11112222", "success", "failed")
	seedSecondSubmission(t, st, rows, 1, "success")

	raw, doc := exportDoc(t, m, "ex11112222")
	if doc["target"] != "b" || doc["command"] != "python b.py" {
		t.Fatalf("file target/command = %v / %v, want b / python b.py:\n%s", doc["target"], doc["command"], raw)
	}
	if o, _ := doc["overrides"].(map[string]any); o["timeout"] != "3h" {
		t.Fatalf("file overrides = %v, want timeout 3h:\n%s", doc["overrides"], raw)
	}
	first := cellLine(raw, rows[0].RunName)
	if !strings.Contains(first, "command: python t.py {{args}}") || !strings.Contains(first, "target: a") || !strings.Contains(first, "overrides: {}") {
		t.Fatalf("row from submission 1 = %q, want its command, target and overrides:\n%s", first, raw)
	}
	if second := cellLine(raw, rows[1].RunName); second == "" || strings.Contains(second, "#") {
		t.Fatalf("row from submission 2 = %q, want no comment:\n%s", second, raw)
	}
}

// The audit's case: the latest submission ran python b.py on b, but every
// shown version came from the first one on a. The file never pairs b's
// command with a's target: it is b's throughout, and each row says it ran
// a's command on a.
func TestExportDoesNotMixSubmissions(t *testing.T) {
	m, _, st := newDraftBackend(t)
	rows := seedJob(t, st, "ex11112222", "success", "success")
	seedSecondSubmission(t, st, rows, 1, "failed")

	raw, doc := exportDoc(t, m, "ex11112222")
	if doc["target"] != "b" || doc["command"] != "python b.py" {
		t.Fatalf("file target/command = %v / %v, want b / python b.py:\n%s", doc["target"], doc["command"], raw)
	}
	for _, r := range rows {
		line := cellLine(raw, r.RunName)
		if !strings.Contains(line, "command: python t.py {{args}}") || !strings.Contains(line, "target: a") {
			t.Fatalf("row %s = %q, want submission 1's command and target:\n%s", r.RunName, line, raw)
		}
	}
}

// A submission whose command was never recorded (before runq stored the
// template) is exported as such — not as today's project command.
func TestExportSaysAnUnrecordedCommand(t *testing.T) {
	m, _, st := newDraftBackend(t)
	rows := seedJob(t, st, "ex33334444", "success", "failed")
	seedSecondSubmission(t, st, rows, 1, "success")
	if _, err := st.DB().Exec(`UPDATE job_batches SET command = '', config_json = '{}' WHERE job_id = 'ex33334444' AND seq = 1`); err != nil {
		t.Fatal(err)
	}
	raw, _ := exportDoc(t, m, "ex33334444")
	if first := cellLine(raw, rows[0].RunName); !strings.Contains(first, "command: (not recorded)") {
		t.Fatalf("row from the unrecorded submission = %q:\n%s", first, raw)
	}
	// an unsorted experiment stays unsorted when the file is submitted
	// again (the section rules would otherwise re-sort it)
	if !strings.Contains(raw, "section: unsorted") {
		t.Fatalf("export drops section unsorted:\n%s", raw)
	}
}
