package dashboard

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/store"
)

// killLane counts the kills a target's execution lane receives.
type killLane struct {
	*backend.UnavailableBackend
	jobKills, taskKills int
}

func (l *killLane) KillJob(context.Context, string) error  { l.jobKills++; return nil }
func (l *killLane) KillTask(context.Context, string) error { l.taskKills++; return nil }

// guardFixture: targets a and b; exaa000001 runs on a only, exbb000001 on b
// only, exab000001 started on a and has a later version on b.
func guardFixture(t *testing.T) (*Server, *store.Store, *killLane, *killLane) {
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
	a := &killLane{UnavailableBackend: backend.NewUnavailableBackend(nil)}
	b := &killLane{UnavailableBackend: backend.NewUnavailableBackend(nil)}
	m, err := backend.NewMultiBackend(map[string]backend.Backend{"a": a, "b": b}, st, "a")
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range []struct{ id, target string }{{"exaa000001", "a"}, {"exbb000001", "b"}, {"exab000001", "a"}} {
		if err := st.InsertJob(ctx, &store.JobRow{ID: j.id, ProjectName: "p", Name: "orig", ConfigJSON: "{}", Status: "running", Target: j.target, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tk := range []struct{ id, job, run, target string }{
		{"tkaa000001", "exaa000001", "c1", "a"},
		{"tkbb000001", "exbb000001", "c1", "b"},
		{"tkab000001", "exab000001", "c1", "a"},
		{"tkab000002", "exab000001", "c2", "b"},
	} {
		if err := st.InsertTask(ctx, &store.TaskRow{ID: tk.id, JobID: tk.job, RunName: tk.run, ProjectName: "p", Command: "x", ParamsJSON: "{}", Status: "running", Target: tk.target, EnqueuedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	s := NewServer(m, &config.GlobalConfig{})
	t.Cleanup(s.Close)
	return s, st, a, b
}

func call(t *testing.T, s *Server, forward, method, path, body string) (int, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	h := s.Handler()
	if forward != "" {
		h = s.RemoteCLIHandler(forward)
	}
	h.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rr.Code, rr.Body.String()
}

// Audit P1: the guard looked up only full ids while the handler resolved
// prefixes, so another target's job was renamable by its short id.
func TestForwardShortIDCannotReachOtherTarget(t *testing.T) {
	s, st, _, _ := guardFixture(t)
	for _, ref := range []string{"exbb000001", "exbb", "exbb0"} {
		if code, body := call(t, s, "a", "POST", "/api/v1/jobs/"+ref+"/rename", `{"new_name":"via-a"}`); code != 403 {
			t.Fatalf("%s through a: %d %s", ref, code, body)
		}
	}
	if j, _ := st.GetJob(context.Background(), "exbb000001"); j.Name != "orig" {
		t.Fatalf("b's job renamed through a: %q", j.Name)
	}
	// The same short form for a's own job is admitted and acts on that job.
	if code, body := call(t, s, "a", "POST", "/api/v1/jobs/exaa/rename", `{"new_name":"mine"}`); code != 200 || !strings.Contains(body, "exaa000001") {
		t.Fatalf("own short id: %d %s", code, body)
	}
}

// Audit P1: a job whose first target is a but which has a version on b
// was killed through a's forward on both lanes.
func TestForwardRefusesCrossTargetExperiment(t *testing.T) {
	s, st, a, b := guardFixture(t)
	for _, ref := range []string{"exab000001", "exab"} {
		code, body := call(t, s, "a", "POST", "/api/v1/jobs/"+ref+"/kill", "")
		if code != 403 || !strings.Contains(body, "other targets (b)") {
			t.Fatalf("kill %s through a: %d %s", ref, code, body)
		}
	}
	if code, body := call(t, s, "a", "POST", "/api/v1/jobs/exab/rename", `{"new_name":"via-a"}`); code != 403 {
		t.Fatalf("rename mixed through a: %d %s", code, body)
	}
	if code, _ := call(t, s, "b", "GET", "/api/v1/jobs/exab000001", ""); code != 403 {
		t.Fatalf("read mixed through b: %d", code)
	}
	if a.jobKills != 0 || b.jobKills != 0 {
		t.Fatalf("lanes reached: a=%d b=%d", a.jobKills, b.jobKills)
	}
	if j, _ := st.GetJob(context.Background(), "exab000001"); j.Name != "orig" {
		t.Fatalf("mixed job renamed: %q", j.Name)
	}
	// a's own version of it stays reachable by task id; b's is not.
	if code, body := call(t, s, "a", "POST", "/api/v1/tasks/tkab000001/kill", ""); code != 200 || a.taskKills != 1 {
		t.Fatalf("own version kill: %d %s", code, body)
	}
	if code, _ := call(t, s, "a", "POST", "/api/v1/tasks/tkab000002/kill", ""); code != 403 || b.taskKills != 0 {
		t.Fatalf("foreign version kill: %d b=%d", code, b.taskKills)
	}
	// The controller still kills the whole experiment on every lane.
	if code, body := call(t, s, "", "POST", "/api/v1/jobs/exab/kill", ""); code != 200 || a.jobKills != 1 || b.jobKills != 1 {
		t.Fatalf("controller kill: %d %s a=%d b=%d", code, body, a.jobKills, b.jobKills)
	}
}

// <experiment>/<cell>[@vN] arrives as one escaped segment; the guard used
// to split it at the %2F, look up "exbb000001" as a task id, find nothing
// and pass the request through.
func TestForwardVersionRefResolvesLikeHandler(t *testing.T) {
	s, _, _, b := guardFixture(t)
	for _, ref := range []string{"exbb000001%2Fc1", "exbb%2Fc1%40v1", "tkbb"} {
		if code, body := call(t, s, "a", "POST", "/api/v1/tasks/"+ref+"/kill", ""); code != 403 {
			t.Fatalf("%s through a: %d %s", ref, code, body)
		}
	}
	if b.taskKills != 0 {
		t.Fatalf("b's lane reached %d times", b.taskKills)
	}
	if code, body := call(t, s, "a", "GET", "/api/v1/tasks/exbb%2Fc1", ""); code != 403 || strings.Contains(body, "tkbb000001") {
		t.Fatalf("read b's version through a: %d %s", code, body)
	}
	if code, body := call(t, s, "a", "GET", "/api/v1/tasks/exaa%2Fc1", ""); code != 200 || !strings.Contains(body, "tkaa000001") {
		t.Fatalf("own version ref: %d %s", code, body)
	}
}

func TestForwardRefusesUnknownAndAmbiguousIDs(t *testing.T) {
	s, _, _, _ := guardFixture(t)
	if code, body := call(t, s, "a", "POST", "/api/v1/jobs/exzz/kill", ""); code != 404 {
		t.Fatalf("unknown: %d %s", code, body)
	}
	code, body := call(t, s, "a", "POST", "/api/v1/jobs/ex/rename", `{"new_name":"x"}`)
	if code != 409 || strings.Contains(body, "exbb000001") {
		t.Fatalf("ambiguous must be refused without naming other targets' ids: %d %s", code, body)
	}
	// LIKE wildcards in a typed ref are literal characters.
	for _, ref := range []string{"ex%25", "ex__000001", "exbb_"} {
		if code, body := call(t, s, "a", "POST", "/api/v1/jobs/"+ref+"/rename", `{"new_name":"x"}`); code != 404 {
			t.Fatalf("%s: %d %s", ref, code, body)
		}
	}
}

// The controller's handlers get the same resolution: a short id works for
// every job/task route, an ambiguous one is a 409 listing candidates.
func TestHandlerCanonicalizesRefs(t *testing.T) {
	s, _, _, b := guardFixture(t)
	if code, body := call(t, s, "", "POST", "/api/v1/jobs/exbb/kill", ""); code != 200 || b.jobKills != 1 {
		t.Fatalf("short-id kill: %d %s", code, body)
	}
	if code, body := call(t, s, "", "POST", "/api/v1/tasks/exbb%2Fc1/kill", ""); code != 200 || b.taskKills != 1 {
		t.Fatalf("version-ref kill: %d %s", code, body)
	}
	if code, body := call(t, s, "", "POST", "/api/v1/jobs/ex/kill", ""); code != 409 || !strings.Contains(body, "exbb000001") {
		t.Fatalf("ambiguous: %d %s", code, body)
	}
}
