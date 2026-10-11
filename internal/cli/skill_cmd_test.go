package cli

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/skill"
)

// TestSkillMentionsOnlyRealCommands keeps the embedded agent skill in sync
// with the CLI: every `runq <cmd> [<sub>]` it tells an agent to run must
// resolve to a real command. Lines that list removed commands are exempt.
func TestSkillMentionsOnlyRealCommands(t *testing.T) {
	mention := regexp.MustCompile(`(?:^|[^\w.-])runq ([a-z][a-z-]+)(?: ([a-z][a-z-]+))?`)
	inlineCode := regexp.MustCompile("`([^`]+)`")
	err := fs.WalkDir(skill.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		data, err := fs.ReadFile(skill.FS(), p)
		if err != nil {
			return err
		}
		inFence := false
		for n, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
				continue
			}
			lower := strings.ToLower(line)
			if strings.Contains(lower, "removed") || strings.Contains(lower, "unknown command") ||
				strings.Contains(lower, "do not try") {
				continue
			}
			// Only code is executable advice: fenced blocks, or `inline spans`.
			code := []string{line}
			if inFence {
				if i := strings.Index(line, "#"); i >= 0 { // shell/python comment = prose
					code = []string{line[:i]}
				}
			} else {
				code = code[:0]
				for _, m := range inlineCode.FindAllStringSubmatch(line, -1) {
					code = append(code, m[1])
				}
			}
			for _, c := range code {
				for _, m := range mention.FindAllStringSubmatch(c, -1) {
					cmd, _, ferr := rootCmd.Find([]string{m[1]})
					if ferr != nil || cmd == rootCmd {
						t.Errorf("%s:%d mentions `runq %s`, which is not a command", p, n+1, m[1])
						continue
					}
					if m[2] != "" && cmd.HasSubCommands() {
						if sub, _, err := cmd.Find([]string{m[2]}); err != nil || sub == cmd {
							t.Errorf("%s:%d mentions `runq %s %s`, which is not a command", p, n+1, m[1], m[2])
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
