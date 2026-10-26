package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/preflight"
)

func TestNoPreflightIsRetired(t *testing.T) {
	f := submitCmd.Flags().Lookup("no-preflight")
	t.Cleanup(func() { _ = f.Value.Set("false"); f.Changed = false })
	if !f.Hidden {
		t.Fatal("--no-preflight should be hidden")
	}
	if err := submitCmd.ParseFlags([]string{"--no-preflight"}); err != nil {
		t.Fatal(err)
	}
	err := checkRetiredFlags(submitCmd)
	if err == nil || !strings.Contains(err.Error(), "preflight only warns now; nothing to skip") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrintPreflightWarnings(t *testing.T) {
	var b bytes.Buffer
	printPreflightWarnings(&b, nil)
	if b.Len() != 0 {
		t.Fatalf("no warnings, no output: %q", b.String())
	}
	printPreflightWarnings(&b, []preflight.CheckResult{{Name: "huggingface", Status: "warning", Detail: "org/m is not in the HF cache", Commands: []string{"huggingface-cli download org/m"}}})
	for _, s := range []string{"1 warning(s) — submitted anyway", "! huggingface", "$ huggingface-cli download org/m"} {
		if !strings.Contains(b.String(), s) {
			t.Errorf("missing %q:\n%s", s, b.String())
		}
	}
}
