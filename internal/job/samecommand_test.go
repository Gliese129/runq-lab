package job

import "testing"

// Templates compare as what they run: both rendered with the cell's params
// under the submission's rules (scheduler params left out of {{args}},
// flags as bare switches), then compared as written.
func TestSameCommand(t *testing.T) {
	params := TaskParams{"lr": 0.1, "seed": float64(1), "sample": true, "h_rt": "4:00"}
	rules := CommandRules{Exclude: map[string]bool{"h_rt": true}, Flags: map[string]bool{"sample": true}}
	same := [][2]string{
		{"python t.py --lr {{lr}}", "python t.py --lr 0.1"},                          // a placeholder or its value
		{"python t.py {{args}}", "python t.py --lr={{lr}} --sample --seed={{seed}}"}, // {{args}} spelled out, h_rt left out
	}
	differ := [][2]string{
		{`echo '$VAR'`, `echo "$VAR"`},
		{`printf '%s' 'a  b'`, `printf '%s' 'a b'`},
		{"python train.py", "python eval.py"},
		{"python t.py {{missing}}", "python t.py {{other}}"},
		{"python t.py {{args}}", "python t.py --lr={{lr}} --sample=true --seed={{seed}}"}, // a flag runs as a bare switch
		{"python t.py {{args}}", "python t.py --h_rt={{h_rt}} --lr={{lr}} --sample --seed={{seed}}"},
	}
	for _, c := range same {
		if !SameCommand(c[0], c[1], params, rules) {
			t.Errorf("%q vs %q must be the same", c[0], c[1])
		}
	}
	for _, c := range differ {
		if SameCommand(c[0], c[1], params, rules) {
			t.Errorf("%q vs %q must differ", c[0], c[1])
		}
	}
}

// What runs is the template as written: whitespace inside quotes stays;
// only an empty {{args}} takes its blanks along.
func TestRenderKeepsTheTemplatesWhitespace(t *testing.T) {
	got, err := Render("printf '%s' 'a  b' {{args}}", TaskParams{})
	if err != nil || got != "printf '%s' 'a  b'" {
		t.Fatalf("render = %q, %v", got, err)
	}
	got, _ = Render("python t.py {{args}} --x", TaskParams{"lr": 0.1})
	if got != "python t.py --lr=0.1 --x" {
		t.Fatalf("render = %q", got)
	}
}
