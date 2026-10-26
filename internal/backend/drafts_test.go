package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/preflight"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/store"
)

type countingLane struct {
	v2PreviewLane
	submits atomic.Int32
}

func (l *countingLane) SubmitJob(ctx context.Context, cfg job.JobConfig, opts SubmitOptions) (string, int, error) {
	n := l.submits.Add(1)
	l.received = cfg
	if opts.Preflight != nil {
		*opts.Preflight = preflight.Report{Results: []preflight.CheckResult{
			{Name: "entry", Status: "warning", Detail: "t.py does not exist"}, {Name: "data_dirs", Status: "passed"}}}
	}
	return fmt.Sprintf("jb%08d", n), len(cfg.Tasks), nil
}

func newDraftBackend(t *testing.T) (*MultiBackend, *countingLane, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	lane := &countingLane{v2PreviewLane: v2PreviewLane{UnavailableBackend: NewUnavailableBackend(nil)}}
	m, err := NewMultiBackend(map[string]Backend{"a": lane}, st, "a")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CreateProject(context.Background(), project.Config{ProjectName: "kl", Target: "a", WorkingDir: t.TempDir(), CmdTemplate: "python t.py {{args}}"}); err != nil {
		t.Fatal(err)
	}
	return m, lane, st
}

const sweepYAML = `
project: kl
name: kl sweep
fixed_params: { lr: 0.000001 }
sweep:
  - method: grid
    parameters: { kl: [0.0, 0.1], seed: [1, 2] }
`

func TestDraftCreateRetentionAndYAML(t *testing.T) {
	ctx := context.Background()
	m, lane, _ := newDraftBackend(t)
	res, err := m.CreateDraft(ctx, DraftRequest{JobYAML: sweepYAML, SourcePath: "exp/kl.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Draft.Rows != 4 || res.Draft.Rev != 1 || res.Draft.Target != "a" || !strings.Contains(res.Table, "base (baseline)") {
		t.Fatalf("draft = %+v\n%s", res.Draft, res.Table)
	}
	if lane.submits.Load() != 0 {
		t.Fatal("creating a draft must not submit")
	}
	if _, err := m.CreateDraft(ctx, DraftRequest{JobYAML: "name: x\nfixed_params: {a: 1}"}); err == nil || !strings.Contains(err.Error(), "explicit project") {
		t.Fatalf("project must be explicit, got %v", err)
	}

	// The editable YAML is stable: a draft made from it renders identically.
	first, err := m.DraftYAML(ctx, res.Draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.CreateDraft(ctx, DraftRequest{JobYAML: string(first)})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := m.DraftYAML(ctx, again.Draft.ID)
	body := func(b []byte) string {
		s := string(b)
		return s[strings.Index(s, "project:"):]
	}
	if body(first) != body(second) {
		t.Fatalf("draft YAML not stable:\n%s\n---\n%s", first, second)
	}

	// Retention: the 6th open draft discards the oldest.
	for i := 0; i < 4; i++ {
		if res, err = m.CreateDraft(ctx, DraftRequest{JobYAML: sweepYAML}); err != nil {
			t.Fatal(err)
		}
	}
	if len(res.Discarded) != 1 {
		t.Fatalf("6th draft should discard one, got %v", res.Discarded)
	}
	if list, _ := m.ListDrafts(ctx, "kl"); len(list) != store.MaxOpenDrafts {
		t.Fatalf("open drafts = %d", len(list))
	}
	if _, err := m.GetDraft(ctx, res.Discarded[0]); !errors.Is(err, store.ErrDraftNotFound) {
		t.Fatalf("discarded draft should be gone, got %v", err)
	}
}

func TestDraftEditIsRevChecked(t *testing.T) {
	ctx := context.Background()
	m, _, _ := newDraftBackend(t)
	res, err := m.CreateDraft(ctx, DraftRequest{JobYAML: sweepYAML})
	if err != nil {
		t.Fatal(err)
	}
	text, _ := m.DraftYAML(ctx, res.Draft.ID)
	edited := strings.Replace(string(text), "{run: base}", "{run: B0, lr: 0.000003}", 1)

	ok, err := m.EditDraft(ctx, res.Draft.ID, edited)
	if err != nil {
		t.Fatal(err)
	}
	if ok.Draft.Rev != 2 || !ok.Draft.Edited || !strings.Contains(ok.Table, "B0") {
		t.Fatalf("edit not applied: %+v\n%s", ok.Draft, ok.Table)
	}
	if _, err := m.EditDraft(ctx, res.Draft.ID, edited); !errors.Is(err, store.ErrDraftChanged) {
		t.Fatalf("stale rev must be refused, got %v", err)
	}
	bad := strings.Replace(edited, "rev: 1", "rev: 2", 1)
	if _, err := m.EditDraft(ctx, res.Draft.ID, strings.Replace(bad, "{run: seed=2", "{run: B0", 1)); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("invalid plan must be refused, got %v", err)
	}
	if _, err := m.EditDraft(ctx, res.Draft.ID, strings.Replace(bad, "project: kl", "project: other", 1)); err == nil {
		t.Fatal("project is fixed")
	}
	if cur, _ := m.GetDraft(ctx, res.Draft.ID); cur.Draft.Rev != 2 {
		t.Fatalf("refused edits must leave the draft alone, rev %d", cur.Draft.Rev)
	}
}

func TestDraftSubmitIsIdempotent(t *testing.T) {
	ctx := context.Background()
	m, lane, st := newDraftBackend(t)
	res, err := m.CreateDraft(ctx, DraftRequest{JobYAML: sweepYAML})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.SubmitDraft(ctx, res.Draft.ID, DraftSubmitOptions{Rev: 7}); !errors.Is(err, store.ErrDraftChanged) {
		t.Fatalf("wrong rev must be refused, got %v", err)
	}

	var wg sync.WaitGroup
	ids := make([]string, 5)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := m.SubmitDraft(ctx, res.Draft.ID[:5], DraftSubmitOptions{})
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = r.JobID
		}(i)
	}
	wg.Wait()
	if lane.submits.Load() != 1 {
		t.Fatalf("concurrent submits made %d jobs, want 1", lane.submits.Load())
	}
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("submits disagree on the job: %v", ids)
		}
	}
	if lane.received.DraftID != res.Draft.ID || len(lane.received.Tasks) != 4 {
		t.Fatalf("lane got %+v", lane.received)
	}
	again, err := m.SubmitDraft(ctx, res.Draft.ID, DraftSubmitOptions{})
	if err != nil || !again.AlreadySubmitted || again.JobID != ids[0] {
		t.Fatalf("resubmit = %+v, %v", again, err)
	}
	text, _ := m.DraftYAML(ctx, res.Draft.ID)
	if _, err := m.EditDraft(ctx, res.Draft.ID, string(text)); !errors.Is(err, store.ErrDraftSubmitted) {
		t.Fatalf("a submitted draft is frozen, got %v", err)
	}
	if err := m.DiscardDraft(ctx, res.Draft.ID); !errors.Is(err, store.ErrDraftSubmitted) {
		t.Fatalf("a submitted draft cannot be discarded, got %v", err)
	}
	if n, _ := st.CountOpenDrafts(ctx); n != 0 {
		t.Fatalf("open drafts = %d", n)
	}
}

// Drafts run preflight (they used to skip it): the result carries every
// check, and a submit hands back its warnings without blocking.
func TestDraftRunsPreflightAndSubmitWarns(t *testing.T) {
	ctx := context.Background()
	m, _, _ := newDraftBackend(t)
	res, err := m.CreateDraft(ctx, DraftRequest{JobYAML: sweepYAML})
	if err != nil {
		t.Fatal(err)
	}
	var entry *preflight.CheckResult
	for i, c := range res.Preflight {
		if c.Name == "entry" {
			entry = &res.Preflight[i]
		}
	}
	if entry == nil || entry.Status != "warning" || !strings.Contains(entry.Detail, "t.py does not exist") {
		t.Fatalf("draft preflight: %+v", res.Preflight)
	}

	sub, err := m.SubmitDraft(ctx, res.Draft.ID, DraftSubmitOptions{})
	if err != nil {
		t.Fatalf("warnings must not block the submit: %v", err)
	}
	if len(sub.PreflightWarnings) != 1 || sub.PreflightWarnings[0].Name != "entry" {
		t.Fatalf("submit warnings: %+v", sub.PreflightWarnings)
	}
}
