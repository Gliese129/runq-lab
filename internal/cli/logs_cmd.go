package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gliese129/runq-lab/internal/api"
	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/logfile"
	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// ── runq logs（spec §7.2: GET /tasks/{id}/log, follow → SSE stream）──
//
// v1 起日志一律走 daemon（远端任务经归属 target 的 FS 读取）；CLI 不再
// 直接摸本地文件 —— fsnotify tail 已退役，remote 任务因此免费获得 logs -f。

var logsCmd = &cobra.Command{
	Use:   "logs <task_id>",
	Short: "Show task output (tail; at a terminal, then follow)",
	Long: `Print the tail of a task's log. At a terminal it then follows the log
until Ctrl-C; with stdout piped or captured (scripts, agents) it prints the
tail and exits. --timeout <dur> follows for at most that long, terminal or not.`,
	Example: `  runq logs a3f9              # last 200 lines, then follow at a terminal
  runq logs a3f9 --no-follow  # print tail and exit
  runq logs a3f9 --timeout 2m # follow for up to 2 minutes
  runq logs a3f9 -n 500       # tail size
  runq logs a3f9 --grep 'loss|throughput'          # last 200 matching lines
  runq logs a3f9 --grep '^(DONE|LOAD)' -n 5        # last 5 matches (extended regex)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		noFollow, _ := cmd.Flags().GetBool("no-follow")
		lines, _ := cmd.Flags().GetInt("lines")
		pattern, _ := cmd.Flags().GetString("grep")
		timeout, _ := cmd.Flags().GetDuration("timeout")
		follow := logsFollow(noFollow, timeout, stdoutIsTerminal())
		if follow && timeout > 0 {
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			cmd.SetContext(ctx)
			defer func() {
				if ctx.Err() == context.DeadlineExceeded {
					fmt.Println(utils.Dimf("── stopped following after %s (--timeout) ──", shortDuration(timeout)))
				}
			}()
		}

		if err := consumerRetired(id); err != nil {
			return err
		}
		return withBackend(cmd, func(be backend.Backend) error {
			view, err := be.GetTask(cmd.Context(), id)
			if err != nil {
				return err
			}

			fmt.Printf("%s  %s  %s\n",
				utils.IDColor(view.ID),
				utils.StatusColor(view.Status),
				utils.Dimf("%s", view.LogPath))

			if pattern != "" {
				return printLogGrep(cmd, be, view.ID, pattern, lines)
			}

			// First paint: tail view (spec — the entry point has no byte
			// coordinates; the returned NextOffset seeds the follow).
			page, err := be.TaskLogTail(cmd.Context(), view.ID, lines)
			if err != nil {
				return err
			}
			for _, l := range page.Lines {
				fmt.Println(l)
			}
			if !follow {
				return nil
			}
			return followLogSSE(cmd, view.ID, page.NextOffset)
		})
	},
}

// stdoutIsTerminal is a var so tests can play terminal / pipe.
var stdoutIsTerminal = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

// logsFollow decides whether `runq logs` follows after the tail. Following
// is for a person watching a terminal; a captured stdout (scripts, agents)
// gets the tail and returns instead of hanging. --timeout asks for a
// bounded follow, so it follows anywhere.
func logsFollow(noFollow bool, timeout time.Duration, tty bool) bool {
	return !noFollow && (tty || timeout > 0)
}

// printLogGrep prints the last n lines of one task log that match an
// extended regex. The grep runs on the target that owns the log, so only
// the matches travel — no follow (a filtered stream is a different tool).
func printLogGrep(cmd *cobra.Command, be backend.Backend, taskID, pattern string, n int) error {
	grepper, ok := be.(interface {
		TaskLogGrep(context.Context, string, string, int) ([]backend.LogMatch, error)
	})
	if !ok {
		return fmt.Errorf("logs --grep: %w", backend.ErrNotSupported)
	}
	matches, err := grepper.TaskLogGrep(cmd.Context(), taskID, pattern, n)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		fmt.Println(utils.Dimf("(no matching lines)"))
		return nil
	}
	for _, m := range matches {
		fmt.Printf("%s %s\n", utils.Dimf("%6d", m.Line), m.Text)
	}
	return nil
}

// followLogSSE consumes GET /tasks/{id}/log/stream (spec §6.3):
// text/event-stream, events named "lines", data = LogPage JSON. The
// next_offset of each page is the reconnect anchor — on stream end
// (daemon restart, transient error) we resume from where we stopped.
func followLogSSE(cmd *cobra.Command, taskID string, offset int64) error {
	// Timeout 0: a follow stream is open-ended; lifetime is ctx-controlled.
	client := api.NewClientWithTimeout(getSocketPath(), 0)
	for {
		path := "/api/v1/tasks/" + taskID + "/log/stream?offset=" + strconv.FormatInt(offset, 10)
		resp, err := client.Do(cmd.Context(), "GET", path, nil)
		if err != nil {
			if cmd.Context().Err() != nil {
				return nil // ^C
			}
			return err
		}

		sc := bufio.NewScanner(resp.Body)
		// A page can carry up to the server's page byte budget.
		sc.Buffer(make([]byte, 0, 64*1024), logfile.MaxPageSize+1<<20)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue // event:/heartbeat/blank lines
			}
			var page backend.LogPage
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &page); err != nil {
				continue
			}
			if page.Offset < offset {
				fmt.Println(utils.Dimf("── log rotated, restarting from top ──"))
			}
			for _, l := range page.Lines {
				fmt.Println(l)
			}
			offset = page.NextOffset
		}
		resp.Body.Close()
		if cmd.Context().Err() != nil {
			return nil
		}
		// Stream ended without cancellation → reconnect from offset.
	}
}

func init() {
	logsCmd.Flags().Bool("no-follow", false, "Print tail and exit (the default when stdout is not a terminal)")
	logsCmd.Flags().Duration("timeout", 0, "Follow for at most this long, then exit (follows even when stdout is not a terminal)")
	logsCmd.Flags().IntP("lines", "n", logfile.DefaultPageLines, "Tail size (lines); with --grep, how many of the last matches")
	logsCmd.Flags().String("grep", "", "Only lines matching this extended regex (grepped on the target; implies --no-follow)")

	logsCmd.GroupID = groupCore
	rootCmd.AddCommand(logsCmd)
}
