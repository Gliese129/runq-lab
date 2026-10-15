package views

import (
	"strings"
	"testing"
)

// Numbers print in full unless a format asks for scientific notation: an
// 8-digit seed must not become 1.2345678e+07.
func TestPlainNumbersNeverScientific(t *testing.T) {
	for v, want := range map[float64]string{12345678: "12345678", 1e-6: "0.000001", 0.25: "0.25", -3: "-3", 1e20: "100000000000000000000"} {
		if got := Plain(v); got != want {
			t.Errorf("Plain(%v) = %q, want %q", v, got, want)
		}
	}
	if got, _ := Format(12345678.0, "", 0); got != "12345678" {
		t.Errorf("unformatted cell = %q", got)
	}
	for f, want := range map[string]string{"sci0": "1e-6", "sci1": "1.0e-6", "sci2": "3.00e-6"} {
		v := 1e-6
		if f == "sci2" {
			v = 3e-6
		}
		if got, _ := Format(v, f, 0); got != want {
			t.Errorf("Format(%v, %s) = %q, want %q", v, f, got, want)
		}
	}
}

func TestKeyFormatsApplyToExportsNotCSV(t *testing.T) {
	r := &Result{
		RowKeys: []string{"param.seed", "param.lr"}, KeyFormats: map[string]string{"param.lr": "sci0"},
		Columns: []Column{{Name: "Acc"}},
		Rows:    []Row{{Key: map[string]any{"param.seed": 12345678.0, "param.lr": 1e-6}, Cells: []Cell{{Value: 0.5, Text: "0.5"}}}},
	}
	md, err := Export(r, "markdown", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "12345678") || !strings.Contains(md, "1e-6") || strings.Contains(md, "e+07") {
		t.Errorf("markdown keys wrong:\n%s", md)
	}
	csv, _ := Export(r, "csv", false, false)
	if !strings.Contains(csv, "0.000001") {
		t.Errorf("csv should keep the raw key value:\n%s", csv)
	}
	if err := (Spec{Name: "v", Project: "p", KeyFormats: map[string]string{"param.lr": "sci9"}}).Validate(); err == nil || !strings.Contains(err.Error(), "key_formats") {
		t.Errorf("bad key format accepted: %v", err)
	}
}
