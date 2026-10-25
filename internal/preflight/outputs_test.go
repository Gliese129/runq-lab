package preflight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/project"
)

func TestCheckOutputArgsWarnsOnlyForUnpassedFlags(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("train.py", "p.add_argument('--output_dir', default='out')\np.add_argument(\"--save-dir\")\np.add_argument('--lr')\n")
	write("tiny.py", "print('hi')\n")
	proj := &project.Config{WorkingDir: dir}
	p := Preflight{}

	r := p.checkOutputArgs(proj, "python train.py --lr 0.1")
	if r.Status != "warning" || !strings.Contains(r.Detail, "--output_dir, --save-dir") {
		t.Fatalf("both unpassed output flags should warn: %+v", r)
	}
	r = p.checkOutputArgs(proj, "python train.py --output-dir $RUNQ_OUTPUT_DIR --save_dir $RUNQ_CHECKPOINT_DIR")
	if r.Status != "passed" {
		t.Fatalf("passed flags (either spelling) must not warn: %+v", r)
	}
	if r = p.checkOutputArgs(proj, "python tiny.py"); r.Status != "passed" {
		t.Fatalf("a script without output flags is fine: %+v", r)
	}
	if r = p.checkOutputArgs(proj, "bash run.sh"); r.Status != "skipped" {
		t.Fatalf("non-python commands are skipped: %+v", r)
	}
}
