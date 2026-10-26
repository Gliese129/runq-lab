package api

import (
	"context"
	"encoding/json"
	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/job"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func response(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestClientPublishesRunqdProtocolVersion(t *testing.T) {
	client := &Client{httpc: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("X-Runqd-Protocol"); got != MachineProtocolVersion {
			t.Fatalf("machine protocol header = %q, want %q", got, MachineProtocolVersion)
		}
		return response(`{"ready":true}`), nil
	})}}
	resp, err := client.Do(context.Background(), http.MethodGet, "/api/v1/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func TestMachineGPUStatusDecodesStandaloneRunqdShape(t *testing.T) {
	client := &Client{httpc: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(`[{"index":1,"name":"GPU","mem_total":1000,"mem_free":250,"util_pct":80,"attempt_id":"attempt-1","task_id":"attempt-1"}]`), nil
	})}}
	gpus, err := NewProxyFromClient(client).MachineGPUStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(gpus) != 1 || gpus[0].MemTotalMB != 1000 || gpus[0].MemUsedMB != 750 ||
		gpus[0].UtilPercent != 80 || gpus[0].TaskID != "attempt-1" {
		t.Fatalf("machine GPU projection = %+v", gpus)
	}
}

func TestQueryTargetFilterDoesNotChooseSubmitOrPreview(t *testing.T) {
	var bodies []submitWireBody
	client := &Client{httpc: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body submitWireBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, body)
		if strings.HasSuffix(r.URL.Path, "preview") {
			return response(`{"preview":"ok","preflight":{"results":[]}}`), nil
		}
		return response(`{"job_id":"job","total_tasks":1}`), nil
	})}}
	proxy := NewProxyFromClient(client)
	proxy.TargetFilter = "query-only"
	cfg := job.JobConfig{Project: "p", Target: "job-target"}
	if _, _, err := proxy.SubmitJob(context.Background(), cfg, backend.SubmitOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := proxy.PreviewSubmit(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if bodies[0].Target != "" || bodies[1].Target != "" || bodies[1].Config.Target != "job-target" {
		t.Fatalf("query filter changed submission: %+v", bodies)
	}
	proxy.SubmitTarget = "explicit"
	if _, err := proxy.PreviewSubmit(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if bodies[2].Target != "explicit" {
		t.Fatal("explicit preview override lost")
	}
}
