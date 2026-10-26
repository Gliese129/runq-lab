package cli

import (
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/spf13/cobra"
)

// ANSI helpers for help template (only active when stdout is a terminal).
var helpFuncs = template.FuncMap{
	"bold":      func(s string) string { return utils.Bold(s) },
	"underline": func(s string) string { return utils.Underline(s) },
	"dim":       func(s string) string { return utils.Dimf("%s", s) },
	"ungroupedCommands": func(cmd *cobra.Command) []*cobra.Command {
		var commands []*cobra.Command
		for _, child := range cmd.Commands() {
			if child.GroupID == "" && !child.Hidden && child.IsAvailableCommand() && child.Name() != "help" {
				commands = append(commands, child)
			}
		}
		return commands
	},
	"commandsWithGroup": func(cmd *cobra.Command, groupID string) []*cobra.Command {
		var commands []*cobra.Command
		for _, child := range cmd.Commands() {
			if child.GroupID == groupID && !child.Hidden && child.IsAvailableCommand() {
				commands = append(commands, child)
			}
		}
		return commands
	},
}

// Command group IDs.
const (
	groupCore       = "core"
	groupManagement = "mgmt"
	groupDiag       = "diag"
)

var rootCmd = &cobra.Command{
	Use:   "runq",
	Short: "Run ablation sweeps on HPC clusters — built to be driven by AI agents",
	Long: `runq — run ablation sweeps on HPC clusters, built to be driven by AI agents.

  A project is a code base; a job.yaml is one experiment (command, target,
  fixed_params, sweep). Every task gets its own workspace, log and params.json;
  the training code reads swept values with the Python SDK (ctx.params), so
  ablation axes need no command-line plumbing and no wrapper scripts.

  Typical loop:
    runq project project.yaml               register / update the code base
    runq draft job.yaml                     plan: tasks, target, rendered command -> dr…
    runq submit dr… --json                  submit -> {job_id, total_tasks}
    runq ps <job_id> --json --fresh         task states
    runq logs <task_id> --grep 'loss' -n 20 last matching log lines
    runq kill <job_id|task_id>

  Agents: run "runq skill install" once — the skill has the rules and recipes.`,
	// main() prints the returned error; without this cobra prints it too
	// and every failure shows up twice.
	SilenceErrors: true,
	// Business errors (a plan error, target unreachable) are not usage
	// errors — burying them under 30 lines of flags helps nobody (RQ-65
	// #6). Cobra checks Root().SilenceUsage, so this covers every command;
	// genuine usage errors (unknown flag) get a --help hint via the
	// FlagErrorFunc below instead of the full usage dump.
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().String("socket", "", "path to the runq service socket (default: the local service)")
	// --fresh（spec §7 全局 flag）：绕过 daemon 缓存 TTL 强制刷新（受服务端
	// 5min 下限保护）；未真刷时提示「x 秒前已刷新」而非静默 no-op（D22）。
	rootCmd.PersistentFlags().Bool("fresh", false, "force-refresh cached target data before the command (rate-limited server-side)")
	// --wait[=max]：ps / show 阻塞到显示的状态变化、全部结束或到上限，再照常输出
	// （approach/wait-global-flag）。其他命令带了报错。
	rootCmd.PersistentFlags().String("wait", "", "ps / show: wait up to `max` for the statuses shown to change or all finish, then print (--wait=10m; bare --wait = "+waitDefault+")")
	rootCmd.PersistentFlags().Lookup("wait").NoOptDefVal = waitDefault
	rootCmd.PersistentPreRunE = checkWaitSupported

	// Register command groups.
	rootCmd.AddGroup(
		&cobra.Group{ID: groupCore, Title: "Core Commands:"},
		&cobra.Group{ID: groupManagement, Title: "Management:"},
		&cobra.Group{ID: groupDiag, Title: "Setup & Diagnostics:"},
	)

	// Flag errors ARE usage errors — point at --help without the dump.
	rootCmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return fmt.Errorf("%w\nRun '%s --help' for usage", err, c.CommandPath())
	})

	// Custom help template with bold headers and underlined section titles.
	rootCmd.SetUsageTemplate(usageTemplate)
	// Every usage section opens with its own blank line, so the description
	// is followed by exactly one newline (cobra's default adds two).
	rootCmd.SetHelpTemplate(`{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}
{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`)

	// Merge help functions for bold/underline.
	cobra.AddTemplateFuncs(helpFuncs)

	// Don't show "completion" command.
	rootCmd.CompletionOptions.HiddenDefaultCmd = true
}

// Execute is the entry point called from main.
func Execute() error {
	rootCmd.SetOut(os.Stdout)
	rootCmd.SetArgs(joinWaitValue(os.Args[1:]))
	printRemoteContextBanner()
	err := rootCmd.Execute()
	if err != nil && os.Getenv("RUNQ_SOCKET") != "" && isSocketDialError(err) {
		// Remote-CLI perspective (RQ-74): a dead forwarded socket means the
		// OWNING machine is gone, not this login node — every command says
		// so, not just doctor, and says what (not) to do about it.
		return fmt.Errorf("%w\n\nremote CLI: the runq service on your own machine is not reachable through the forward (asleep / offline / stopped). It reconnects automatically once that machine is back — nothing to fix on this login node. Details: runq doctor", err)
	}
	return err
}

// isSocketDialError matches the failure shapes of a unix-socket dial against
// a forward whose owning daemon is gone (refused / file missing / EOF).
func isSocketDialError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, sig := range []string{"dial unix", "connection refused", "no such file or directory", "broken pipe"} {
		if strings.Contains(msg, sig) {
			return true
		}
	}
	return false
}

// printRemoteContextBanner prints ONE stderr line before every command when
// runq is running as a remote CLI (RUNQ_SOCKET set by the login-node env
// file): which target this shell is bound to, i.e. whose daemon the command
// will reach. With several machines forwarding to one cluster account this
// is the cheapest guard against submitting through the wrong context
// (RQ-74). stderr on purpose — `runq ps --json | jq` must stay parseable.
func printRemoteContextBanner() {
	if os.Getenv("RUNQ_SOCKET") == "" {
		return
	}
	// Never during shell-completion callbacks: cobra's __complete protocol
	// runs `runq __complete ...` on every TAB, and some shells surface
	// stderr — a banner there would garble completion for remote users.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd, "completion":
			return
		}
	}
	target := os.Getenv("RUNQ_TARGET")
	if target == "" {
		target = "(RUNQ_TARGET unset)"
	}
	fmt.Fprintf(os.Stderr, "%s\n", utils.Dimf("runq target: %s (remote CLI)", target))
}

// usageTemplate is a customized Cobra usage template with grouped commands
// and ANSI formatting.
// usageTemplate renders USAGE ONLY — no .Short/.Long here: cobra's help
// template already prints LongOrShort before calling usage, so including
// them again double-prints every description (RQ-65 #6).
const usageTemplate = `{{- if .Runnable}}
{{underline "Usage:"}}
  {{.UseLine}}
{{end}}
{{- range $g := .Groups}}{{with (commandsWithGroup $ $g.ID)}}
{{bold $g.Title}}
{{- range .}}
  {{rpad .Name .NamePadding}}  {{.Short}}
{{- end}}
{{end}}{{end}}
{{- with (ungroupedCommands .)}}
{{underline "Commands:"}}
{{- range .}}
  {{rpad .Name .NamePadding}}  {{.Short}}
{{- end}}
{{end}}
{{- if .HasAvailableLocalFlags}}
{{underline "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
{{end}}
{{- if .HasAvailableInheritedFlags}}
{{underline "Global Flags:"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}
{{end}}
{{- if .HasExample}}
{{underline "Examples:"}}
{{.Example}}
{{end}}
{{- if .HasAvailableSubCommands}}
Use "{{.CommandPath}} [command] --help" for more information about a command.
{{end}}
{{dim "Docs & annotated config examples: https://github.com/gliese129/runq-lab — docs/ and examples/"}}
`
