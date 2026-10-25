package backend

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/remote"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
	"gopkg.in/yaml.v3"
)

type planShape struct {
	Name, Project, Baseline string
	Rows                    []string
}

func shapeOf(t *testing.T, cfg job.JobConfig) planShape {
	t.Helper()
	cfg.NormalizeLegacy()
	rows, err := job.Rows(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	out := planShape{Name: cfg.Name, Project: cfg.Project, Baseline: rows[job.BaselineIndex(rows, cfg.Baseline)].Run}
	for _, r := range rows {
		var kv []string
		for k, v := range r.Params {
			kv = append(kv, k+"="+job.FormatValue(v))
		}
		sortStrings(kv)
		out.Rows = append(out.Rows, r.Run+" {"+strings.Join(kv, " ")+"} "+strings.Join(r.NeedsCode, ","))
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// Export → submit again reproduces the plan: rows, run names, params,
// needs_code, baseline and name, for both tasks: and grid jobs.
func TestExportRoundTrip(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	working := t.TempDir()
	lane := &remote.Backend{
		Cfg:   &config.TargetConfig{Name: "a", Scheduler: "sge", DataPath: filepath.Join(t.TempDir(), "data"), SubmitTemplate: "echo 55", SubmitIDRegex: "([0-9]+)", KillTemplate: "true"},
		Store: st, FS: rfs.NewLocalFS(),
	}
	m, err := NewMultiBackend(map[string]Backend{}, st, "")
	if err != nil {
		t.Fatal(err)
	}
	proj := &project.Config{ProjectName: "kl", Target: "a", WorkingDir: working, CmdTemplate: "python t.py {{args}}",
		Params: []project.ParamDef{{Name: "seed"}, {Name: "lr"}}}

	for _, src := range []string{`
project: kl
name: A1 第一批消融
baseline: "B0'"
fixed_params: { lr: 0.000001, seed: s1, std: "on", group: tga-x }
tasks:
  - { run: B0 }
  - { run: "B0'", seed: s2 }
  - { run: R1 lr, lr: 0.000002 }
  - { run: R5 Dr.GRPO, std: "off", needs_code: [std] }
`, `
project: kl
name: kl sweep
fixed_params: { lr: 0.000001 }
sweep:
  - method: grid
    parameters: { kl: [0.0, 0.01], seed: [1, 2] }
`} {
		var cfg job.JobConfig
		if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
			t.Fatal(err)
		}
		want := shapeOf(t, cfg)
		p := *proj
		jobID, _, err := lane.Prepare(ctx, cfg, &p, remote.SubmitOpts{SkipPreflight: true})
		if err != nil {
			t.Fatal(err)
		}
		out, err := m.ExportJob(ctx, jobID[:6])
		if err != nil {
			t.Fatal(err)
		}
		var back job.JobConfig
		if err := yaml.Unmarshal(out, &back); err != nil {
			t.Fatalf("export is not valid job.yaml: %v\n%s", err, out)
		}
		if back.FromJob != jobID || len(back.Sweep) != 0 || len(back.Tasks) == 0 {
			t.Fatalf("export must be tasks: rows from %s:\n%s", jobID, out)
		}
		if got := shapeOf(t, back); !reflect.DeepEqual(got, want) {
			t.Fatalf("round trip changed the plan:\nwant %+v\ngot  %+v\n%s", want, got, out)
		}
		if strings.Contains(string(out), "e-06") {
			t.Fatalf("numbers must be full decimal:\n%s", out)
		}
		t.Logf("export:\n%s", out)
	}
}
