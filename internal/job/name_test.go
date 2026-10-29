package job

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNormalizeLegacyName(t *testing.T) {
	// note: is the submission's reason now, not the old name key.
	c := JobConfig{Name: "kl sweep", Note: "OOM: walltime 2h → 4h"}
	if warns := c.NormalizeLegacy(); len(warns) != 0 || c.Name != "kl sweep" || c.Note == "" {
		t.Fatalf("note must stay as written: %+v %v", c, warns)
	}

	c = JobConfig{Name: "kl-{{version}}"}
	if warns := c.NormalizeLegacy(); len(warns) != 1 || c.Name != "kl-{{version}}" {
		t.Fatalf("templates are shown as written, with a warning: %+v %v", c, warns)
	}

	c = JobConfig{Name: "A1 原始基线"}
	if warns := c.NormalizeLegacy(); len(warns) != 0 {
		t.Fatalf("a plain name needs no warning: %v", warns)
	}
}

func TestNormalizeLegacyJobPreflight(t *testing.T) {
	off := false
	c := JobConfig{Name: "x", Preflight: &PreflightOverride{Imports: &off}}
	if warns := c.NormalizeLegacy(); len(warns) != 1 || c.Preflight != nil || !strings.Contains(warns[0], "only warns") {
		t.Fatalf("job-level preflight must be dropped with a warning: %v %+v", warns, c.Preflight)
	}
	run := "test -f x"
	c = JobConfig{Name: "x", Preflight: &PreflightOverride{ExtraRun: &run}}
	if warns := c.NormalizeLegacy(); len(warns) != 1 || c.Preflight != nil {
		t.Fatalf("job-level extra_run is dropped with a warning too: %v %+v", warns, c.Preflight)
	}
	if _, err := Rows(&c); err != nil {
		t.Fatalf("job-level extra_run no longer refuses: %v", err)
	}
}

func TestNormalizeLegacyConsumers(t *testing.T) {
	var c JobConfig
	if err := yaml.Unmarshal([]byte("name: x\nconsumers:\n  - name: score\n    command: python score.py\n"), &c); err != nil {
		t.Fatalf("old job.yaml with consumers: must still load: %v", err)
	}
	if warns := c.NormalizeLegacy(); len(warns) != 1 || c.Consumers != nil || !strings.Contains(warns[0], "runq.job()") {
		t.Fatalf("consumers: must be dropped with a warning naming runq.job(): %v %+v", warns, c.Consumers)
	}
}
