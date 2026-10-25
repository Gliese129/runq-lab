package job

import (
	"strings"
	"testing"
)

func TestNormalizeLegacyName(t *testing.T) {
	c := JobConfig{Note: "kl sweep"}
	warns := c.NormalizeLegacy()
	if c.Name != "kl sweep" || c.Note != "" || len(warns) != 1 {
		t.Fatalf("note must fold into name with one warning: %+v %v", c, warns)
	}

	// Old files: name: was the scheduler override next to note:.
	c = JobConfig{Note: "kl sweep", Name: "rq-{{lr}}"}
	warns = c.NormalizeLegacy()
	if c.Name != "kl sweep" || len(warns) != 2 || !strings.Contains(warns[0], "scheduler") {
		t.Fatalf("legacy scheduler override must be dropped with a warning: %+v %v", c, warns)
	}

	c = JobConfig{Name: "kl-{{version}}"}
	if warns = c.NormalizeLegacy(); len(warns) != 1 || c.Name != "kl-{{version}}" {
		t.Fatalf("templates are shown as written, with a warning: %+v %v", c, warns)
	}

	c = JobConfig{Name: "A1 原始基线"}
	if warns = c.NormalizeLegacy(); len(warns) != 0 {
		t.Fatalf("a plain name needs no warning: %v", warns)
	}
}
