package submitplan

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/project"
	"gopkg.in/yaml.v3"
)

func TestV2EffectivePlanAndWireOverrides(t *testing.T) {
	source := &project.Config{ProjectName: "p", WorkingDir: t.TempDir(), CmdTemplate: "echo old", Environment: map[string]string{"A": "old"}, Preflight: &project.PreflightConfig{HF: []string{"old/repo"}}, Params: []project.ParamDef{{Name: "h_rt", Scope: "scheduler"}}}
	var cfg job.JobConfig
	if err := yaml.Unmarshal([]byte("project: p\ncommand: echo {{seed}}\npreflight: {enabled: false, imports: false, hf: []}\nfixed_params: {seed: 1, lr: 0.1, h_rt: '4:00:00'}\noverrides: {env: {A: new}, timeout: 2h}"), &cfg); err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded job.JobConfig
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	plan, err := Build(context.Background(), decoded, source, Deps{Paths: Paths{WorkspaceRoot: "<workspace>", LogRoot: "<workspace>"}})
	if err != nil {
		t.Fatal(err)
	}
	effective := plan.EffectiveConfig
	// rc6: job-level preflight is retired — the project's settings stand.
	if len(effective.Preflight.HF) != 1 {
		t.Fatalf("job-level preflight must not override the project: %+v", effective.Preflight)
	}
	if plan.Tasks[0].Command != "echo 1" || plan.Tasks[0].Params["lr"] == nil || plan.Tasks[0].Timeout != 7200 || plan.Env["A"] != "new" || !reflect.DeepEqual(plan.UnconsumedParams, []string{"lr"}) {
		t.Fatalf("wrong plan: %+v", plan)
	}
	if source.CmdTemplate != "echo old" || source.Environment["A"] != "old" || len(source.Preflight.HF) != 1 {
		t.Fatal("shared project mutated")
	}
	decoded.Command = ""
	source.CmdTemplate = ""
	if _, err := Build(context.Background(), decoded, source, Deps{}); err == nil {
		t.Fatal("missing command accepted")
	}
	decoded.Command = "echo {{missing}}"
	if _, err := Build(context.Background(), decoded, source, Deps{}); err == nil || !strings.Contains(err.Error(), "unknown parameter") {
		t.Fatalf("missing explicit placeholder: %v", err)
	}
}

func TestV2ChoicesIgnoreOldStrictAndDefaults(t *testing.T) {
	for _, strict := range []string{"", ", strict: false", ", strict: true"} {
		var source project.Config
		if err := yaml.Unmarshal([]byte("project_name: p\ncommand: echo hi\nparams: [{name: lr, default: 99, choices: ['0.1']"+strict+"}]"), &source); err != nil {
			t.Fatal(err)
		}
		source.WorkingDir = t.TempDir()
		cfg := job.JobConfig{Project: "p", FixedParams: map[string]any{"lr": 0.2}}
		if _, err := Build(context.Background(), cfg, &source, Deps{}); err == nil {
			t.Fatalf("outside choices accepted with %q", strict)
		}
		cfg.FixedParams = nil
		plan, err := Build(context.Background(), cfg, &source, Deps{})
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := plan.Tasks[0].Params["lr"]; exists {
			t.Fatal("legacy default injected")
		}
	}
}

func TestV2ScalarPreflightFalse(t *testing.T) {
	var cfg job.JobConfig
	if err := yaml.Unmarshal([]byte("project: p\ncommand: echo hi\npreflight: false"), &cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(cfg)
	var back job.JobConfig
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	source := &project.Config{ProjectName: "p", WorkingDir: t.TempDir()}
	if _, err := Build(context.Background(), back, source, Deps{}); err != nil {
		t.Fatalf("preflight false failed: %v", err)
	}
}

// Preflight checks the job's command (overrides win), not the project's.
func TestV2PreflightUsesOverriddenCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "new.py"), []byte("import sys\n"), 0600); err != nil {
		t.Fatal(err)
	}
	source := &project.Config{ProjectName: "p", WorkingDir: dir, CmdTemplate: "python old.py"}
	cfg := job.JobConfig{Project: "p", Command: "python new.py"}
	plan, err := Build(context.Background(), cfg, source, Deps{Paths: Paths{WorkspaceRoot: dir, LogRoot: dir}})
	if err != nil {
		t.Fatal(err)
	}
	if r := plan.Preflight.Results[0]; r.Name != "entry" || r.Status != "passed" || !strings.Contains(r.Detail, "new.py") {
		t.Fatalf("entry should check new.py: %+v", plan.Preflight.Results)
	}
}

// A job-level extra_run is retired like the rest of job-level preflight:
// warned and dropped, never run, never an error.
func TestJobLevelExtraRunIsDropped(t *testing.T) {
	var cfg job.JobConfig
	if err := yaml.Unmarshal([]byte("project: p\npreflight: {extra_run: 'exit 1'}\nfixed_params: {a: 1}"), &cfg); err != nil {
		t.Fatal(err)
	}
	source := &project.Config{ProjectName: "p", WorkingDir: t.TempDir(), CmdTemplate: "echo {{a}}"}
	if _, err := Build(context.Background(), cfg, source, Deps{Paths: Paths{WorkspaceRoot: "<w>", LogRoot: "<w>"}}); err != nil {
		t.Fatalf("job-level extra_run should no longer refuse the plan: %v", err)
	}
}
