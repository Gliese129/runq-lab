package config

import (
	"strings"
	"testing"
)

func TestRunqPresetIncludesTimeoutPlaceholder(t *testing.T) {
	cfg, err := HPCPreset("runq")
	if err != nil {
		t.Fatalf("HPCPreset(runq): %v", err)
	}
	if !strings.Contains(cfg.SubmitTemplate, "--timeout {{timeout}}") {
		t.Fatalf("runq submit template does not forward timeout: %s", cfg.SubmitTemplate)
	}

	for _, result := range cfg.CheckHPC() {
		if result.Name != "submit_template" {
			continue
		}
		if result.Status != "ok" {
			t.Fatalf("submit template check failed: %+v", result)
		}
		if !strings.Contains(result.Detail, "--timeout '60'") {
			t.Fatalf("submit template preview does not render timeout: %s", result.Detail)
		}
		return
	}
	t.Fatal("submit template check result not found")
}

func TestCheckHPCOptionalSegments(t *testing.T) {
	cfg := &TargetConfig{
		SubmitTemplate: "qsub [[-ar {{param.ar_id}}]] [[--no-requeue]] {{run_sh}}",
		KillTemplate:   "qdel {{ext_id}}",
	}
	for _, r := range cfg.CheckHPC() {
		if r.Name != "submit_template" {
			continue
		}
		if r.Status != "ok" || !strings.Contains(r.Detail, "-ar '<ar_id>' --no-requeue") {
			t.Fatalf("check should preview every segment: %+v", r)
		}
		if got := HPCTemplateParamRefs(cfg.SubmitTemplate); strings.Join(got, ",") != "ar_id,no-requeue" {
			t.Fatalf("HPCTemplateParamRefs = %v", got)
		}
		cfg.SubmitTemplate = "qsub [[-l h_rt={{param.h_rt}},gpu={{param.g}}]] {{run_sh}}"
		for _, r := range cfg.CheckHPC() {
			if r.Name == "submit_template" && r.Status != "fail" {
				t.Fatalf("two params in one segment must fail the check: %+v", r)
			}
		}
		return
	}
	t.Fatal("submit template check result not found")
}
