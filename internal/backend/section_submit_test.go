package backend

import (
	"context"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/store"
)

func TestSubmitResolvesSection(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lane := &v2PreviewLane{UnavailableBackend: NewUnavailableBackend(nil)}
	m, err := NewMultiBackend(map[string]Backend{"a": lane}, st, "a")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CreateProject(ctx, project.Config{ProjectName: "kl", Target: "a", WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSection(ctx, "kl", "main"); err != nil {
		t.Fatal(err)
	}
	mainID, _ := st.ResolveSection(ctx, "kl", "main")
	smokeID, _ := st.ResolveSection(ctx, "kl", store.SectionSmoke)

	for _, c := range []struct {
		cfg  job.JobConfig
		want string
	}{
		{job.JobConfig{Project: "kl", Name: "A1 原始基线", Section: "main"}, mainID},
		{job.JobConfig{Project: "kl", Note: "a1 launch smoke"}, smokeID}, // default rule, legacy key
		{job.JobConfig{Project: "kl", Name: "A1 原始基线"}, ""},
	} {
		if _, _, err := m.SubmitJob(ctx, c.cfg, SubmitOptions{}); err != nil {
			t.Fatal(err)
		}
		if lane.received.SectionID != c.want {
			t.Errorf("%+v: section id %q, want %q", c.cfg, lane.received.SectionID, c.want)
		}
	}
	if _, _, err := m.SubmitJob(ctx, job.JobConfig{Project: "kl", Section: "nope"}, SubmitOptions{}); err == nil || !strings.Contains(err.Error(), "runq section create") {
		t.Fatalf("unknown section must fail with the create command, got %v", err)
	}
}
