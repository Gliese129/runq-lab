package dashboard

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/views"
)

// GET /jobs/{id}/view renders a job with no saved view: a short id
// resolves, superseded tasks stay out, rows are one per cell.
func TestJobViewRoute(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.DB().Exec(`INSERT INTO projects (name, config_json) VALUES ('p', '{}')`); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertJob(ctx, &store.JobRow{ID: "jbview123", ProjectName: "p", ConfigJSON: "{}", Status: "success", TotalTasks: 2, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, tk := range []struct{ id, params string }{{"tka", `{"lr":0.1}`}, {"tkb", `{"lr":0.2}`}, {"tkold", `{"lr":0.2}`}} {
		if err := st.InsertTask(ctx, &store.TaskRow{ID: tk.id, JobID: "jbview123", ProjectName: "p", Command: "x", ParamsJSON: tk.params, Status: "success", EnqueuedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		rec := store.ResultRecordRow{TaskID: tk.id, JobID: "jbview123", TS: 1, AxesJSON: `{"split":"test"}`, MetricsJSON: `{"acc":0.5}`}
		if err := st.ApplyResultsIngestDelta(ctx, tk.id, false, []store.ResultRecordRow{rec}, 1, store.FileIngestMark{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.DB().Exec(`UPDATE tasks SET superseded_by = 'tkb', status = 'failed' WHERE id = 'tkold'`); err != nil {
		t.Fatal(err)
	}
	m, err := backend.NewMultiBackend(map[string]backend.Backend{}, st, "")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(m, &config.GlobalConfig{})
	defer s.Close()
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, httptest.NewRequest("GET", "/api/v1/jobs/jbview/view", nil))
	if res.Code != 200 {
		t.Fatalf("status %d: %s", res.Code, res.Body.String())
	}
	var out views.JobView
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.JobID != "jbview123" || len(out.Views) != 1 || len(out.Views[0].Rows) != 2 {
		t.Fatalf("job view = %s", res.Body.String())
	}
	res = httptest.NewRecorder()
	s.Handler().ServeHTTP(res, httptest.NewRequest("GET", "/api/v1/jobs/jbnope/view", nil))
	if res.Code != 404 {
		t.Fatalf("unknown job: %d %s", res.Code, res.Body.String())
	}
}
