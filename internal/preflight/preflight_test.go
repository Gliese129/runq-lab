package preflight

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/rfs"
)

func write(t *testing.T, p, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// run checks proj with one task and returns results by name.
func run(t *testing.T, p Preflight, proj *project.Config, params job.TaskParams) map[string]CheckResult {
	t.Helper()
	if params == nil {
		params = job.TaskParams{}
	}
	rep, err := p.Run(context.Background(), proj, []job.TaskParams{params})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]CheckResult{}
	var names []string
	for _, c := range rep.Results {
		got[c.Name] = c
		names = append(names, c.Name)
		if c.Status != "passed" && c.Status != "warning" && c.Status != "skipped" {
			t.Errorf("%s: status %q is not warning-only grammar", c.Name, c.Status)
		}
	}
	// Five target checks, in order; shell_syntax is added by the backend.
	if want := CheckNames[:5]; !slices.Equal(names, want) {
		t.Fatalf("checks = %v, want %v", names, want)
	}
	return got
}

func want(t *testing.T, got map[string]CheckResult, name, status, detail string) {
	t.Helper()
	c := got[name]
	if c.Status != status || !strings.Contains(c.Detail, detail) {
		t.Errorf("%s = %s %q, want %s containing %q", name, c.Status, c.Detail, status, detail)
	}
}

// cleanPath keeps a local conda / pyenv out of the tests' way.
var cleanPath = map[string]string{"PATH": "/usr/bin:/bin"}

func TestEntry(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "train.py"), "", 0o644)
	p := Preflight{Env: cleanPath}
	proj := &project.Config{WorkingDir: dir, CmdTemplate: "python train.py --lr 1"}
	want(t, run(t, p, proj, nil), "entry", "passed", filepath.Join(dir, "train.py"))

	proj.CmdTemplate = "bash scripts/missing.sh"
	want(t, run(t, p, proj, nil), "entry", "warning", "scripts/missing.sh does not exist")

	proj.CmdTemplate = "python -m pkg.train"
	want(t, run(t, p, proj, nil), "entry", "skipped", "-m pkg.train")
}

// fakeVenv makes <dir>/.venv with an activate script and a python that
// reports prefix as its sys.prefix.
func fakeVenv(t *testing.T, dir, activate, prefix string) {
	t.Helper()
	write(t, filepath.Join(dir, ".venv/bin/activate"), activate, 0o644)
	write(t, filepath.Join(dir, ".venv/bin/python"), "#!/bin/sh\necho "+prefix+"\n", 0o755)
}

func TestInterpreterAndEnvActivationVenv(t *testing.T) {
	dir := t.TempDir()
	proj := &project.Config{WorkingDir: dir, CmdTemplate: "python train.py", PythonEnv: project.PythonEnvConfig{Type: "venv"}}
	p := Preflight{Env: cleanPath}

	// No venv at all: the interpreter is missing; activation fails too.
	got := run(t, p, proj, nil)
	want(t, got, "interpreter", "warning", filepath.Join(dir, ".venv/bin/python")+" is missing")
	want(t, got, "env_activation", "warning", "did not activate in a non-interactive shell")

	venv := filepath.Join(dir, ".venv")
	fakeVenv(t, dir, "true\n", venv)
	got = run(t, p, proj, nil)
	want(t, got, "interpreter", "passed", "is executable")
	want(t, got, "env_activation", "passed", "venv at "+venv)

	// An activate that fails says why (its stderr's last line).
	fakeVenv(t, dir, "echo 'activate: broken on purpose' >&2; return 3\n", venv)
	want(t, run(t, p, proj, nil), "env_activation", "warning", "exit 3: activate: broken on purpose")

	// Activates, but into another python: warn, never block.
	fakeVenv(t, dir, "true\n", "/somewhere/else")
	want(t, run(t, p, proj, nil), "env_activation", "warning", "expected the venv")
}

func TestEnvActivationCondaNotInNonInteractiveShell(t *testing.T) {
	proj := &project.Config{WorkingDir: t.TempDir(), CmdTemplate: "python x.py", PythonEnv: project.PythonEnvConfig{Type: "conda", Name: "torch"}}
	got := run(t, Preflight{Env: cleanPath}, proj, nil)
	want(t, got, "env_activation", "warning", "conda registered only in ~/.bashrc")
	want(t, got, "interpreter", "skipped", "did not activate")

	got = run(t, Preflight{Env: cleanPath, DisableLocal: true}, proj, nil)
	want(t, got, "env_activation", "skipped", "preflight_local")
}

func TestInterpreterOnPath(t *testing.T) {
	bin := t.TempDir()
	write(t, filepath.Join(bin, "python3.42"), "#!/bin/sh\necho /opt/py\n", 0o755)
	proj := &project.Config{WorkingDir: t.TempDir(), CmdTemplate: "python3.42 x.py"}
	p := Preflight{Env: map[string]string{"PATH": bin + ":/usr/bin:/bin"}}
	got := run(t, p, proj, nil)
	want(t, got, "interpreter", "passed", filepath.Join(bin, "python3.42"))
	want(t, got, "env_activation", "skipped", "no python_env")

	proj.CmdTemplate = "python9 x.py"
	want(t, run(t, p, proj, nil), "interpreter", "warning", "python9 is not on PATH")

	proj.CmdTemplate = "/nope/bin/python x.py"
	want(t, run(t, p, proj, nil), "interpreter", "warning", "/nope/bin/python is missing")

	proj.CmdTemplate = "echo hi"
	want(t, run(t, p, proj, nil), "interpreter", "skipped", "runs no python")
}

func TestDataDirs(t *testing.T) {
	root := t.TempDir()
	proj := &project.Config{WorkingDir: root, CmdTemplate: "echo hi"}
	p := Preflight{Env: cleanPath, DataDir: filepath.Join(root, "a/b/jb1")}
	want(t, run(t, p, proj, nil), "data_dirs", "passed", root+" is writable")

	ro := filepath.Join(root, "ro")
	if err := os.Mkdir(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	p.DataDir = filepath.Join(ro, "jb1")
	want(t, run(t, p, proj, nil), "data_dirs", "warning", "nearest existing parent "+ro+" is not a writable directory")
}

func TestHuggingFaceCacheFilesOnly(t *testing.T) {
	hub := t.TempDir()
	write(t, filepath.Join(hub, "models--org--cached/snapshots/abc/config.json"), "{}", 0o644)
	write(t, filepath.Join(hub, "datasets--org--data/snapshots/def/x"), "", 0o644)
	if err := os.MkdirAll(filepath.Join(hub, "models--org--empty/snapshots"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"PATH": "/usr/bin:/bin", "HF_HUB_CACHE": hub}
	proj := &project.Config{WorkingDir: t.TempDir(), CmdTemplate: "echo {{model}}",
		Preflight: &project.PreflightConfig{HF: []string{"org/data", "{{param.model}}"}}}

	want(t, run(t, Preflight{Env: env}, proj, job.TaskParams{"model": "org/cached"}), "huggingface", "passed", "2 declared repo(s)")

	got := run(t, Preflight{Env: env}, proj, job.TaskParams{"model": "org/empty"})
	want(t, got, "huggingface", "warning", "org/empty is not in the HF cache ("+hub+")")
	if c := got["huggingface"]; !slices.Equal(c.Commands, []string{"huggingface-cli download org/empty"}) {
		t.Errorf("pre-download hint: %v", c.Commands)
	}

	proj.Preflight = nil
	want(t, run(t, Preflight{Env: env}, proj, job.TaskParams{"model": "x"}), "huggingface", "skipped", "no preflight.hf")
}

func TestShellSyntax(t *testing.T) {
	ctx := context.Background()
	names := []string{"run.sh", "submit line"}
	ok := ShellSyntax(ctx, names, map[string]string{"run.sh": "set -e\necho hi\n", "submit line": "qsub -N a run.sh"})
	if ok.Name != "shell_syntax" || ok.Status != "passed" {
		t.Fatalf("good scripts: %+v", ok)
	}
	bad := ShellSyntax(ctx, names, map[string]string{"run.sh": "echo hi\n", "submit line": "qsub -v 'A=1 run.sh"})
	if bad.Status != "warning" || !strings.Contains(bad.Detail, "submit line:") || strings.Contains(bad.Detail, "run.sh:") {
		t.Fatalf("bad submit line: %+v", bad)
	}
}

// A timeout learns nothing: every target check is skipped, none warns.
func TestTimeoutSkipsNeverWarns(t *testing.T) {
	proj := &project.Config{WorkingDir: t.TempDir(), CmdTemplate: "python x.py", PythonEnv: project.PythonEnvConfig{Type: "venv"}}
	p := Preflight{Env: cleanPath, EnvSetup: "sleep 5", Timeout: 300 * time.Millisecond, DataDir: t.TempDir()}
	start := time.Now()
	for name, c := range run(t, p, proj, nil) {
		if name != "huggingface" && (c.Status != "skipped" || !strings.Contains(c.Detail, "timed out")) {
			t.Errorf("%s = %s %q, want skipped (timed out)", name, c.Status, c.Detail)
		}
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("timeout not enforced: %s", time.Since(start))
	}
}

type countFS struct {
	rfs.FS
	execs []string
}

func (f *countFS) Exec(ctx context.Context, cmd string, args ...string) ([]byte, []byte, int, error) {
	f.execs = append(f.execs, cmd+" "+strings.Join(args, " "))
	return f.FS.Exec(ctx, cmd, args...)
}

// The target checks are ONE exec on the target, whatever is declared.
func TestTargetChecksAreOneExec(t *testing.T) {
	dir := t.TempDir()
	fakeVenv(t, dir, "true\n", filepath.Join(dir, ".venv"))
	fs := &countFS{FS: rfs.NewLocalFS()}
	proj := &project.Config{WorkingDir: dir, CmdTemplate: "python x.py", PythonEnv: project.PythonEnvConfig{Type: "venv"},
		Preflight: &project.PreflightConfig{HF: []string{"org/a", "org/b"}}}
	got := run(t, Preflight{FS: fs, Env: cleanPath, DataDir: filepath.Join(dir, "jb")}, proj, nil)
	if len(fs.execs) != 1 {
		t.Fatalf("execs = %d, want 1: %v", len(fs.execs), fs.execs)
	}
	want(t, got, "env_activation", "passed", "venv at")
	want(t, got, "entry", "warning", "x.py does not exist")
}

// A target shell that answers nothing (transport trouble) skips, never warns.
func TestNoAnswerSkips(t *testing.T) {
	proj := &project.Config{WorkingDir: "/w", CmdTemplate: "python x.py"}
	got := run(t, Preflight{FS: silentFS{}}, proj, nil)
	for _, name := range []string{"entry", "interpreter"} {
		want(t, got, name, "skipped", "no answer from the target shell")
	}
}

type silentFS struct{ rfs.FS }

func (silentFS) Exec(context.Context, string, ...string) ([]byte, []byte, int, error) {
	return nil, nil, 0, nil
}

func TestExpandContractHF(t *testing.T) {
	refs, problems := expandContractHF([]string{"org/a", "{{param.m}}", "{{param.none}}", "./local"},
		[]job.TaskParams{{"m": "org/b"}, {"m": "org/a"}})
	var ids []string
	for _, r := range refs {
		ids = append(ids, r.RepoID)
	}
	if !slices.Equal(ids, []string{"org/a", "org/b"}) || len(problems) != 2 {
		t.Fatalf("refs %v problems %v", ids, problems)
	}
}

func TestReportWarningsAndFormat(t *testing.T) {
	r := Report{Results: []CheckResult{
		{Name: "entry", Status: "passed", Detail: "ok"},
		{Name: "huggingface", Status: "warning", Detail: "org/m missing", Commands: []string{"huggingface-cli download org/m"}},
		{Name: "gpu", Status: "skipped"},
	}}
	if w := r.Warnings(); len(w) != 1 || w[0].Name != "huggingface" {
		t.Fatalf("warnings: %+v", w)
	}
	out := Format(r.Results)
	for _, s := range []string{"✓ entry", "! huggingface", "$ huggingface-cli download org/m", "- gpu"} {
		if !strings.Contains(out, s) {
			t.Errorf("format missing %q:\n%s", s, out)
		}
	}
}
