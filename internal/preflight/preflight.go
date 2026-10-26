package preflight

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/utils"
)

// Preflight runs on every `runq draft` and `runq submit dr…` and only
// WARNS: it looks for deterministic problems — a missing file, an env that
// does not activate, a directory runq cannot write — and never blocks.
// Whether the job actually runs (imports, data, GPUs) is a smoke job's
// question, not preflight's.
//
// Six checks, reported by these names (CheckNames):
//
//	entry           the script the command runs (.py / .sh, relative to
//	                working_dir) exists — test -f
//	interpreter     the configured python interpreter exists and is
//	                executable — test -x (looked up inside the env for
//	                conda / system)
//	env_activation  python_env activates in a NON-interactive shell, the
//	                shell class compute nodes give the task (no ~/.bashrc)
//	data_dirs       the task data directory's nearest existing parent is
//	                writable
//	huggingface     each project.yaml `preflight.hf` repo is in the HF
//	                cache (files only: no python, no network)
//	shell_syntax    bash -n on run.sh and the submit line, locally — added
//	                by the target backend once it has rendered both
//	                (ShellSyntax)
//
// The first five are target-side and run as ONE exec on the target (one
// SSH round trip, one env activation); the activation is the only part
// that takes more than a few milliseconds.
type Preflight struct {
	// Timeout caps the target exec. Checks whose markers did not arrive
	// report as skipped (a timeout learns nothing). Default 20s, under the
	// dashboard's 30s request timeout.
	Timeout time.Duration

	// DisableLocal skips env_activation (and the interpreter lookup that
	// needs the activated env) — for login nodes whose policy forbids
	// running the env there (hpc: preflight_local: false).
	DisableLocal bool

	// Scope labels where target checks ran (e.g. "on tsubame login node"):
	// login-node facts do not guarantee the compute node.
	Scope string

	// ExcludeParams are scheduler-consumed params (submit_template's
	// {{param.*}}) — the sample command renders without demanding them.
	ExcludeParams map[string]bool

	// Env is the merged task environment (project environment + job
	// overrides, incl. RUNQ_ENV_FILE) and EnvSetup the target's env_setup:
	// the exec assembles THE SAME environment run.sh gives the task.
	Env      map[string]string
	EnvSetup string

	// DataDir is the task data directory (the job root) data_dirs checks.
	// Disk space and inode headroom are internal/diskspace's, reported
	// next to these results by the target backend — data_dirs answers only
	// "can runq create it".
	DataDir string

	// FS is the filesystem THE TASK runs against; its Exec runs the target
	// checks. nil = this machine.
	FS rfs.FS
}

// CheckNames are the checks a report contains, in report order.
var CheckNames = []string{"entry", "interpreter", "env_activation", "data_dirs", "huggingface", "shell_syntax"}

// CheckResult is one check: passed / warning / skipped. skipped means
// "could not be checked here" (nothing to check, disabled by target
// policy, timeout). Nothing blocks a submit.
type CheckResult struct {
	Name   string `json:"name"`   // one of CheckNames
	Status string `json:"status"` // passed | warning | skipped
	Detail string `json:"detail,omitempty"`
	// Commands are ready-made remediation commands (e.g. the HF
	// pre-download). The CLI prints them; the dashboard can offer them.
	Commands []string `json:"commands,omitempty"`
}

// Report is every check's result, in CheckNames order.
type Report struct {
	Results []CheckResult `json:"results"`
}

// Warnings returns the results that found something.
func (r Report) Warnings() []CheckResult {
	var out []CheckResult
	for _, c := range r.Results {
		if c.Status == "warning" {
			out = append(out, c)
		}
	}
	return out
}

// Add appends a result computed outside Run (shell_syntax).
func (r *Report) Add(c CheckResult) { r.Results = append(r.Results, c) }

// DefaultPreflight returns a runner with the default timeout.
func DefaultPreflight() Preflight {
	return Preflight{Timeout: 20 * time.Second}
}

// Run checks a project against its first task's rendered command (all
// tasks share script and env; only arg values differ). The error is a
// render failure only — findings are warnings in the report.
func (p Preflight) Run(ctx context.Context, proj *project.Config, taskParams []job.TaskParams) (Report, error) {
	sampleCmd, err := renderSampleCommand(proj, taskParams, p.ExcludeParams)
	if err != nil {
		return Report{}, err
	}
	refs, hfIssues := expandContractHF(proj.Preflight.HFRepos(), taskParams)
	return p.check(ctx, proj, sampleCmd, refs, hfIssues), nil
}

// targetPlan is what the target exec looks at, decided Go-side from the
// project and the sample command.
type targetPlan struct {
	entry     string // absolute script path; "" = none
	entryWhy  string // why entry is skipped
	interp    string // interpreter the command runs; "" = none
	interpAbs string // test -x this path outside the env ("" = look it up in the env)
	envType   string // "" = no python_env
	needEnv   bool   // run the activation subshell
	refs      []HFRef
}

func (p Preflight) plan(proj *project.Config, sampleCmd string, refs []HFRef) targetPlan {
	pl := targetPlan{refs: refs, envType: proj.PythonEnv.Type}
	if pl.envType == "system" {
		pl.envType = ""
	}
	switch e, why := entryScript(sampleCmd); {
	case e == "":
		pl.entryWhy = why
	case path.IsAbs(e):
		pl.entry = e
	default:
		pl.entry = path.Join(proj.WorkingDir, e)
	}
	interp, named := pythonInterpreterIn(sampleCmd)
	switch {
	case named && path.IsAbs(interp):
		pl.interp, pl.interpAbs = interp, interp
	case pl.envType == "venv" || pl.envType == "uv":
		pl.interp, pl.interpAbs = interp, path.Join(venvDir(proj), "bin", interp)
	case named || pl.envType != "":
		pl.interp = interp // conda / system: whatever PATH gives inside the env
	}
	pl.needEnv = !p.DisableLocal && (pl.envType != "" || (pl.interp != "" && pl.interpAbs == ""))
	return pl
}

func venvDir(proj *project.Config) string {
	d := proj.PythonEnv.Path
	if d == "" {
		d = ".venv"
	}
	if !path.IsAbs(d) {
		d = path.Join(proj.WorkingDir, d)
	}
	return d
}

func (p Preflight) check(ctx context.Context, proj *project.Config, sampleCmd string, refs []HFRef, hfIssues []string) Report {
	pl := p.plan(proj, sampleCmd, refs)
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, runErr := p.runShell(cctx, p.script(proj, pl))
	m := parseMarkers(out)

	// lost: a check whose marker never arrived learned nothing.
	lost := func(name string) CheckResult {
		detail := "no answer from the target shell: " + lastLine(out)
		switch {
		case cctx.Err() != nil:
			detail = fmt.Sprintf("timed out after %s on the target — not checked", timeout)
		case runErr != nil:
			detail = fmt.Sprintf("could not run on the target: %v", runErr)
		}
		return CheckResult{Name: name, Status: "skipped", Detail: detail}
	}
	scoped := func(d string) string {
		if p.Scope != "" {
			return d + " (" + p.Scope + ")"
		}
		return d
	}
	var rep Report

	switch v, ok := m["entry"]; {
	case pl.entry == "":
		rep.Add(CheckResult{Name: "entry", Status: "skipped", Detail: pl.entryWhy})
	case !ok:
		rep.Add(lost("entry"))
	case v.status == "ok":
		rep.Add(CheckResult{Name: "entry", Status: "passed", Detail: scoped(pl.entry + " exists")})
	default:
		rep.Add(CheckResult{Name: "entry", Status: "warning", Detail: fmt.Sprintf(
			"%s does not exist — the task would fail at once; paths in the command resolve from working_dir (%s)", pl.entry, proj.WorkingDir)})
	}

	act, actOK := m["activate"]
	activated := m["activated"].status == "ok"
	switch {
	case pl.interp == "":
		rep.Add(CheckResult{Name: "interpreter", Status: "skipped", Detail: "the command runs no python and no python_env is declared"})
	case pl.interpAbs != "":
		switch v, ok := m["interp"]; {
		case !ok:
			rep.Add(lost("interpreter"))
		case v.status == "ok":
			rep.Add(CheckResult{Name: "interpreter", Status: "passed", Detail: scoped(pl.interpAbs + " is executable")})
		default:
			hint := ""
			if pl.envType == "venv" || pl.envType == "uv" {
				hint = " — create the venv on the target (python_env.path is relative to working_dir)"
			}
			rep.Add(CheckResult{Name: "interpreter", Status: "warning", Detail: pl.interpAbs + " is missing or not executable" + hint})
		}
	case p.DisableLocal:
		rep.Add(CheckResult{Name: "interpreter", Status: "skipped", Detail: "looked up inside the env; disabled by hpc preflight_local"})
	case !actOK:
		rep.Add(lost("interpreter"))
	case !activated:
		rep.Add(CheckResult{Name: "interpreter", Status: "skipped", Detail: "the environment did not activate (see env_activation)"})
	case m["pyfound"].status == "ok":
		rep.Add(CheckResult{Name: "interpreter", Status: "passed", Detail: scoped(pl.interp + " → " + m["pyfound"].detail)})
	default:
		where := "in the activated environment"
		if pl.envType == "" {
			where = "in the target's shell environment"
		}
		rep.Add(CheckResult{Name: "interpreter", Status: "warning", Detail: fmt.Sprintf(
			"%s is not on PATH %s — the task's command would not find it", pl.interp, where)})
	}

	switch {
	case pl.envType == "":
		rep.Add(CheckResult{Name: "env_activation", Status: "skipped", Detail: "no python_env declared — the command runs in the target's plain shell environment"})
	case p.DisableLocal:
		rep.Add(CheckResult{Name: "env_activation", Status: "skipped", Detail: "disabled by hpc preflight_local"})
	case !actOK:
		rep.Add(lost("env_activation"))
	case !activated:
		hint := "Make the env available via the target's env_setup or absolute paths."
		if pl.envType == "conda" {
			hint = "A conda registered only in ~/.bashrc is invisible here: put its init in the target's env_setup."
		}
		rep.Add(CheckResult{Name: "env_activation", Status: "warning", Detail: fmt.Sprintf(
			"python_env (%s) did not activate in a non-interactive shell (exit %s: %s); compute nodes use the same shell class, so the task would fail too. %s",
			pl.envType, act.status, act.detail, hint)})
	default:
		rep.Add(envResult(proj, m["prefix"], scoped))
	}

	// data_dirs answers "writable"; space and inode headroom are
	// internal/diskspace's warnings, joined by the target backend.
	switch v, ok := m["data_dirs"]; {
	case p.DataDir == "":
		rep.Add(CheckResult{Name: "data_dirs", Status: "skipped", Detail: "no task data directory in this plan"})
	case !ok:
		rep.Add(lost("data_dirs"))
	case v.status == "ok":
		rep.Add(CheckResult{Name: "data_dirs", Status: "passed", Detail: scoped(v.detail + " is writable")})
	default:
		rep.Add(CheckResult{Name: "data_dirs", Status: "warning", Detail: fmt.Sprintf(
			"cannot create %s: its nearest existing parent %s is not a writable directory", p.DataDir, v.detail)})
	}

	rep.Add(hfResult(pl, m, hfIssues, lost, scoped))
	return rep
}

// envResult cross-checks the activated env's sys.prefix against the
// declared env: a mismatch warns (activation produced A python, maybe not
// the one meant).
func envResult(proj *project.Config, prefix marker, scoped func(string) string) CheckResult {
	pass := func(d string) CheckResult {
		return CheckResult{Name: "env_activation", Status: "passed", Detail: scoped(d)}
	}
	pre := prefix.detail
	if prefix.status != "ok" || pre == "" {
		return pass(proj.PythonEnv.Type + " env activates in a non-interactive shell")
	}
	switch proj.PythonEnv.Type {
	case "conda":
		name := proj.PythonEnv.Name
		if name == "" || name == "base" || path.Base(pre) == name || strings.Contains(pre, "/envs/"+name) {
			return pass("conda env at " + pre)
		}
		return CheckResult{Name: "env_activation", Status: "warning", Detail: fmt.Sprintf(
			"python resolves to %s, which does not look like conda env %q — check the env name", pre, name)}
	case "venv", "uv":
		if path.Clean(pre) == path.Clean(venvDir(proj)) {
			return pass("venv at " + pre)
		}
		return CheckResult{Name: "env_activation", Status: "warning", Detail: fmt.Sprintf(
			"python resolves to %s, expected the venv %s", pre, venvDir(proj))}
	}
	return pass("python at " + pre)
}

func hfResult(pl targetPlan, m map[string]marker, issues []string, lost func(string) CheckResult, scoped func(string) string) CheckResult {
	if len(pl.refs) == 0 && len(issues) == 0 {
		return CheckResult{Name: "huggingface", Status: "skipped", Detail: "no preflight.hf repos declared in project.yaml"}
	}
	problems := append([]string(nil), issues...)
	var cmds []string
	for _, r := range pl.refs {
		v, ok := m["hf:"+r.RepoID]
		if !ok {
			return lost("huggingface")
		}
		if v.status != "ok" {
			problems = append(problems, r.RepoID+" is not in the HF cache")
			cmds = append(cmds, r.DownloadCommand())
		}
	}
	if len(problems) == 0 {
		return CheckResult{Name: "huggingface", Status: "passed", Detail: scoped(fmt.Sprintf("%d declared repo(s) in the HF cache", len(pl.refs)))}
	}
	detail := strings.Join(problems, "; ")
	if len(cmds) > 0 {
		detail += fmt.Sprintf(" (%s) — the task would download it at run time, which fails on compute nodes without internet; pre-download it", m["hfhub"].detail)
	}
	return CheckResult{Name: "huggingface", Status: "warning", Detail: detail, Commands: cmds}
}

// ---- the target exec ----------------------------------------------

// markerPrefix starts every machine-readable line the target shell
// prints: MARKER \t key \t status \t detail. Anything else (activation
// banners) is ignored.
const markerPrefix = "##RUNQ_PF##"

type marker struct{ status, detail string }

func parseMarkers(out string) map[string]marker {
	m := map[string]marker{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, markerPrefix+"\t") {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 3 {
			continue
		}
		v := marker{status: parts[2]}
		if len(parts) == 4 {
			v.detail = parts[3]
		}
		m[parts[1]] = v
	}
	return m
}

// script renders the ONE POSIX-sh exec: the task's env prelude, the file
// tests, then the env activation in a subshell — its stderr captured for
// the warning, its stdout passed through for the markers.
func (p Preflight) script(proj *project.Config, pl targetPlan) string {
	q := utils.ShellQuote
	var b strings.Builder
	b.WriteString(utils.EnvPrelude{EnvSetup: p.EnvSetup, EnvFile: p.Env["RUNQ_ENV_FILE"], Env: p.Env}.Render())
	b.WriteString(`pf() { printf '` + markerPrefix + `\t%s\t%s\t%s\n' "$1" "$2" "$(printf '%s' "$3" | tr '\t\n' '  ')"; }` + "\n")
	if pl.entry != "" {
		fmt.Fprintf(&b, "if [ -f %s ]; then pf entry ok; else pf entry missing; fi\n", q(pl.entry))
	}
	if pl.interpAbs != "" {
		fmt.Fprintf(&b, "if [ -x %[1]s ] && [ ! -d %[1]s ]; then pf interp ok; else pf interp missing; fi\n", q(pl.interpAbs))
	}
	if p.DataDir != "" {
		fmt.Fprintf(&b, `d=%s; while [ ! -e "$d" ] && [ "$d" != / ] && [ "$d" != . ]; do d=$(dirname "$d"); done
if [ -d "$d" ] && [ -w "$d" ]; then pf data_dirs ok "$d"; else pf data_dirs no "$d"; fi
`, q(p.DataDir))
	}
	if len(pl.refs) > 0 {
		b.WriteString(`hub="${HF_HUB_CACHE:-${HUGGINGFACE_HUB_CACHE:-${HF_HOME:-$HOME/.cache/huggingface}/hub}}"; pf hfhub ok "$hub"
hfc() { for s in "$hub/$1"/snapshots/*; do [ -e "$s" ] && return 0; done; return 1; }
`)
		for _, r := range pl.refs {
			var conds []string
			for _, d := range r.cacheDirs() {
				conds = append(conds, "hfc "+q(d))
			}
			key := q("hf:" + r.RepoID)
			fmt.Fprintf(&b, "if %s; then pf %s ok; else pf %s missing; fi\n", strings.Join(conds, " || "), key, key)
		}
	}
	if pl.needEnv {
		inner := "pf activated ok"
		switch {
		case pl.interpAbs != "":
			inner += "; PY=" + q(pl.interpAbs)
		case pl.interp != "":
			inner += fmt.Sprintf(`; PY=$(command -v %s 2>/dev/null); if [ -n "$PY" ]; then pf pyfound ok "$PY"; else pf pyfound missing; fi`, q(pl.interp))
		default:
			inner += `; PY=$(command -v python3 2>/dev/null || command -v python 2>/dev/null)`
		}
		inner += `; if [ -n "$PY" ]; then pf prefix ok "$("$PY" -c 'import sys; print(sys.prefix)' 2>/dev/null)"; fi`
		wrapped := utils.WrapCommand(pl.envType, proj.PythonEnv.Path, proj.PythonEnv.Name, "{ "+inner+" ; }", proj.WorkingDir)
		// stdout → fd 3 (the markers reach the output), stderr → err.
		fmt.Fprintf(&b, "exec 3>&1; err=$( ( %s ) 2>&1 1>&3 ); rc=$?; exec 3>&-\npf activate \"$rc\" \"$(printf '%%s' \"$err\" | tail -n 1)\"\n", wrapped)
	}
	return b.String()
}

// runShell executes the check script where the task will run: locally via
// sh, remotely via FS.Exec. err non-nil = the script could not run (no
// fact learned); partial output is returned either way.
func (p Preflight) runShell(ctx context.Context, script string) (string, error) {
	if p.FS == nil {
		c := exec.CommandContext(ctx, "sh", "-c", script)
		// Kill the whole process group on timeout: an activation hook's
		// grandchildren would otherwise hold the output pipe open.
		setupProcessGroup(c)
		c.WaitDelay = time.Second
		out, err := c.CombinedOutput()
		var ee *exec.ExitError
		if err != nil && !errors.As(err, &ee) {
			return string(out), err
		}
		return string(out), nil
	}
	stdout, stderr, _, err := p.FS.Exec(ctx, "sh", "-c", script)
	return string(stdout) + string(stderr), err
}

// ---- shell_syntax (local) -----------------------------------------

// ShellSyntax runs `bash -n` locally on each named script, in order (the
// target backend passes run.sh and the submit line). bash -n parses
// without executing.
func ShellSyntax(ctx context.Context, names []string, scripts map[string]string) CheckResult {
	bash, err := exec.LookPath("bash")
	if err != nil {
		return CheckResult{Name: "shell_syntax", Status: "skipped", Detail: "bash not found on this machine"}
	}
	var bad []string
	for _, name := range names {
		c := exec.CommandContext(ctx, bash, "-n")
		c.Stdin = strings.NewReader(scripts[name])
		if out, err := c.CombinedOutput(); err != nil {
			msg := strings.Join(strings.Fields(bashPrefix.ReplaceAllString(string(out), "")), " ")
			bad = append(bad, name+": "+msg)
		}
	}
	if len(bad) > 0 {
		return CheckResult{Name: "shell_syntax", Status: "warning", Detail: "syntax error — " + strings.Join(bad, "; ")}
	}
	return CheckResult{Name: "shell_syntax", Status: "passed", Detail: "bash -n ok: " + strings.Join(names, ", ")}
}

// bashPrefix is the "/bin/bash: " every bash -n message starts with.
var bashPrefix = regexp.MustCompile(`(?m)^\S*bash: `)

// ---- command parsing -----------------------------------------------

// entryRegex finds the first .py / .sh token of a command.
var entryRegex = regexp.MustCompile(`(?:^|[\s=])['"]?([^\s'"=;&|]+\.(?:py|sh))['"]?(?:$|[\s;&|])`)

// moduleRegex catches `python -m pkg.mod` entries.
var moduleRegex = regexp.MustCompile(`\bpython[\d.]*\s+(?:-\S+\s+)*-m\s+([A-Za-z_][\w.]*)`)

// entryScript returns the script token the command runs, or why there is
// none to check.
func entryScript(cmd string) (string, string) {
	if m := moduleRegex.FindStringSubmatch(cmd); m != nil {
		return "", fmt.Sprintf("the command runs a module (-m %s), not a script file", m[1])
	}
	m := entryRegex.FindStringSubmatch(cmd)
	if m == nil {
		return "", "no .py or .sh script in the command"
	}
	if strings.ContainsAny(m[1], "${}") {
		return "", fmt.Sprintf("script path %s is computed at run time", m[1])
	}
	return m[1], ""
}

// pythonInterpreterRegex captures the python executable token in a command
// (`python`, `python3.11`, `/opt/py/bin/python3`).
var pythonInterpreterRegex = regexp.MustCompile(`(?:^|[\s;&|])((?:/[\w.+-]+)*/python[\d.]*|python[\d.]*)(?:$|\s)`)

// pythonInterpreterIn returns the interpreter the command names, and
// whether it named one ("python" when not).
func pythonInterpreterIn(text string) (string, bool) {
	if m := pythonInterpreterRegex.FindStringSubmatch(text); m != nil {
		return m[1], true
	}
	return "python", false
}

// lastLine returns the last non-empty line (a shell error's useful part).
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// renderSampleCommand renders the first task's command — the same
// command the plan submits (flag params with store_true semantics).
func renderSampleCommand(proj *project.Config, taskParams []job.TaskParams, exclude map[string]bool) (string, error) {
	if len(taskParams) == 0 {
		return "", nil
	}
	var flags map[string]bool
	for _, def := range proj.Params {
		if def.Style == "flag" {
			if flags == nil {
				flags = map[string]bool{}
			}
			flags[def.Name] = true
		}
	}
	return job.RenderWithFlags(proj.CmdTemplate, taskParams[0], exclude, flags)
}

// Format renders results one line each (✓ passed, ! warning, - skipped),
// with remediation commands copy-paste ready under their check.
func Format(results []CheckResult) string {
	var b strings.Builder
	for _, c := range results {
		mark := map[string]string{"passed": "✓", "warning": "!"}[c.Status]
		if mark == "" {
			mark = "-"
		}
		fmt.Fprintf(&b, "%s %-14s %s\n", mark, c.Name, c.Detail)
		for _, cmd := range c.Commands {
			fmt.Fprintf(&b, "                 $ %s\n", cmd)
		}
	}
	return b.String()
}
