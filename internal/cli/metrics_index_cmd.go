package cli

import (
	"fmt"

	"github.com/gliese129/runq-lab/internal/workspace"
	"github.com/spf13/cobra"
)

// ── runq metrics-index（hidden plumbing）──
//
// 在 metrics.jsonl 所在的机器上构建 metrics.pyr 多分辨率索引。调用方是
// run.sh 的收尾（done marker 之前，`"$RUNQ_BIN" metrics-index build ... ||
// true` —— 索引失败不得污染任务终态）。纯本地 IO：这正是它作为 CLI 子命令
// 而非 daemon 功能存在的理由 —— 构建者必须与文件同侧。

var metricsIndexCmd = &cobra.Command{
	Use:    "metrics-index",
	Hidden: true,
	Short:  "Build the on-target metrics pyramid index (plumbing)",
}

var metricsIndexBuildCmd = &cobra.Command{
	Use:   "build --task-dir <dir>",
	Short: "Build metrics.pyr next to metrics.jsonl (single streaming pass)",
	RunE: func(cmd *cobra.Command, args []string) error {
		taskDir, _ := cmd.Flags().GetString("task-dir")
		if taskDir == "" {
			return fmt.Errorf("--task-dir is required")
		}
		// nil FS = this machine's filesystem — the whole point of running
		// the builder where the file lives.
		stats, err := workspace.BuildPyramid(cmd.Context(), nil, taskDir)
		if err != nil {
			return err
		}
		// Full accounting on stderr: dropped lines must be visible, not
		// silent (bare-NaN lines from Python's json.dumps land here).
		fmt.Fprintf(cmd.ErrOrStderr(), "pyramid: %d keys, %d points, %d other events, %d skipped lines\n",
			stats.Keys, stats.Points, stats.OtherEvents, stats.SkippedLines)
		return nil
	},
}

func init() {
	metricsIndexBuildCmd.Flags().String("task-dir", "", "task directory containing metrics.jsonl")
	metricsIndexCmd.AddCommand(metricsIndexBuildCmd)
	rootCmd.AddCommand(metricsIndexCmd)
}
