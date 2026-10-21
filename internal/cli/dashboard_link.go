package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"time"

	"github.com/gliese129/runq-lab/internal/api"
	"github.com/gliese129/runq-lab/internal/backend"
	"github.com/gliese129/runq-lab/internal/utils"
)

// dashboardBase asks the runq service where its dashboard UI is served
// (/health dashboard_url). Through a remote CLI forward this is the URL on
// the researcher's own machine — the one the link is for. "" when the
// service serves no UI or does not answer; links are best-effort and never
// fail a command.
func dashboardBase(ctx context.Context, be backend.Backend) string {
	p, ok := be.(*api.Proxy)
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	h, err := p.Health(ctx)
	if err != nil {
		return ""
	}
	base, _ := h["dashboard_url"].(string)
	return base
}

// printDashboardLink prints a dim "dashboard: <url>" line under text output.
func printDashboardLink(w io.Writer, url string) {
	if url != "" {
		fmt.Fprintln(w, utils.Dimf("dashboard: %s", url))
	}
}

// openBrowser opens url in the default browser. Without a launcher (a
// headless login node) the printed URL is the result, so that is not an
// error.
func openBrowser(ctx context.Context, w io.Writer, url string) error {
	program := "xdg-open"
	if runtime.GOOS == "darwin" {
		program = "open"
	}
	err := exec.CommandContext(ctx, program, url).Run()
	if errors.Is(err, exec.ErrNotFound) {
		fmt.Fprintln(w, utils.Dimf("no browser launcher (%s) here — open the URL above on your own machine", program))
		return nil
	}
	return err
}
