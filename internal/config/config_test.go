package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_MissingFile(t *testing.T) {
	t.Setenv("RUNQ_DATA_DIR", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DataPath != "" {
		t.Errorf("DataPath = %q, want empty", cfg.DataPath)
	}
	if got := cfg.ResolveTargets(); len(got) != 0 {
		t.Errorf("ResolveTargets() = %+v, want empty", got)
	}
	if got := cfg.ResolveDefaultTarget(); got != "" {
		t.Errorf("ResolveDefaultTarget() = %q, want empty", got)
	}
}

func TestLoad_WithDataPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RUNQ_DATA_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("data_path: /scratch/runq\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DataPath != "/scratch/runq" {
		t.Errorf("DataPath = %q, want /scratch/runq", cfg.DataPath)
	}
}

func TestLoad_StaleModeKeyIgnored(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RUNQ_DATA_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("mode: hpc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// mode is dead (D9): a leftover key in an old file parses as an
	// ignored unknown — never an error, never behavior.
	if got := cfg.ResolveDefaultTarget(); got != "" {
		t.Errorf("default target = %q, want unconfigured", got)
	}
}

func TestResolveDefaultTarget(t *testing.T) {
	tests := []struct {
		name string
		cfg  GlobalConfig
		want string
	}{
		{name: "empty", cfg: GlobalConfig{}, want: ""},
		{name: "stale default without targets", cfg: GlobalConfig{DefaultTarget: "deleted"}, want: ""},
		{name: "first configured target", cfg: GlobalConfig{Targets: []TargetConfig{{Name: "a"}, {Name: "b"}}}, want: "a"},
		{name: "explicit configured target", cfg: GlobalConfig{DefaultTarget: "b", Targets: []TargetConfig{{Name: "a"}, {Name: "b"}}}, want: "b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ResolveDefaultTarget(); got != tt.want {
				t.Fatalf("ResolveDefaultTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoad_ArbitraryModeValueIgnored(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RUNQ_DATA_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("mode: cluster\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err != nil {
		t.Fatalf("stale mode key must be ignored, got error: %v", err)
	}
}

func TestSetKeyPreservesHPCSection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RUNQ_DATA_DIR", dir)
	src := []byte("data_path: /old\ntargets:\n  - name: my-lab\n    scheduler: slurm\nhpc:\n  submit_template: sbatch {{run_sh}}\n  submit_id_regex: \"([0-9]+)\"\n  kill_template: scancel {{ext_id}}\n")
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), src, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetKey("default_target", "my-lab"); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultTarget != "my-lab" {
		t.Errorf("DefaultTarget = %q, want my-lab", cfg.DefaultTarget)
	}

	buf, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf), "hpc:") || !strings.Contains(string(buf), "submit_template") {
		t.Fatalf("hpc section was not preserved:\n%s", string(buf))
	}
}

func TestSetKeyRejectsUnconfiguredDefaultTarget(t *testing.T) {
	t.Setenv("RUNQ_DATA_DIR", t.TempDir())
	if err := SetKey("default_target", "missing"); err == nil || !strings.Contains(err.Error(), "add the target first") {
		t.Fatalf("SetKey error = %v, want actionable missing-target error", err)
	}
}
