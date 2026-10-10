package project

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/gliese129/runq-lab/internal/rfs"
)

// SyncFile follows the rsync include-file mental model (the same file the
// hand-rolled rsync scripts used): file patterns select, directory patterns
// only permit traversal, and everything unlisted is excluded.
const SyncFile = ".rsync"

// SyncResult describes one source-to-destination mirror pass.
type SyncResult struct {
	SourceTarget      string `json:"source_target"`
	DestinationTarget string `json:"destination_target"`
	Copied            int    `json:"copied"`
	Deleted           int    `json:"deleted"`
}

type syncRule struct {
	exclude   bool
	directory bool
	prefix    string
	re        *regexp.Regexp
}

// syncRules is a positive allow-list with optional ! exclusions. As with
// `rsync --include-from=... --exclude='*'`, an included directory pattern
// (src/) only permits traversal; files need a file pattern (src/** or
// src/**.py). Exclusions always win; unlike rsync's first-match ordering, a
// later file pattern cannot re-include a path the user marked with !.
type syncRules struct {
	rules []syncRule
}

func parseSyncRules(data []byte) (*syncRules, error) {
	var out syncRules
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		exclude := false
		if strings.HasPrefix(line, `\!`) || strings.HasPrefix(line, `\#`) {
			line = line[1:]
		} else if strings.HasPrefix(line, "!") {
			exclude = true
			line = strings.TrimSpace(line[1:])
		}
		if line == "" {
			return nil, fmt.Errorf("%s:%d: empty pattern", SyncFile, i+1)
		}
		re, err := compileSyncPattern(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", SyncFile, i+1, err)
		}
		directory, prefix := syncPatternTraversal(line)
		out.rules = append(out.rules, syncRule{exclude: exclude, directory: directory, prefix: prefix, re: re})
	}
	return &out, nil
}

// syncPatternTraversal returns the literal directory prefix that can contain
// matches. It lets a whitelist such as src/ avoid walking datasets/ at all.
// An unanchored basename pattern (*.py) may match anywhere and returns "".
func syncPatternTraversal(pattern string) (directory bool, prefix string) {
	pattern = strings.ReplaceAll(pattern, `\`, "/")
	pattern = strings.TrimPrefix(pattern, "./")
	anchored := strings.HasPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	directory = strings.HasSuffix(pattern, "/")
	pattern = strings.TrimSuffix(pattern, "/")
	if !anchored && !strings.Contains(pattern, "/") {
		return directory, ""
	}
	if i := strings.IndexAny(pattern, "*?["); i >= 0 {
		pattern = pattern[:i]
	}
	if slash := strings.LastIndex(pattern, "/"); slash >= 0 {
		return directory, pattern[:slash+1]
	}
	return directory, pattern
}

func compileSyncPattern(pattern string) (*regexp.Regexp, error) {
	pattern = strings.ReplaceAll(pattern, `\`, "/")
	pattern = strings.TrimPrefix(pattern, "./")
	anchored := strings.HasPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	directory := strings.HasSuffix(pattern, "/")
	pattern = strings.TrimSuffix(pattern, "/")
	if pattern == "" || path.Clean(pattern) == ".." || strings.HasPrefix(path.Clean(pattern), "../") {
		return nil, fmt.Errorf("pattern must stay inside the project: %q", pattern)
	}

	var b strings.Builder
	if anchored || strings.Contains(pattern, "/") {
		b.WriteString("^")
	} else {
		b.WriteString(`(^|.*/)`)
	}
	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i += 2
				if i < len(pattern) && pattern[i] == '/' {
					i++
					b.WriteString(`(?:.*/)?`)
				} else {
					b.WriteString(`.*`)
				}
				continue
			}
			b.WriteString(`[^/]*`)
		case '?':
			b.WriteString(`[^/]`)
		case '[':
			// Character classes are intentionally unsupported rather than
			// accepted with subtly different escaping across filesystems.
			return nil, fmt.Errorf("character classes are not supported in pattern %q", pattern)
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
		i++
	}
	if directory {
		b.WriteString(`(?:/.*)?$`)
	} else {
		b.WriteString(`$`)
	}
	return regexp.Compile(b.String())
}

func (r *syncRules) selected(name string) bool {
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	included := false
	for _, rule := range r.rules {
		if !rule.re.MatchString(name) {
			continue
		}
		if rule.exclude {
			return false
		}
		if rule.directory {
			continue // traversal permission, not file selection
		}
		included = true
	}
	return included
}

func (r *syncRules) maySelectUnder(directory string) bool {
	dirPrefix := strings.TrimSuffix(directory, "/") + "/"
	for _, rule := range r.rules {
		if rule.exclude {
			continue
		}
		if rule.prefix == "" || strings.HasPrefix(rule.prefix, dirPrefix) || strings.HasPrefix(dirPrefix, rule.prefix) {
			return true
		}
	}
	return false
}

func (r *syncRules) excludedDirectory(name string) bool {
	for _, rule := range r.rules {
		if rule.exclude && rule.directory && rule.re.MatchString(name) {
			return true
		}
	}
	return false
}

type syncEntry struct {
	rel  string
	mode fs.FileMode
}

func collectSyncEntries(root string, fsys rfs.FS, rules *syncRules) ([]syncEntry, error) {
	var entries []syncEntry
	var walk func(string, string) error
	walk = func(abs, rel string) error {
		children, err := fsys.ReadDir(abs)
		if err != nil {
			return err
		}
		for _, child := range children {
			childRel := child.Name()
			if rel != "" {
				childRel = path.Join(rel, child.Name())
			}
			// Runtime artifacts and VCS internals are never deployment input.
			if child.IsDir() && (child.Name() == ".runq" || child.Name() == ".git") {
				continue
			}
			info, err := child.Info()
			if err != nil {
				return fmt.Errorf("stat %s: %w", childRel, err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				if rules.selected(childRel) || rules.maySelectUnder(childRel) {
					return fmt.Errorf("%s: symbolic links are not supported", childRel)
				}
				continue
			}
			if child.IsDir() {
				if rules.excludedDirectory(childRel) || !rules.maySelectUnder(childRel) {
					continue
				}
				if err := walk(path.Join(abs, child.Name()), childRel); err != nil {
					return err
				}
				continue
			}
			if info.Mode().IsRegular() && rules.selected(childRel) {
				entries = append(entries, syncEntry{rel: childRel, mode: info.Mode().Perm()})
			}
		}
		return nil
	}
	if err := walk(root, ""); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	return entries, nil
}

type syncRemover interface {
	Remove(name string) error
}

func ensureNoDestinationSymlink(root, rel string, fsys rfs.FS) error {
	parent := root
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		children, err := fsys.ReadDir(parent)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		found := false
		for _, child := range children {
			if child.Name() != part {
				continue
			}
			found = true
			info, err := child.Info()
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("destination %s traverses symbolic link %s", rel, path.Join(strings.TrimPrefix(parent, root), part))
			}
			break
		}
		if !found {
			return nil
		}
		parent = path.Join(parent, part)
	}
	return nil
}

// Mirror copies the .rsync allow-list from sourceRoot to destinationRoot
// and removes stale selected files at the destination. Everything outside the
// allow-list (and every ! exclusion) is left untouched.
func Mirror(sourceRoot string, source rfs.FS, destinationRoot string, destination rfs.FS) (copied, deleted int, err error) {
	data, err := source.ReadFile(path.Join(sourceRoot, SyncFile))
	if err != nil {
		return 0, 0, fmt.Errorf("read %s: %w", SyncFile, err)
	}
	rules, err := parseSyncRules(data)
	if err != nil {
		return 0, 0, err
	}
	sourceEntries, err := collectSyncEntries(sourceRoot, source, rules)
	if err != nil {
		return 0, 0, fmt.Errorf("scan sync source: %w", err)
	}
	if err := destination.MkdirAll(destinationRoot, 0o755); err != nil {
		return 0, 0, fmt.Errorf("create destination root: %w", err)
	}
	destinationEntries, err := collectSyncEntries(destinationRoot, destination, rules)
	if err != nil {
		return 0, 0, fmt.Errorf("scan sync destination: %w", err)
	}
	// The policy travels with the project even though it is not required to
	// list itself. This makes a declared destination usable as a one-shot
	// force source later without creating a second place to store the rules.
	if err := ensureNoDestinationSymlink(destinationRoot, SyncFile, destination); err != nil {
		return 0, 0, err
	}
	if err := destination.WriteFile(path.Join(destinationRoot, SyncFile), data, 0o644); err != nil {
		return 0, 0, fmt.Errorf("write %s: %w", SyncFile, err)
	}

	sourceSet := make(map[string]struct{}, len(sourceEntries))
	for _, entry := range sourceEntries {
		sourceSet[entry.rel] = struct{}{}
		if err := ensureNoDestinationSymlink(destinationRoot, entry.rel, destination); err != nil {
			return copied, deleted, err
		}
		data, err := source.ReadFile(path.Join(sourceRoot, entry.rel))
		if err != nil {
			return copied, deleted, fmt.Errorf("read %s: %w", entry.rel, err)
		}
		if err := destination.MkdirAll(path.Dir(path.Join(destinationRoot, entry.rel)), 0o755); err != nil {
			return copied, deleted, fmt.Errorf("create parent for %s: %w", entry.rel, err)
		}
		if err := destination.WriteFile(path.Join(destinationRoot, entry.rel), data, entry.mode); err != nil {
			return copied, deleted, fmt.Errorf("write %s: %w", entry.rel, err)
		}
		copied++
	}

	remover, ok := destination.(syncRemover)
	for _, entry := range destinationEntries {
		if _, exists := sourceSet[entry.rel]; exists {
			continue
		}
		if !ok {
			return copied, deleted, fmt.Errorf("destination filesystem cannot remove stale file %s", entry.rel)
		}
		if err := remover.Remove(path.Join(destinationRoot, entry.rel)); err != nil && !os.IsNotExist(err) {
			return copied, deleted, fmt.Errorf("delete stale %s: %w", entry.rel, err)
		}
		deleted++
	}
	return copied, deleted, nil
}
