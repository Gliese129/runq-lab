package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/utils"
)

// TaskDir returns the task workspace path under an already-selected root.
// Root selection is a backend concern; this helper only joins mechanically.
func TaskDir(root, taskID string) string {
	return path.Join(root, taskID)
}

func ParamsPath(dir string) string {
	return path.Join(dir, "params.json")
}

func MetricsPath(dir string) string {
	return path.Join(dir, "metrics.jsonl")
}

// ResultsPath is the runq.record data plane (three-file contract, RQ2-1):
// bounded result records ingested into the store in full, one JSON object
// per line. The SDK derives this path as metrics.jsonl's sibling.
func ResultsPath(dir string) string {
	return path.Join(dir, "results.jsonl")
}

// LinksPath is the SDK's runq.link sink: a small JSON object name → URL
// (e.g. the task's W&B run), rewritten atomically on every call.
func LinksPath(dir string) string {
	return path.Join(dir, "links.json")
}

// PyramidPath is the multi-resolution metrics index built next to
// metrics.jsonl by `runq metrics-index build` (pyramid.go, this package).
func PyramidPath(dir string) string {
	return path.Join(dir, "metrics.pyr")
}

func OutputsDir(dir string) string { return path.Join(dir, "outputs") }

func CheckpointsDir(dir string) string {
	return path.Join(dir, "checkpoints")
}

func WandbConfigPath(dir string) string {
	return path.Join(dir, "wandb_config.json")
}

func NotePath(dir string) string {
	return path.Join(dir, "note.txt")
}

func ActivityPath(dir string) string {
	return path.Join(dir, "activity.tsv")
}

// TaskMeta is the JSON schema for task.meta. Add new fields here as needed;
// existing files without the field will unmarshal to their zero value.
// Activity data (ts/bytes/lines) is now recorded directly in activity.tsv
// by the sidecar; this struct is reserved for other post-mortem metadata.
type TaskMeta struct {
	// Future fields: TotalLines, ErrorCount, TimeRange, etc.
}

// Write creates <dir>/checkpoints/, writes params.json, and (when wandb is
// configured at project level) writes wandb_config.json. When note is non-empty
// it also writes note.txt. All writes are atomic via utils.AtomicWriteFile so
// the Python SDK never observes a half-written file.
func Write(dir string, params job.TaskParams, wandb *project.WandbConfig, note string) error {
	if err := os.MkdirAll(OutputsDir(dir), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(CheckpointsDir(dir), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	paramsJSON, err := json.MarshalIndent(params, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal params: %w", err)
	}
	if err := utils.AtomicWriteFile(ParamsPath(dir), paramsJSON, 0o644); err != nil {
		return fmt.Errorf("write params.json: %w", err)
	}

	if wandb != nil {
		wandbJSON, err := json.MarshalIndent(wandb, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal wandb config: %w", err)
		}
		if err := utils.AtomicWriteFile(WandbConfigPath(dir), wandbJSON, 0o644); err != nil {
			return fmt.Errorf("write wandb_config.json: %w", err)
		}
	}

	if note != "" {
		if err := utils.AtomicWriteFile(NotePath(dir), []byte(note), 0o644); err != nil {
			return fmt.Errorf("write note.txt: %w", err)
		}
	}
	return nil
}

// WriteFS is Write through an explicit filesystem — the remote-target path:
// task workspaces live on the TARGET's filesystem (rfs.SSHFS), not the
// client's. nil falls back to the local, atomic-write implementation.
//
// Remote note: sftp has no atomic rename-write. Ordering makes this safe
// anyway — everything here lands BEFORE the task is submitted, so the SDK on
// the compute node can never observe a half-written file.
func WriteFS(fsys rfs.FS, dir string, params job.TaskParams, wandb *project.WandbConfig, note string) error {
	if fsys == nil {
		return Write(dir, params, wandb, note)
	}
	if err := fsys.MkdirAll(OutputsDir(dir), 0o755); err != nil {
		return err
	}
	if err := fsys.MkdirAll(CheckpointsDir(dir), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	paramsJSON, err := json.MarshalIndent(params, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal params: %w", err)
	}
	if err := fsys.WriteFile(ParamsPath(dir), paramsJSON, 0o644); err != nil {
		return fmt.Errorf("write params.json: %w", err)
	}

	if wandb != nil {
		wandbJSON, err := json.MarshalIndent(wandb, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal wandb config: %w", err)
		}
		if err := fsys.WriteFile(WandbConfigPath(dir), wandbJSON, 0o644); err != nil {
			return fmt.Errorf("write wandb_config.json: %w", err)
		}
	}

	if note != "" {
		if err := fsys.WriteFile(NotePath(dir), []byte(note), 0o644); err != nil {
			return fmt.Errorf("write note.txt: %w", err)
		}
	}
	return nil
}

// MarkerPrefix starts the empty file that names an id-named directory
// (<pj…>/ or <jb…>/) for humans: "0-<name>" sorts first in `ls`.
const MarkerPrefix = "0-"

// MarkerName is the marker file name for a display name: path separators
// and control characters become '-', capped at 100 bytes on a rune
// boundary. Empty name → "" (no marker).
func MarkerName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if r == '/' || r == '\\' || r < 0x20 || r == 0x7f {
			r = '-'
		}
		if b.Len()+utf8.RuneLen(r) > 100 {
			break
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return ""
	}
	return MarkerPrefix + b.String()
}

// WriteMarker replaces dir's marker with one for name (best effort: the
// marker is decoration, never a source of truth).
func WriteMarker(ctx context.Context, fsys rfs.FS, dir, name string) error {
	marker := MarkerName(name)
	if marker == "" {
		return nil
	}
	script := "rm -f " + utils.ShellQuote(dir) + "/" + MarkerPrefix + "* && : > " + utils.ShellQuote(path.Join(dir, marker))
	_, stderr, code, err := fsys.Exec(ctx, "sh", "-c", script)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("write marker in %s: exit %d: %s", dir, code, strings.TrimSpace(string(stderr)))
	}
	return nil
}
