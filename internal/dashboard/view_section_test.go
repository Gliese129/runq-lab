package dashboard

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/views"
)

// Filing a view under a section over HTTP; the view then reports the
// section's experiments as its default set.
func TestViewSectionEndpoint(t *testing.T) {
	s, _, st := scopeFixture(t)
	ctx := context.Background()
	if _, err := st.DB().Exec(`UPDATE projects SET id='pj00000001' WHERE name='p'`); err != nil { // sections key on the project id
		t.Fatal(err)
	}
	if _, err := st.CreateSection(ctx, "p", "ablation"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetJobSection(ctx, "ablation", []string{"exa"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveView(ctx, store.SavedView{Spec: views.Spec{Name: "v", Project: "p"}}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/views/v/section", strings.NewReader(`{"section":"ablation"}`)))
	if rec.Code != 200 {
		t.Fatalf("move: %d %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/projects/p/views/v", nil))
	var got struct {
		InSection   string   `json:"in_section"`
		DefaultJobs []string `json:"default_jobs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	if got.InSection != "ablation" || strings.Join(got.DefaultJobs, ",") != "exa" {
		t.Fatalf("want ablation / [exa], got %+v", got)
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/views/v/section", strings.NewReader(`{"section":"nope"}`)))
	if rec.Code == 200 || !strings.Contains(rec.Body.String(), "runq section create") {
		t.Fatalf("an unknown section must be refused with the fix, got %d %s", rec.Code, rec.Body)
	}
}
