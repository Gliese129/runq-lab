package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/store"
)

func TestWriteErrorAmbiguousAndNothingToAppend(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, fmt.Errorf("get: %w", &store.AmbiguousRefError{Kind: "job", Ref: "jb65", IDs: []string{"jb6573a", "jb6574b"}}))
	var body backend.ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusConflict || body.Code != backend.CodeAmbiguous || len(body.Candidates) != 2 {
		t.Fatalf("ambiguous → %d %+v", rec.Code, body)
	}
	rec = httptest.NewRecorder()
	writeError(rec, job.NothingToAppendError(job.AppendSummary{Into: "jb1", Cells: make([]job.CellClass, 3)}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("nothing to append → %d", rec.Code)
	}
}
