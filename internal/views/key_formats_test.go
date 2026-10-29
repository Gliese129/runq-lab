package views

import (
	"encoding/json"
	"os"
	"testing"
)

// testdata/key_formats.json is shared with the dashboard's viewFormat
// spec: a key prints the same in an export and on the page.
func TestKeyFormatsShared(t *testing.T) {
	raw, err := os.ReadFile("testdata/key_formats.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Value  any    `json:"value"`
		Format string `json:"format"`
		Now    int64  `json:"now"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		r := &Result{KeyFormats: map[string]string{"k": c.Format}, GeneratedAt: c.Now}
		if got := r.KeyText("k", c.Value); got != c.Text {
			t.Errorf("%v as %q = %q, want %q", c.Value, c.Format, got, c.Text)
		}
	}
}
