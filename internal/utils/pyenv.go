package utils

import (
	"path/filepath"
	"strings"
)

// WrapCommand prepends shell commands to activate the given Python environment
// before running the actual task command. Returns the command unchanged
// if envType is "system", empty, or unrecognized.
//
// envType: "venv", "uv", "conda", "system", or "" (no-op).
// envPath: venv/uv directory (e.g. ".venv"); relative paths resolved against workingDir.
// envName: conda environment name (e.g. "torch").
func WrapCommand(envType, envPath, envName, cmd, workingDir string) string {
	switch envType {
	case "venv", "uv":
		// uv creates standard venvs — activation is identical.
		path := envPath
		if path == "" {
			path = ".venv"
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(workingDir, path)
		}
		return ". " + shellQuote(filepath.Join(path, "bin", "activate")) + " && " + cmd
	case "conda":
		name := envName
		if name == "" {
			name = "base"
		}
		// conda activate requires shell initialization first.
		return `eval "$(conda shell.bash hook)" && conda activate ` + shellQuote(name) + " && " + cmd
	default:
		// "system", "", or unknown — run as-is.
		return cmd
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
