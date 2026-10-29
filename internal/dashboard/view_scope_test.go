package dashboard

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/views"
	"github.com/gliese129/runq-lab/internal/workspace"
)

// scopeHistorian answers history reads with fixed data and records which
// sources were read, so a test can see a view's curves and frames scope.
type scopeHistorian struct {
	*backend.MultiBackend
	mu    sync.Mutex
	reads map[string]bool
}

func (h *scopeHistorian) read(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reads[id] = true
}

func (h *scopeHistorian) sources() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []string{}
	for id := range h.reads {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (h *scopeHistorian) SourceHistory(_ context.Context, id, _, _ string, _ int) (*workspace.ExactHistory, error) {
	h.read(id)
	return &workspace.ExactHistory{Frames: []workspace.ExactFrame{{At: 1}, {At: 2}},
		Ranges: map[string][2]float64{"eval": {1, 2}, "train": {1, 1000000}},
		Series: []workspace.ExactSeries{{Key: "loss", Step: []float64{1, 2}, Value: []float64{.5, .2}, Signature: "same"}}}, nil
}

func (h *scopeHistorian) SourceFramesAt(_ context.Context, id, _ string, ats []float64) (*workspace.ExactHistory, error) {
	h.read(id)
	out := &workspace.ExactHistory{}
	for _, at := range ats {
		out.Frames = append(out.Frames, workspace.ExactFrame{At: at, Records: []workspace.ExactRecord{{TS: 1, Section: "eval", Axes: map[string]any{}, Metrics: map[string]any{"acc": .5}}}})
	}
	return out, nil
}

// scopeFixture: experiments exa (lr=0.1) and exb (lr=0.2), one cell each;
// exa's cell also has an older version (tkold) superseded by tkexa, and
// tkexa has a scoring script's results (tkexa@judge).
func scopeFixture(t *testing.T) (*Server, *scopeHistorian, *store.Store) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.DB().Exec(`INSERT INTO projects(name,config_json) VALUES('p','{}')`); err != nil {
		t.Fatal(err)
	}
	insert := func(id, job, cell, params string) {
		if err := st.InsertTask(ctx, &store.TaskRow{ID: id, JobID: job, ProjectName: "p", RunName: cell, Command: "x", ParamsJSON: params, Status: "success", EnqueuedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"exa", "exb"} {
		if err := st.InsertJob(ctx, &store.JobRow{ID: id, ProjectName: "p", ConfigJSON: "{}", Status: "done", CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	insert("tkold", "exa", "exa", `{"lr":0.1}`)
	insert("tkexa", "exa", "exa", `{"lr":0.1}`)
	insert("tkexb", "exb", "exb", `{"lr":0.2}`)
	for _, q := range []string{
		`UPDATE tasks SET superseded_by='tkexa' WHERE id='tkold'`,
		`INSERT INTO file_ingest(task_id,file) VALUES('tkexa','results.judge')`,
	} {
		if _, err := st.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	m, err := backend.NewMultiBackend(map[string]backend.Backend{}, st, "")
	if err != nil {
		t.Fatal(err)
	}
	h := &scopeHistorian{MultiBackend: m, reads: map[string]bool{}}
	s := NewServer(h, &config.GlobalConfig{})
	t.Cleanup(s.Close)
	return s, h, st
}

func saveScopeView(t *testing.T, st *store.Store, spec views.Spec) {
	t.Helper()
	spec.Name, spec.Project, spec.Jobs = "v", "p", []string{"exa", "exb"}
	if err := st.SaveView(context.Background(), store.SavedView{Spec: spec}); err != nil {
		t.Fatal(err)
	}
}

func serveView(t *testing.T, s *Server, url string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
	if rec.Code != 200 {
		t.Fatalf("GET %s: %d %s", url, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func curveTasks(t *testing.T, body string) []string {
	t.Helper()
	var out struct {
		Series []struct {
			Tasks []string `json:"tasks"`
		} `json:"series"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	tasks := []string{}
	for _, s := range out.Series {
		tasks = append(tasks, s.Tasks...)
	}
	sort.Strings(tasks)
	return tasks
}

func same(a, b []string) bool { return strings.Join(a, ",") == strings.Join(b, ",") }

func curvesSpec(filter map[string][]any) views.Spec {
	return views.Spec{Filter: filter, Charts: []map[string]any{{"template": "curves", "metric": "loss", "color": "param.lr"}}}
}

// A saved view's curves read the table's scope: its filter keeps lr=0.1
// out of lr=0.2, and a superseded version is history.
func TestViewCurvesFollowFilter(t *testing.T) {
	s, h, st := scopeFixture(t)
	saveScopeView(t, st, curvesSpec(map[string][]any{"param.lr": {0.1}}))
	body := serveView(t, s, "/api/v1/projects/p/views/v/charts/0/series")
	if got := curveTasks(t, body); !same(got, []string{"tkexa"}) {
		t.Fatalf("curves of filter lr=0.1 drew %v: %s", got, body)
	}
	if got := h.sources(); !same(got, []string{"tkexa"}) {
		t.Fatalf("curves read %v", got)
	}
}

func TestViewCurvesFollowCellFilterAndJobOverride(t *testing.T) {
	s, _, st := scopeFixture(t)
	saveScopeView(t, st, curvesSpec(map[string][]any{"cell": {"exb"}}))
	if got := curveTasks(t, serveView(t, s, "/api/v1/projects/p/views/v/charts/0/series")); !same(got, []string{"tkexb"}) {
		t.Fatalf("curves of filter cell=exb drew %v", got)
	}
	saveScopeView(t, st, curvesSpec(nil))
	if got := curveTasks(t, serveView(t, s, "/api/v1/projects/p/views/v/charts/0/series?job=exa")); !same(got, []string{"tkexa"}) {
		t.Fatalf("curves of ?job=exa drew %v", got)
	}
}

func framesSpec(filter map[string][]any, producer *string) views.Spec {
	return views.Spec{Section: "eval", Rows: []string{"cell"}, Filter: filter, Producer: producer,
		Columns: []views.Column{{Name: "acc", Value: "acc"}},
		Charts:  []map[string]any{{"template": "compare", "x": "cell", "y": "acc", "color": "cell"}}}
}

func frameJobs(t *testing.T, body string) []string {
	t.Helper()
	var out struct {
		Frames []struct {
			Rows []views.Row `json:"rows"`
		} `json:"frames"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Frames) == 0 {
		t.Fatalf("no frames: %s", body)
	}
	seen := map[string]bool{}
	for _, f := range out.Frames {
		for _, r := range f.Rows {
			seen[r.JobID] = true
		}
	}
	jobs := []string{}
	for id := range seen {
		jobs = append(jobs, id)
	}
	sort.Strings(jobs)
	return jobs
}

// The animation of a view limited to exa (?job=exa, as the dashboard asks
// when the table is) holds exa alone, as the table does.
func TestViewFramesFollowJobOverride(t *testing.T) {
	s, h, st := scopeFixture(t)
	saveScopeView(t, st, framesSpec(nil, nil))
	body := serveView(t, s, "/api/v1/projects/p/views/v/frames?job=exa&count=2")
	if got := frameJobs(t, body); !same(got, []string{"exa"}) {
		t.Fatalf("frames?job=exa hold %v: %s", got, body)
	}
	if got := h.sources(); !same(got, []string{"tkexa", "tkexa@judge"}) {
		t.Fatalf("frames read %v", got)
	}
	table := serveView(t, s, "/api/v1/projects/p/views/v/render?job=exa")
	if strings.Contains(table, "exb") {
		t.Fatalf("table of ?job=exa holds exb: %s", table)
	}
}

func TestViewFramesFollowFilterAndProducer(t *testing.T) {
	s, h, st := scopeFixture(t)
	own := ""
	saveScopeView(t, st, framesSpec(map[string][]any{"param.lr": {0.1}}, &own))
	body := serveView(t, s, "/api/v1/projects/p/views/v/frames?count=2")
	if got := frameJobs(t, body); !same(got, []string{"exa"}) {
		t.Fatalf("frames of filter lr=0.1 hold %v", got)
	}
	// filter drops exb's versions; producer "" drops the judge's results
	if got := h.sources(); !same(got, []string{"tkexa"}) {
		t.Fatalf("frames read %v", got)
	}
	// frame rows name their version as the table's do
	if !strings.Contains(body, `"version":2`) {
		t.Fatalf("frame contributors lack the version: %s", body)
	}
}

// A view reading one section frames that section's span: another section
// reaching step 1,000,000 does not stretch it.
func TestViewFramesSpanTheViewsSection(t *testing.T) {
	s, _, st := scopeFixture(t)
	last := func() float64 {
		var out struct {
			Frames []struct {
				At float64 `json:"at"`
			} `json:"frames"`
		}
		if err := json.Unmarshal([]byte(serveView(t, s, "/api/v1/projects/p/views/v/frames?count=5")), &out); err != nil {
			t.Fatal(err)
		}
		return out.Frames[len(out.Frames)-1].At
	}
	saveScopeView(t, st, framesSpec(nil, nil))
	if at := last(); at != 2 {
		t.Fatalf("eval view ends at %v, want 2", at)
	}
	every := framesSpec(nil, nil)
	every.Section = ""
	saveScopeView(t, st, every)
	if at := last(); at != 1000000 {
		t.Fatalf("a view of every section ends at %v, want 1000000", at)
	}
}
