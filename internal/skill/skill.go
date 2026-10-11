// Package skill ships the agent skill for runq inside the binary, so the
// guide an agent reads always matches the CLI it is driving.
package skill

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:runq
var files embed.FS

// Name is the skill's directory name under an agent's skills root.
const Name = "runq"

// FS returns the skill tree (SKILL.md + references/).
func FS() fs.FS {
	sub, err := fs.Sub(files, Name)
	if err != nil {
		panic(err) // embed layout is fixed at build time
	}
	return sub
}

// Install writes the skill to <root>/runq, replacing any previous copy
// atomically: the new tree is staged next to it and swapped in, so a reader
// never sees a half-written skill and stale reference files disappear.
func Install(root string) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(root, Name)
	stage, err := os.MkdirTemp(root, "."+Name+"-install-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)

	src := FS()
	err = fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(stage, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		return "", fmt.Errorf("stage skill: %w", err)
	}

	old := dest + ".old"
	_ = os.RemoveAll(old)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, old); err != nil {
			return "", err
		}
	}
	if err := os.Rename(stage, dest); err != nil {
		_ = os.Rename(old, dest) // put the previous copy back
		return "", err
	}
	_ = os.RemoveAll(old)
	return dest, nil
}
