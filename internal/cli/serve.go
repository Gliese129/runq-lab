package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gliese129/runq-lab/internal/api"
	"github.com/gliese129/runq-lab/internal/app"
	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/spf13/cobra"
)

// serveCmd runs the runq service: target routing, status tracking, the
// dashboard, and the CLI socket. It is deliberately not called a daemon so it
// cannot be confused with runqd, the separately installed GPU executor.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the runq service (dashboard + CLI backend); -d to background",
	Long: `Run the runq service: target routing, status tracking, the web dashboard,
and the Unix socket every other runq command talks to.

This is not runqd. runqd is the GPU executor installed separately on GPU
machines; runq serve only routes to it (or to HPC schedulers).`,
	Example: `  runq serve -d          # start in background
  runq serve status      # is it running, where are its files
  runq serve restart     # after upgrading the binary or editing config
  runq serve stop
  RUNQ_DATA_DIR=/data/runq runq serve`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		bg, _ := cmd.Flags().GetBool("detach")
		if bg {
			selfPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve executable path: %w", err)
			}
			_, dataDir := utils.ResolveDataDir()
			logFile, _ := os.OpenFile(filepath.Join(dataDir, "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
			subp := exec.Command(selfPath, "serve")
			subp.Stdin = nil
			subp.Stdout = logFile
			subp.Stderr = logFile
			subp.Dir = "/"                                     // set to root for background
			subp.Env = append(os.Environ(), "RUNQ_IS_CHILD=1") // maybe useful
			if err := subp.Start(); err != nil {
				return err
			}
			fmt.Printf("runq service started in background (pid %d)\n", subp.Process.Pid)
			return nil
		}

		d, err := app.NewDaemon()
		if err != nil {
			return err
		}
		return d.Run()
	},
}

var serveStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the runq service",
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, startTime, err := api.ReadPID(api.DefaultPIDPath())
		if err != nil {
			return err
		}
		if pid == 0 {
			return fmt.Errorf("runq service is not running")
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return err
		}

		ctx, cancel := signal.NotifyContext(context.Background())
		defer cancel()
		deadline, stop := context.WithTimeout(ctx, 10*time.Second)
		defer stop()

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-deadline.Done():
				return fmt.Errorf("process no response in 10s")
			case <-ticker.C:
				if !utils.IsProcessAlive(pid, startTime) {
					return nil
				}
			}
		}
	},
}

var serveRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the runq service (always starts in background)",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Stopping runq service...")
		if err := serveStopCmd.RunE(cmd, args); err != nil {
			return err
		}
		// Force detach mode — restart should never block the terminal.
		fmt.Println("Starting runq service...")
		_ = serveCmd.Flags().Set("detach", "true")
		return serveCmd.RunE(serveCmd, args)
	},
}

var serveStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether the runq service is running and where its files are",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, dataDir := utils.ResolveDataDir()
		paths := utils.PathsFromDataDir(dataDir)
		pid, startTime, err := api.ReadPID(paths.PIDPath)
		if err != nil {
			return err
		}
		running := pid != 0 && utils.IsProcessAlive(pid, startTime)
		if running {
			fmt.Printf("running     pid %d, since %s\n", pid, startTime.Local().Format(time.RFC3339))
		} else {
			fmt.Println("stopped     start with: runq serve -d")
		}
		fmt.Printf("data dir    %s\n", dataDir)
		fmt.Printf("socket      %s\n", paths.SocketPath)
		fmt.Printf("log         %s\n", filepath.Join(dataDir, "daemon.log"))
		fmt.Printf("access log  %s\n", filepath.Join(paths.LogDir, "access.log"))
		if !running {
			return fmt.Errorf("runq service is not running")
		}
		return nil
	},
}

func init() {
	serveCmd.Flags().BoolP("detach", "d", false, "run in background")
	serveCmd.AddCommand(serveStopCmd)
	serveCmd.AddCommand(serveRestartCmd)
	serveCmd.AddCommand(serveStatusCmd)
	serveCmd.GroupID = groupManagement
	rootCmd.AddCommand(serveCmd)
}
