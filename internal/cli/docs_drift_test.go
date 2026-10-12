package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/skill"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Docs and the agent skill are how agents learn runq; every command line they
// show must run against THIS binary. For each `runq …` in code (fenced blocks
// and `inline spans`) the command must exist and not be hidden, and every
// flag must be accepted and not hidden — hidden things are compatibility
// shims, not what we teach. Lines that list removed commands are exempt.

var (
	runqInvocation = regexp.MustCompile(`(?:^|[^\w./-])runq((?:\s+[^\s|;&#` + "`" + `]+)*)`)
	inlineCodeSpan = regexp.MustCompile("`([^`]+)`")
)

type docSource struct {
	name string
	read func() ([]byte, error)
}

func docSources(t *testing.T) []docSource {
	t.Helper()
	root := filepath.Join("..", "..")
	var out []docSource
	add := func(rel string) {
		p := filepath.Join(root, rel)
		out = append(out, docSource{name: rel, read: func() ([]byte, error) { return os.ReadFile(p) }})
	}
	add("README.md")
	add(filepath.Join("sdk", "python", "README.md"))
	for _, pattern := range []string{"docs/*.md", "examples/*.md", "examples/*/*.md"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range matches {
			rel, _ := filepath.Rel(root, m)
			add(rel)
		}
	}
	_ = fs.WalkDir(skill.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			name := p
			out = append(out, docSource{name: "skill/" + name, read: func() ([]byte, error) { return fs.ReadFile(skill.FS(), name) }})
		}
		return nil
	})
	return out
}

// codeFragments returns the executable-looking parts of a markdown line.
func codeFragments(line string, inFence bool) []string {
	if inFence {
		if i := strings.Index(line, " #"); i >= 0 { // trailing shell/python comment = prose
			line = line[:i]
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			return nil
		}
		return []string{line}
	}
	var out []string
	for _, m := range inlineCodeSpan.FindAllStringSubmatch(line, -1) {
		out = append(out, m[1])
	}
	return out
}

func isExemptLine(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range []string{"removed", "unknown command", "do not try", "no longer", "replaced by", "→ `runq"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func lookupFlag(cmd *cobra.Command, name string) *pflag.Flag {
	if f := cmd.Flags().Lookup(name); f != nil {
		return f
	}
	return cmd.InheritedFlags().Lookup(name)
}

func lookupShorthand(cmd *cobra.Command, short string) *pflag.Flag {
	if f := cmd.Flags().ShorthandLookup(short); f != nil {
		return f
	}
	return cmd.InheritedFlags().ShorthandLookup(short)
}

func checkInvocation(t *testing.T, where, words string) {
	t.Helper()
	tokens := strings.Fields(words)
	var path []string
	for _, tok := range tokens {
		if strings.HasPrefix(tok, "-") || strings.ContainsAny(tok, "<>[]{}()=./'\"$*") {
			break
		}
		path = append(path, tok)
	}
	if len(path) == 0 {
		return // bare `runq` / `runq --help`
	}
	cmd, rest, err := rootCmd.Find(path)
	if err != nil || cmd == rootCmd {
		t.Errorf("%s: `runq %s` is not a command", where, path[0])
		return
	}
	if cmd.Hidden {
		t.Errorf("%s: `%s` is hidden — document the canonical command instead", where, cmd.CommandPath())
	}
	if len(rest) > 0 && cmd.HasSubCommands() && !cmd.Runnable() {
		t.Errorf("%s: `runq %s %s` is not a command", where, strings.TrimPrefix(cmd.CommandPath(), "runq "), rest[0])
		return
	}
	for _, tok := range tokens {
		switch {
		case strings.HasPrefix(tok, "--") && len(tok) > 2:
			name := strings.TrimPrefix(tok, "--")
			name, _, _ = strings.Cut(name, "=")
			name = strings.TrimRight(name, ",:)")
			f := lookupFlag(cmd, name)
			if f == nil {
				t.Errorf("%s: `%s` has no flag --%s", where, cmd.CommandPath(), name)
			} else if f.Hidden {
				t.Errorf("%s: `%s --%s` is a hidden compatibility flag — document the canonical one", where, cmd.CommandPath(), name)
			}
		case len(tok) == 2 && tok[0] == '-' && tok[1] != '-':
			if lookupShorthand(cmd, tok[1:]) == nil {
				t.Errorf("%s: `%s` has no flag %s", where, cmd.CommandPath(), tok)
			}
		}
	}
}

func TestDocsAndSkillShowOnlyRealCommandsAndFlags(t *testing.T) {
	for _, src := range docSources(t) {
		data, err := src.read()
		if err != nil {
			t.Fatalf("%s: %v", src.name, err)
		}
		inFence := false
		for n, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
				continue
			}
			if isExemptLine(line) {
				continue
			}
			for _, frag := range codeFragments(line, inFence) {
				for _, m := range runqInvocation.FindAllStringSubmatch(frag, -1) {
					checkInvocation(t, src.name+":"+strconv.Itoa(n+1), m[1])
				}
			}
		}
	}
}
