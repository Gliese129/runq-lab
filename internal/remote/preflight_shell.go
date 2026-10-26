package remote

import (
	"context"
	"path"

	"github.com/gliese129/runq-lab/internal/preflight"
	"github.com/gliese129/runq-lab/internal/submitplan"
)

// addShellSyntax completes the preflight report with shell_syntax: bash -n
// on the first task's run.sh and submit line, rendered exactly as Prepare
// writes them. A submit_template that does not render is a hard plan error
// elsewhere; here it only means there is no line to parse.
//
// Disk space / inode headroom for the data directory belongs to
// internal/diskspace; its warnings join the preview next to this report.
func (b *Backend) addShellSyntax(ctx context.Context, plan *submitplan.Plan) {
	if len(plan.Tasks) == 0 {
		return
	}
	t := plan.Tasks[0]
	runsh := path.Join(t.TaskDir, runScriptName)
	cmd, err := renderSubmitCmd(b.Cfg.SubmitTemplate, t, *plan, runsh)
	if err != nil {
		plan.Preflight.Add(preflight.CheckResult{Name: "shell_syntax", Status: "skipped", Detail: "submit_template does not render"})
		return
	}
	plan.Preflight.Add(preflight.ShellSyntax(ctx, []string{"run.sh", "submit line"}, map[string]string{
		"run.sh":      b.buildRunScript(t, *plan),
		"submit line": b.envPrelude(plan.Env).Render() + cmd,
	}))
}
