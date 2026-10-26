package project

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/gliese129/runq-lab/internal/rfs"

	"gopkg.in/yaml.v3"
)

const yamlHeader = `# runq project configuration
# Edit as needed, then re-register: runq project project.yaml
#
`

// WriteYAML writes the Config as a project.yaml file into the WorkingDir
// ON THE PROJECT'S OWN FILESYSTEM (fsys; nil = local). If the file already
// exists there, it is left untouched (user may have comments or custom
// formatting). Use OverwriteYAML to force-write.
//
// No pre-flight stat of WorkingDir (RQ-65): registration is registration,
// not a health check — and the only machine entitled to answer "does this
// dir exist" is the project's own target. A write to a missing dir fails
// loudly with the target's error, which is the honest signal.
func (c *Config) WriteYAML(fsys rfs.FS) error {
	if c.WorkingDir == "" {
		return fmt.Errorf("working_dir is required")
	}
	if fsys == nil {
		fsys = rfs.NewLocalFS()
	}
	yamlPath := path.Join(c.WorkingDir, "project.yaml")
	if _, err := fsys.Stat(yamlPath); err == nil {
		return nil // already exists, don't clobber
	}
	return c.OverwriteYAML(fsys)
}

// OverwriteYAML writes the Config as project.yaml on the project's own
// filesystem, replacing any existing file.
func (c *Config) OverwriteYAML(fsys rfs.FS) error {
	if c.WorkingDir == "" {
		return fmt.Errorf("working_dir is required")
	}
	if fsys == nil {
		fsys = rfs.NewLocalFS()
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal project config: %w", err)
	}
	content := yamlHeader + string(data)
	// Scaffold the preflight contract when the config has none: the block
	// spells out the defaults WITH comments, so the contract is
	// discoverable in the one file users actually read. Semantics are
	// identical with or without the block — this only makes them visible.
	if c.Preflight == nil {
		content += c.preflightScaffold()
	}
	return fsys.WriteFile(path.Join(c.WorkingDir, "project.yaml"), []byte(content), 0o644)
}

// hfParamNameRegex spots params whose values look like they carry HF
// repo ids — used only to pre-fill the scaffold suggestion.
var hfParamNameRegex = regexp.MustCompile(`(?i)^(hf_|model|.*model_name|.*model$|dataset|.*dataset$)`)

// preflightScaffold renders the commented preflight block appended to a
// freshly written project.yaml. Params with model/dataset-ish names are
// suggested as "{{param.X}}" entries (quoted — a bare {{ is not valid
// YAML); the user removes wrong guesses rather than authoring from zero.
func (c *Config) preflightScaffold() string {
	var hfLines string
	for _, p := range c.Params {
		if hfParamNameRegex.MatchString(p.Name) {
			hfLines += fmt.Sprintf("    - \"{{param.%s}}\"  # suggested from param names — remove if this is not a HuggingFace repo id\n", p.Name)
		}
	}
	hfBlock := "  hf: []              # HuggingFace repos that must be in the HF cache; \"{{param.NAME}}\" checks every sweep value\n"
	if hfLines != "" {
		hfBlock = "  hf:                 # HuggingFace repos that must be in the HF cache; \"{{param.NAME}}\" checks every sweep value\n" + hfLines
	}
	return `
# Preflight runs on every runq draft and runq submit and only warns about
# deterministic problems (entry script, interpreter, env activation in a
# non-interactive shell, data dir, HF cache, run.sh syntax). To know the
# job runs, submit a smoke job.
preflight:
` + hfBlock
}

// Config represents a parsed project.yaml file.
type Config struct {
	// ID is the immutable "pj…" id, owned by the registry (never read from
	// project.yaml). ProjectName is the renamable label.
	ID          string `yaml:"-" json:"id,omitempty"`
	ProjectName string `yaml:"project_name" json:"project_name"`
	// SourcePath / SourceSHA record the file this registration came from
	// (`runq project <file>`), so a later draft can say the file changed.
	// runq never reads or writes that file on its own.
	SourcePath string `yaml:"-" json:"source_path,omitempty"`
	SourceSHA  string `yaml:"-" json:"source_sha,omitempty"`
	// Target is the project's home: the compute target whose filesystem
	// owns WorkingDir and every other path in this config (RQ-65). All
	// path semantics — yaml sync, preflight, workspace layout — resolve
	// on that machine, never on whichever machine happens to run the
	// daemon. New registrations require an explicit target.
	Target string `yaml:"target,omitempty" json:"target,omitempty"`
	// Targets is the explicit set of compute targets this project may be
	// submitted to. Target above is the single source of truth for the
	// project tree; before a submit to any other member, runq mirrors the
	// paths selected by <working_dir>/.rsync from Target to that member.
	// Empty preserves the legacy single-target contract (Target only).
	Targets     []string          `yaml:"targets,omitempty" json:"targets,omitempty"`
	WorkingDir  string            `yaml:"working_dir" json:"working_dir"`
	CmdTemplate string            `yaml:"command,omitempty" json:"command,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`
	// SetupCommand optionally runs ONCE per job submission, on the node where
	// runq runs (login node in HPC mode), after the plan compiles and before
	// anything is persisted or submitted — a failure aborts with zero residue.
	// Typical use: model pre-download. {{placeholder}}s may reference
	// fixed_params only (it runs once; swept params are ambiguous → error).
	// Runs synchronously: long setups are CLI-submission territory.
	SetupCommand string `yaml:"setup_command,omitempty" json:"setup_command,omitempty"`
	// JobName is a template for the per-task scheduler job name, exposed to
	// submit_template as {{name}}. Vocabulary: params + {{project}}
	// {{job_id}} {{task_id}}. job.yaml `name:` overrides per submission.
	// Always sanitized (scheduler-safe charset, never digit-first).
	// Empty → "rq-{{task_id}}".
	JobName string `yaml:"job_name,omitempty" json:"job_name,omitempty"`
	// EnvFile is an ambient env file sourced by the shell AT TASK START —
	// runq never parses or stores its values (secrets stay out of the DB,
	// run.sh exports, and every UI; rotation applies on the next task).
	// Precedence: .env < explicit env (environment / overrides.env).
	// nil → auto: use <working_dir>/.env when present. "" → disabled.
	// other → path (relative to working_dir), missing file = submit error.
	EnvFile   *string         `yaml:"env_file,omitempty" json:"env_file,omitempty"`
	Defaults  Defaults        `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	PythonEnv PythonEnvConfig `yaml:"python_env,omitempty" json:"python_env,omitempty"`
	Wandb     *WandbConfig    `yaml:"wandb,omitempty" json:"wandb,omitempty"`
	Params    []ParamDef      `yaml:"params,omitempty" json:"params,omitempty"`
	// Preflight declares what the (warning-only) submit-time checks look
	// for beyond what the command itself names: the HF repos to find cached.
	Preflight *PreflightConfig `yaml:"preflight,omitempty" json:"preflight,omitempty"`
}

// PreflightConfig is project.yaml's preflight block. Preflight always
// runs and only warns; the block only adds what to look for.
type PreflightConfig struct {
	// HF lists HuggingFace repos that must be in the target's HF cache.
	// "{{param.NAME}}" entries expand against the sweep — EVERY value is
	// checked. A missing repo warns with its pre-download command.
	HF []string `yaml:"hf,omitempty" json:"hf,omitempty"`

	// Retired (warned and ignored, see NormalizeLegacy): preflight cannot
	// be switched off, and imports / extra_run are a smoke job's job now.
	Enabled  *bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Imports  *bool   `yaml:"imports,omitempty" json:"imports,omitempty"`
	ExtraRun *string `yaml:"extra_run,omitempty" json:"extra_run,omitempty"`
	Wandb    *bool   `yaml:"wandb,omitempty" json:"wandb,omitempty"`
}

// HFRepos returns the declared repo list (nil-safe).
func (p *PreflightConfig) HFRepos() []string {
	if p == nil {
		return nil
	}
	return p.HF
}

// ParamDef declares submission semantics. Defaults belong in research code.
type ParamDef struct {
	Name    string   `yaml:"name" json:"name"`
	Type    string   `yaml:"type,omitempty" json:"type,omitempty"`
	Choices []string `yaml:"choices,omitempty" json:"choices,omitempty"`
	Scope   string   `yaml:"scope,omitempty" json:"scope,omitempty"`
	Style   string   `yaml:"style,omitempty" json:"style,omitempty"`
}

// Legacy command_template is read-only; simultaneous keys are ambiguous.
func (c *Config) UnmarshalYAML(n *yaml.Node) error {
	type plain Config
	var legacy *yaml.Node
	hasCommand := false
	for i := 0; i+1 < len(n.Content); i += 2 {
		switch n.Content[i].Value {
		case "project":
			if err := n.Content[i+1].Decode(&c.ProjectName); err != nil {
				return err
			}
		case "command":
			hasCommand = true
		case "command_template":
			legacy = n.Content[i+1]
		}
	}
	if legacy != nil && hasCommand {
		return fmt.Errorf("use only command, not both command and command_template")
	}
	if err := n.Decode((*plain)(c)); err != nil {
		return err
	}
	if legacy != nil {
		return legacy.Decode(&c.CmdTemplate)
	}
	return nil
}

func (c *Config) UnmarshalJSON(data []byte) error {
	type plain Config
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return err
	}
	legacy, old := keys["command_template"]
	if _, current := keys["command"]; current && old {
		return fmt.Errorf("use only command, not both command and command_template")
	}
	if err := json.Unmarshal(data, (*plain)(c)); err != nil {
		return err
	}
	if old {
		return json.Unmarshal(legacy, &c.CmdTemplate)
	}
	return nil
}

type WandbConfig struct {
	Project string   `yaml:"project" json:"project"`
	Entity  string   `yaml:"entity,omitempty" json:"entity,omitempty"`
	Tags    []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	Mode    string   `yaml:"mode,omitempty" json:"mode,omitempty"` // "online" / "offline" / "disabled"
}

// Defaults are project-level defaults that can be overridden per-job.
//
// The int fields carry MEANINGFUL zeros (gpus_per_task 0 = CPU-only),
// and the scheduler already treats an unset
// field as 0 — so the JSON tags must NOT use omitempty: it silently
// dropped a legal explicit 0 from the wire and the dashboard rendered its
// form default (1 GPU) instead, then wrote that back (RQ-69 review).
// YAML keeps omitempty for hand-written project.yaml friendliness; an
// omitted key decodes to the same 0 the scheduler uses, so the file and
// wire semantics agree.
type Defaults struct {
	GPUsPerTask int    `yaml:"gpus_per_task,omitempty" json:"gpus_per_task"`
	Timeout     string `yaml:"timeout,omitempty" json:"timeout,omitempty"` // human duration, e.g. "3h", "1d"
}

// PythonEnvConfig specifies which Python environment to activate before running tasks.
type PythonEnvConfig struct {
	Type string `yaml:"type,omitempty" json:"type,omitempty"` // "venv", "conda", "uv", "system", "" (auto)
	Path string `yaml:"path,omitempty" json:"path,omitempty"` // venv: relative path to venv dir (e.g. ".venv")
	Name string `yaml:"name,omitempty" json:"name,omitempty"` // conda: environment name
}

// NormalizeLegacy applies the rc6 rule for retired project.yaml keys:
// dropping one that changes nothing warns; one whose removal would change
// what runs is an error naming the replacement.
//
//   - env_file: .env → dropped with a warning (runq always reads
//     <working_dir>/.env); any other value (including "") → error.
//   - preflight.wandb → dropped with a warning (no credential check).
//   - preflight.enabled / imports / extra_run → dropped with a warning:
//     preflight always runs and only warns; a smoke job answers "does it
//     run".
func NormalizeLegacy(c *Config) ([]string, error) {
	var warns []string
	if p := c.Preflight; p != nil {
		if p.Wandb != nil {
			warns = append(warns, `"preflight.wandb" is no longer checked; remove it`)
			p.Wandb = nil
		}
		if p.Enabled != nil {
			warns = append(warns, `"preflight.enabled" is no longer used (preflight always runs and only warns); remove it`)
			p.Enabled = nil
		}
		if p.Imports != nil {
			warns = append(warns, `"preflight.imports" is no longer checked (to know the imports resolve, submit a smoke job); remove it`)
			p.Imports = nil
		}
		if p.ExtraRun != nil {
			warns = append(warns, `"preflight.extra_run" is no longer run (put the check in a smoke job); remove it`)
			p.ExtraRun = nil
		}
	}
	if c.EnvFile != nil {
		switch strings.TrimSpace(*c.EnvFile) {
		case ".env", "./.env":
			warns = append(warns, `"env_file" is no longer used (runq always reads <working_dir>/.env); remove it`)
			c.EnvFile = nil
		default:
			return nil, fmt.Errorf(`"env_file" is no longer supported; runq always reads <working_dir>/.env — move the file or its variables there, then remove env_file`)
		}
	}
	return warns, nil
}
