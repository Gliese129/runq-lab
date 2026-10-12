package dashboard

import (
	"encoding/json"
	"errors"
	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/config"
	"net/http/httptest"
	"testing"
)

func TestForwardHealthProvesControllerAndTarget(t *testing.T) {
	s := NewServer(backend.NewUnavailableBackend(errors.New("offline")), &config.GlobalConfig{})
	s.SetControllerID("123456789abc")
	for _, target := range []string{"a", "b"} {
		rr := httptest.NewRecorder()
		s.RemoteCLIHandler(target).ServeHTTP(rr, httptest.NewRequest("GET", "/api/v1/health", nil))
		var body map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if rr.Code != 200 || body["controller_id"] != "123456789abc" || body["forward_target"] != target {
			t.Fatalf("incorrect identity: %s", rr.Body.String())
		}
	}
}
