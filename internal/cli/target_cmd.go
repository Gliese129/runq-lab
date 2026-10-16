package cli

// ── runq target：target 的一等名词命令组（RQ-65）──
//
// target 是一等实体，它的动词理应住在一个屋檐下：
//
//	runq target add/ls/show/edit/check <name>   配置 CRUD（原 runq config add/...）
//	runq target use <name> [-p]                 选择活动 target（原 config set-target）
//	runq target connect <name>...               连接仪式（host key 信任 + 远端 CLI + forward）
//	runq target disconnect <name>...            收掉 forward，remote_cli: false
//	runq target rm <name>                       从 config.yaml 删除（仍被 project 使用时拒绝）
//
// `runq config` 回归纯键值心智（set/get/list）。顶层 `runq connect` 保留为
// 别名——它是新用户的第一动作，值得一个短入口。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gliese129/runq-lab/internal/api"
	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/config"

	"github.com/spf13/cobra"
)

var targetCmd = &cobra.Command{
	Use:   "target",
	Short: "Configure compute targets (add, show, check, connect, select)",
}

var targetUseCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Select the active compute target for this session (or persistently with -p)",
	Long: `Select the active compute target. Without -p, writes to ~/.runq/.active-target
(session-scoped, overridden by $RUNQ_TARGET or --target flag). With -p,
writes to config.yaml default_target (persists across sessions).
This scopes queries; submit always uses -t > job.target > project.target.`,
	Example: `  runq target use tsubame       # session-scoped
  runq target use tsubame -p    # persistent default`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigSetTarget,
}

var targetDisconnectCmd = &cobra.Command{
	Use:          "disconnect <name>...",
	Short:        "Stop the remote CLI forward and disable remote_cli",
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE:         runTargetDisconnect,
}

func runTargetDisconnect(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	changed := false
	for _, name := range args {
		found := false
		for i := range cfg.Targets {
			if cfg.Targets[i].Name != name {
				continue
			}
			found = true
			if cfg.Targets[i].RemoteCLI {
				cfg.Targets[i].RemoteCLI = false
				changed = true
				fmt.Printf("✓ remote_cli disabled for %q\n", name)
			} else {
				fmt.Printf("- %q was not connected\n", name)
			}
		}
		if !found {
			return fmt.Errorf("target %q not found", name)
		}
	}
	if changed {
		if err := config.SaveGlobal(cfg); err != nil {
			return err
		}
	}
	// Live teardown, best-effort: a stopped daemon simply won't start the
	// forward next boot (config already says so).
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()
	client := api.NewClient(getSocketPath())
	for _, name := range args {
		resp, err := client.Do(ctx, http.MethodPost, "/api/v1/targets/"+url.PathEscape(name)+"/disconnect", nil)
		if err != nil {
			fmt.Println("Daemon not running — nothing live to stop.")
			break
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			fmt.Printf("✓ forward stopped for %q\n", name)
		}
	}
	return nil
}

var targetRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Remove a target from config.yaml (refused while a project still uses it)",
	Long: `Remove a target from config.yaml.

Refused while a registered (not archived) project names the target as its
home or in targets: — point those projects elsewhere first. Tasks already
running there keep running until they finish; nothing new can be submitted.
Old jobs stay listed and their logs stay readable. Removing the default
target leaves no default until you set one.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE:         runTargetRm,
}

func runTargetRm(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	client := api.NewClient(getSocketPath())
	var out struct {
		WasDefault  bool `json:"was_default"`
		ActiveTasks int  `json:"active_tasks"`
	}
	// Through the service: it owns the "still in use" check and retires
	// the target's lane right away.
	resp, err := client.Do(ctx, http.MethodDelete, "/api/v1/targets/"+url.PathEscape(args[0]), nil)
	if err != nil {
		return fmt.Errorf("the runq service must be running to remove a target (runq serve -d): %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e backend.ErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return errors.New(e.Error)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	fmt.Printf("✓ removed target %q\n", args[0])
	if out.ActiveTasks > 0 {
		fmt.Printf("  %d task(s) already there keep running until they finish\n", out.ActiveTasks)
	}
	if out.WasDefault {
		fmt.Println("  it was the default target — set a new one: runq config set default_target <name>")
	}
	return nil
}

func init() {
	targetUseCmd.Flags().BoolP("persist", "p", false, "Write to config default_target instead of session file")

	targetCmd.AddCommand(configAddCmd, configShowCmd, configCheckCmd)
	targetCmd.AddCommand(targetUseCmd, connectCmd, targetDisconnectCmd, targetRmCmd)
	targetCmd.GroupID = groupManagement
	rootCmd.AddCommand(targetCmd)
}
