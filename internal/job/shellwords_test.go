package job

import (
	"slices"
	"testing"
)

func TestShellWords(t *testing.T) {
	for in, want := range map[string][]string{
		`python  train.py   --lr 0.1`:   {"python", "train.py", "--lr", "0.1"},
		`printf '%s' 'a  b'`:            {"printf", "%s", "a  b"},
		`echo "a \"b\" $x" c\ d`:        {"echo", `a "b" $x`, "c d"},
		"run.sh \\\n  --x {{param.lr}}": {"run.sh", "--x", "{{param.lr}}"},
		`a""b ''`:                       {"ab", ""},
	} {
		got, ok := ShellWords(in)
		if !ok || !slices.Equal(got, want) {
			t.Errorf("ShellWords(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	if _, ok := ShellWords(`echo 'open`); ok {
		t.Error("an open quote must not split")
	}
}

// Spacing and quoting style between words are the same command; what is
// inside quotes is not.
func TestSameCommand(t *testing.T) {
	same := [][2]string{
		{"python  train.py --lr 0.1", "python train.py\t--lr 0.1"},
		{`echo 'a b'`, `echo "a b"`},
	}
	differ := [][2]string{
		{`printf '%s' 'a  b'`, `printf '%s' 'a b'`},
		{"python train.py", "python eval.py"},
		{`echo 'open`, `echo  'open`},
	}
	for _, c := range same {
		if !SameCommand(c[0], c[1]) {
			t.Errorf("%q vs %q must be the same", c[0], c[1])
		}
	}
	for _, c := range differ {
		if SameCommand(c[0], c[1]) {
			t.Errorf("%q vs %q must differ", c[0], c[1])
		}
	}
}
