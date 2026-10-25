package cli

import (
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/project"
)

// Removed commands and flags answer with their replacement instead of
// "unknown command", and stay out of help.
func TestRetiredCommandsNameTheirReplacement(t *testing.T) {
	for path, want := range map[string]string{
		"task show tk1":           "runq show <tk…>",
		"job archive jb1":         "runq archive <jb…>",
		"project add -f x.yaml":   "runq project <project.yaml>",
		"results jb1":             "runq view --job <jb…>",
		"config set data_path=/x": "~/.runq/config.yaml",
		"target use tsubame":      "-t <target>",
		"version":                 "runq --version",
		"open jb1":                "runq dashboard",
		"consumer ls jb1":         "runq ps <jb…>",
	} {
		args := strings.Fields(path)
		cmd, rest, err := rootCmd.Find(args)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if !cmd.Hidden {
			t.Errorf("%s should be hidden", cmd.CommandPath())
		}
		err = cmd.RunE(cmd, rest)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want it to name %q", path, err, want)
		}
	}
}

func TestRetiredSubmitFlags(t *testing.T) {
	for _, flag := range []string{"dry", "project-file", "note", "watch", "project"} {
		f := submitCmd.Flags().Lookup(flag)
		if f == nil || !f.Hidden {
			t.Fatalf("--%s should stay parseable but hidden", flag)
		}
	}
	if err := submitCmd.Flags().Set("dry", "true"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = submitCmd.Flags().Set("dry", "false"); submitCmd.Flags().Lookup("dry").Changed = false }()
	if err := checkRetiredFlags(submitCmd); err == nil || !strings.Contains(err.Error(), "runq draft") {
		t.Fatalf("--dry should point at runq draft, got %v", err)
	}
}

func TestChangedProjectKeys(t *testing.T) {
	a := &project.Config{ProjectName: "p", WorkingDir: "/w", CmdTemplate: "python a.py", ID: "pj1", SourceSHA: "x"}
	b := &project.Config{ProjectName: "p", WorkingDir: "/w", CmdTemplate: "python b.py", Environment: map[string]string{"A": "1"}, SourceSHA: "y"}
	if got := strings.Join(changedProjectKeys(a, b), ","); got != "command,environment" {
		t.Fatalf("changed keys = %q (bookkeeping fields must not count)", got)
	}
}
