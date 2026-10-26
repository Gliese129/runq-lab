package ingest

import "testing"

func TestProducerFromFile(t *testing.T) {
	for name, want := range map[string]string{
		"results.score.jsonl":          "score",
		"results.score_math-v2.jsonl":  "score_math-v2",
		"results.a.b.jsonl":            "a.b",
		"results.jsonl":                "",
		"results..jsonl":               "",
		"results.bad name.jsonl":       "",
		"results.score.exact.pyr.json": "",
		"metrics.score.jsonl":          "",
	} {
		got, ok := ProducerFromFile(name)
		if got != want || ok != (want != "") {
			t.Errorf("%s: got %q %v, want %q", name, got, ok, want)
		}
	}
}
