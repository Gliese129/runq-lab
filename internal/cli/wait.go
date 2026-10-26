package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/spf13/cobra"
)

// ── global --wait[=max] (approach/wait-global-flag) ──
//
// Instead of `runq ps` → sleep → `runq ps` loops (each one re-reading the
// whole table into an agent's context), ps and show can block until what
// they would show changes, everything shown has finished, or the max is
// reached — then print their normal output once. The wait polls the local
// runq service, which answers from its store; the service keeps polling the
// scheduler on its own cadence, so waiting adds no SSH traffic.

const (
	waitDefault  = "30m"
	waitInterval = 5 * time.Second
	// waitAnnotation marks the commands that accept --wait.
	waitAnnotation = "runq/wait"
)

var errWaitUnsupported = fmt.Errorf("--wait works with `runq ps`, `runq ps <jb…>` and `runq show <jb…|tk…>`")

// waitSnapshot is what one poll saw: a fingerprint of the statuses the
// command would show, whether all of it has finished, and the scheduler
// poll time the data carries (nil when the data carries none).
type waitSnapshot struct {
	Fingerprint string
	Terminal    bool
	RefreshedAt *int64
}

// waitResult is how a wait ended; --json output carries it as "wait".
type waitResult struct {
	// Outcome: "finished" (everything shown is terminal), "changed" or
	// "timeout" (nothing changed within the max).
	Outcome     string `json:"outcome"`
	WaitedSec   int64  `json:"waited_seconds"`
	LastRefresh int64  `json:"last_refresh"` // unix; the service's last scheduler poll
	max         time.Duration
}

// waiter polls fetch until the snapshot changes, turns terminal, or max
// passes. now/after are injectable so tests run on a fake clock.
type waiter struct {
	max      time.Duration
	interval time.Duration
	now      func() time.Time
	after    func(time.Duration) <-chan time.Time
}

func newWaiter(max time.Duration) waiter {
	return waiter{max: max, interval: waitInterval, now: time.Now, after: time.After}
}

func (w waiter) wait(ctx context.Context, fetch func(context.Context) (waitSnapshot, error)) (*waitResult, error) {
	start := w.now()
	res := &waitResult{max: w.max}
	finish := func(outcome string, s waitSnapshot, at time.Time) (*waitResult, error) {
		res.Outcome = outcome
		res.WaitedSec = int64(at.Sub(start).Round(time.Second) / time.Second)
		res.LastRefresh = at.Unix()
		if s.RefreshedAt != nil {
			res.LastRefresh = *s.RefreshedAt
		}
		return res, nil
	}
	first, err := fetch(ctx)
	if err != nil {
		return nil, err
	}
	if first.Terminal {
		return finish("finished", first, start)
	}
	deadline := start.Add(w.max)
	last := first
	for {
		remaining := deadline.Sub(w.now())
		if remaining <= 0 {
			return finish("timeout", last, w.now())
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("--wait interrupted")
		case <-w.after(min(w.interval, remaining)):
		}
		s, err := fetch(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("--wait interrupted")
			}
			return nil, err
		}
		last = s
		switch {
		case s.Terminal:
			return finish("finished", s, w.now())
		case s.Fingerprint != first.Fingerprint:
			return finish("changed", s, w.now())
		}
	}
}

// waitMax reads the global --wait flag: 0 when it was not given.
func waitMax(cmd *cobra.Command) (time.Duration, error) {
	f := cmd.Root().PersistentFlags().Lookup("wait")
	if f == nil || !f.Changed {
		return 0, nil
	}
	d, err := time.ParseDuration(f.Value.String())
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("--wait=%s: want a duration like 10m or 2h", f.Value.String())
	}
	return d, nil
}

// checkWaitSupported is the root pre-run: --wait on any command other than
// ps / show is an error naming the ones that take it.
func checkWaitSupported(cmd *cobra.Command, _ []string) error {
	if f := cmd.Root().PersistentFlags().Lookup("wait"); f == nil || !f.Changed {
		return nil
	}
	if cmd.Annotations[waitAnnotation] == "" {
		return errWaitUnsupported
	}
	_, err := waitMax(cmd)
	return err
}

// runWait blocks per --wait before a command prints; nil result = no --wait.
// Ctrl-C (or SIGTERM) ends the wait with an error.
func runWait(cmd *cobra.Command, fetch func(context.Context) (waitSnapshot, error)) (*waitResult, error) {
	max, err := waitMax(cmd)
	if err != nil || max == 0 {
		return nil, err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return newWaiter(max).wait(ctx, fetch)
}

// joinWaitValue lets `--wait 10m` mean `--wait=10m`: with an optional
// value pflag would otherwise read 10m as a positional argument (a job id).
func joinWaitValue(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			return append(out, args[i:]...)
		}
		if args[i] == "--wait" && i+1 < len(args) {
			if _, err := time.ParseDuration(args[i+1]); err == nil {
				out = append(out, "--wait="+args[i+1])
				i++
				continue
			}
		}
		out = append(out, args[i])
	}
	return out
}

// line is the one-line wait summary printed after the normal output.
func (r *waitResult) line() string {
	refresh := formatRefresh(time.Unix(r.LastRefresh, 0), time.Now())
	switch r.Outcome {
	case "timeout":
		return fmt.Sprintf("no change in %s (last refresh %s)", shortDuration(r.max), refresh)
	case "changed":
		return fmt.Sprintf("changed after %s (last refresh %s)", shortDuration(time.Duration(r.WaitedSec)*time.Second), refresh)
	}
	return fmt.Sprintf("all finished (last refresh %s)", refresh)
}

func (r *waitResult) print(w io.Writer) {
	if r != nil {
		fmt.Fprintln(w, utils.Dimf("%s", r.line()))
	}
}

func formatRefresh(t, now time.Time) string {
	if y, m, d := t.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return t.Format("15:04:05")
	}
	return t.Format("2006-01-02 15:04:05")
}

// shortDuration renders 30m0s as 30m and 2h0m0s as 2h.
func shortDuration(d time.Duration) string {
	s := d.Round(time.Second).String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// ── fingerprints: what each command shows ──

func jobsSnapshot(jobs []backend.JobSummary) waitSnapshot {
	parts := make([]string, len(jobs))
	terminal := true
	var live, all []*int64
	for i, j := range jobs {
		c := j.Tasks
		parts[i] = fmt.Sprintf("%s=%s:%d/%d/%d/%d/%d/%d", j.ID, j.Status, c.Total, c.Pending, c.Running, c.Completed, c.Failed, c.Unknown)
		all = append(all, j.RefreshedAt)
		if !store.IsTerminalJobStatus(j.Status) {
			terminal = false
			live = append(live, j.RefreshedAt)
		}
	}
	return waitSnapshot{Fingerprint: joinSorted(parts), Terminal: terminal, RefreshedAt: oldestRefresh(live, all)}
}

func tasksSnapshot(tasks []backend.TaskView) waitSnapshot {
	parts := make([]string, len(tasks))
	terminal := true
	var live, all []*int64
	for i, t := range tasks {
		parts[i] = t.ID + "=" + t.Status
		all = append(all, t.RefreshedAt)
		if !store.IsTerminalTaskStatus(t.Status) {
			terminal = false
			live = append(live, t.RefreshedAt)
		}
	}
	return waitSnapshot{Fingerprint: joinSorted(parts), Terminal: terminal, RefreshedAt: oldestRefresh(live, all)}
}

func jobDetailSnapshot(d *backend.JobDetail) waitSnapshot {
	s := tasksSnapshot(d.Tasks)
	j := jobsSnapshot([]backend.JobSummary{d.Job})
	return waitSnapshot{Fingerprint: j.Fingerprint + "|" + s.Fingerprint, Terminal: j.Terminal, RefreshedAt: j.RefreshedAt}
}

func taskSnapshot(t *backend.TaskView) waitSnapshot {
	return tasksSnapshot([]backend.TaskView{*t})
}

func joinSorted(parts []string) string {
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// oldestRefresh is the honest freshness of a set: the oldest poll among
// unfinished items (finished ones are no longer polled), else among all.
func oldestRefresh(live, all []*int64) *int64 {
	for _, set := range [][]*int64{live, all} {
		var oldest *int64
		for _, at := range set {
			if at != nil && (oldest == nil || *at < *oldest) {
				oldest = at
			}
		}
		if oldest != nil {
			return oldest
		}
	}
	return nil
}
