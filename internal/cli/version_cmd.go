package cli

import (
	"github.com/gliese129/runq-lab/internal/version"
)

// `runq --version` prints exactly the build version and nothing else: it is
// parsed programmatically (`runq connect` runs it on the remote to decide
// whether the installed binary is stale), so the output is the contract.
func init() {
	rootCmd.Version = version.Version
	rootCmd.SetVersionTemplate("{{.Version}}\n")
}
