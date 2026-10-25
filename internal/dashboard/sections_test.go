package dashboard

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/store"
)

func TestSectionEndpoints(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m, err := backend.NewMultiBackend(map[string]backend.Backend{}, st, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO projects(name,id,config_json)VALUES('kl','pjaa000001','{}')`); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertJob(ctx, &store.JobRow{ID: "jbaa000001", ProjectName: "kl", Name: "x", ConfigJSON: "{}", Status: "done", Target: "c", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	s := NewServer(m, &config.GlobalConfig{})
	defer s.Close()
	call := func(method, path, body string) (int, string) {
		res := httptest.NewRecorder()
		s.Handler().ServeHTTP(res, httptest.NewRequest(method, path, strings.NewReader(body)))
		return res.Code, res.Body.String()
	}

	if code, body := call("POST", "/api/v1/projects/pjaa/sections", `{"name":"KL 消融"}`); code != 201 {
		t.Fatalf("create by id prefix: %d %s", code, body)
	}
	if code, body := call("POST", "/api/v1/projects/kl/section-rules", `{"section":"KL 消融","kind":"name","pattern":"kl*"}`); code != 201 {
		t.Fatalf("add rule: %d %s", code, body)
	}
	if code, body := call("POST", "/api/v1/jobs/jbaa/section", `{"section":"KL 消融"}`); code != 200 {
		t.Fatalf("move job: %d %s", code, body)
	}
	code, body := call("GET", "/api/v1/projects/kl/sections", "")
	var out struct {
		Sections []store.Section `json:"sections"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &out) != nil || len(out.Sections) != 3 || out.Sections[2].Jobs != 1 || len(out.Sections[2].Rules) != 1 {
		t.Fatalf("list: %d %s", code, body)
	}
	if code, body := call("DELETE", "/api/v1/projects/kl/sections/"+"KL%20%E6%B6%88%E8%9E%8D", ""); code != 200 || !strings.Contains(body, `"unsorted":1`) {
		t.Fatalf("remove: %d %s", code, body)
	}

	// The remote-CLI forward may read sections but not change them.
	guarded := s.RemoteCLIHandler("c")
	res := httptest.NewRecorder()
	guarded.ServeHTTP(res, httptest.NewRequest("POST", "/api/v1/projects/kl/sections", strings.NewReader(`{"name":"x"}`)))
	if res.Code != 403 {
		t.Fatalf("forward must refuse section creation, got %d", res.Code)
	}
}
