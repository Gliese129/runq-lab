package dashboard

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/store"
)

func TestDraftRoutesAndForwardOwnership(t *testing.T) {
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
	row := &store.DraftRow{Project: "kl", Target: "c", ConfigJSON: `{"project":"kl","tasks":[{"run":"B0"}]}`}
	if _, err := st.CreateDraft(ctx, row); err != nil {
		t.Fatal(err)
	}
	s := NewServer(m, &config.GlobalConfig{})
	defer s.Close()

	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, httptest.NewRequest("GET", "/api/v1/drafts/drnope", nil))
	if res.Code != 404 || !strings.Contains(res.Body.String(), `"not_found"`) {
		t.Fatalf("missing draft: %d %s", res.Code, res.Body.String())
	}
	res = httptest.NewRecorder()
	s.Handler().ServeHTTP(res, httptest.NewRequest("GET", "/api/v1/drafts/"+row.ID, nil))
	if res.Code != 200 || !strings.Contains(res.Body.String(), "B0 (baseline)") {
		t.Fatalf("get draft: %d %s", res.Code, res.Body.String())
	}

	// Through another target's forward the draft is out of reach.
	res = httptest.NewRecorder()
	s.RemoteCLIHandler("d").ServeHTTP(res, httptest.NewRequest("POST", "/api/v1/drafts/"+row.ID+"/submit", strings.NewReader("{}")))
	if res.Code != 403 {
		t.Fatalf("foreign forward must not submit the draft: %d %s", res.Code, res.Body.String())
	}
	res = httptest.NewRecorder()
	s.RemoteCLIHandler("c").ServeHTTP(res, httptest.NewRequest("GET", "/api/v1/drafts/"+row.ID, nil))
	if res.Code != 200 {
		t.Fatalf("own forward may read its draft: %d %s", res.Code, res.Body.String())
	}
}
