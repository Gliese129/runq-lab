package cli

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/backend"
)

// fakeClock drives a waiter without real sleeps: every after() advances
// the clock by the requested duration and fires at once.
type fakeClock struct{ t time.Time }

func (c *fakeClock) waiter(max time.Duration) waiter {
	return waiter{
		max:      max,
		interval: 5 * time.Second,
		now:      func() time.Time { return c.t },
		after: func(d time.Duration) <-chan time.Time {
			c.t = c.t.Add(d)
			ch := make(chan time.Time, 1)
			ch <- c.t
			return ch
		},
	}
}

// script returns a fetch that serves the given task-status rounds, then
// repeats the last one.
func script(rounds ...[]string) (func(context.Context) (waitSnapshot, error), *int) {
	calls := 0
	return func(context.Context) (waitSnapshot, error) {
		r := rounds[min(calls, len(rounds)-1)]
		calls++
		tasks := make([]backend.TaskView, len(r))
		for i, st := range r {
			tasks[i] = backend.TaskView{ID: "tk" + string(rune('a'+i)), Status: st}
		}
		return tasksSnapshot(tasks), nil
	}, &calls
}

func TestWaitReturnsWhenStatusChanges(t *testing.T) {
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	fetch, calls := script(
		[]string{"running", "pending"},
		[]string{"running", "pending"},
		[]string{"running", "running"},
	)
	res, err := c.waiter(30*time.Minute).wait(context.Background(), fetch)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != "changed" || res.WaitedSec != 10 || *calls != 3 {
		t.Fatalf("got %+v after %d fetches, want changed after 10s / 3 fetches", res, *calls)
	}
}

func TestWaitReturnsAtOnceWhenAlreadyFinished(t *testing.T) {
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	fetch, calls := script([]string{"success", "failed", "killed"})
	res, err := c.waiter(30*time.Minute).wait(context.Background(), fetch)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != "finished" || res.WaitedSec != 0 || *calls != 1 {
		t.Fatalf("got %+v after %d fetches, want finished at once", res, *calls)
	}
}

func TestWaitStopsWhenEverythingFinishes(t *testing.T) {
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	fetch, _ := script([]string{"running"}, []string{"success"})
	res, err := c.waiter(30*time.Minute).wait(context.Background(), fetch)
	if err != nil || res.Outcome != "finished" {
		t.Fatalf("got %+v, %v; want finished", res, err)
	}
}

func TestWaitTimesOutWithLastRefresh(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	c := &fakeClock{t: start}
	polled := start.Unix() - 40
	fetch := func(context.Context) (waitSnapshot, error) {
		return tasksSnapshot([]backend.TaskView{{ID: "tka", Status: "running", RefreshedAt: &polled}}), nil
	}
	res, err := c.waiter(time.Minute).wait(context.Background(), fetch)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != "timeout" || res.WaitedSec != 60 || res.LastRefresh != polled {
		t.Fatalf("got %+v, want timeout after 60s with last_refresh = the service's poll", res)
	}
	if line := res.line(); !strings.HasPrefix(line, "no change in 1m (last refresh ") {
		t.Fatalf("timeout line = %q", line)
	}
}

func TestWaitHonorsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := waiter{max: time.Hour, interval: time.Second, now: time.Now,
		after: func(time.Duration) <-chan time.Time { return nil }} // never fires
	fetch, _ := script([]string{"running"})
	if _, err := w.wait(ctx, fetch); err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("cancelled wait: got %v, want interrupted", err)
	}
}

func TestJobsFingerprintSeesCounts(t *testing.T) {
	a := []backend.JobSummary{{ID: "jb1", Status: "running", Tasks: backend.TaskCountGroup{Total: 4, Running: 2, Pending: 2}}}
	b := []backend.JobSummary{{ID: "jb1", Status: "running", Tasks: backend.TaskCountGroup{Total: 4, Running: 3, Pending: 1}}}
	if jobsSnapshot(a).Fingerprint == jobsSnapshot(b).Fingerprint {
		t.Fatal("a pending→running move inside a running job must change the fingerprint")
	}
	if jobsSnapshot(a).Terminal {
		t.Fatal("a running job is not terminal")
	}
	if !jobsSnapshot([]backend.JobSummary{{ID: "jb1", Status: "partial"}}).Terminal {
		t.Fatal("partial is terminal")
	}
}

func TestWaitFlagOnlyOnPsAndShow(t *testing.T) {
	defer func() {
		f := rootCmd.PersistentFlags().Lookup("wait")
		_ = f.Value.Set("")
		f.Changed = false
		rootCmd.SetArgs(nil)
	}()
	for _, args := range [][]string{{"kill", "jb1", "--wait"}, {"logs", "tk1", "--wait=5m"}, {"show", "dr1", "--wait"}} {
		rootCmd.SetArgs(args)
		err := rootCmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "runq show <jb…|tk…>") {
			t.Errorf("%v: got %v, want the error naming the commands that take --wait", args, err)
		}
		f := rootCmd.PersistentFlags().Lookup("wait")
		_ = f.Value.Set("")
		f.Changed = false
	}
	rootCmd.SetArgs([]string{"ps", "--wait=soon"})
	if err := rootCmd.Execute(); err == nil || !strings.Contains(err.Error(), "duration") {
		t.Errorf("--wait=soon: got %v, want a duration error", err)
	}
}

func TestJoinWaitValue(t *testing.T) {
	got := joinWaitValue([]string{"ps", "jb1", "--wait", "10m", "--json"})
	if want := []string{"ps", "jb1", "--wait=10m", "--json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	got = joinWaitValue([]string{"ps", "--wait", "jb1"})
	if want := []string{"ps", "--wait", "jb1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("a job id after bare --wait stays an argument: got %v", got)
	}
}

func TestLogsFollowsOnlyAtATerminal(t *testing.T) {
	for _, c := range []struct {
		noFollow bool
		timeout  time.Duration
		tty      bool
		want     bool
	}{
		{tty: true, want: true},
		{tty: false, want: false}, // agents / pipes: tail and exit
		{tty: true, noFollow: true, want: false},
		{tty: false, timeout: time.Minute, want: true}, // bounded follow anywhere
		{tty: false, timeout: time.Minute, noFollow: true, want: false},
	} {
		if got := logsFollow(c.noFollow, c.timeout, c.tty); got != c.want {
			t.Errorf("logsFollow(noFollow=%v, timeout=%v, tty=%v) = %v, want %v", c.noFollow, c.timeout, c.tty, got, c.want)
		}
	}
}

func TestShortDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{30 * time.Minute: "30m", 2 * time.Hour: "2h", 90 * time.Second: "1m30s", 10 * time.Second: "10s"} {
		if got := shortDuration(d); got != want {
			t.Errorf("shortDuration(%v) = %q, want %q", d, got, want)
		}
	}
}
