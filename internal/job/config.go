package job

import (
	"fmt"

	"github.com/gliese129/runq-lab/internal/project"
	"gopkg.in/yaml.v3"
)

// JobConfig represents a parsed job.yaml file.
type JobConfig struct {
	// Consumers is retired: parsed so old files still load, then warned
	// about and dropped by NormalizeLegacy (scripts read jobs with runq.job()).
	Consumers       any                `yaml:"consumers,omitempty" json:"-"`
	Command         string             `yaml:"command,omitempty" json:"command,omitempty"`
	Target          string             `yaml:"target,omitempty" json:"target,omitempty"`
	Preflight       *PreflightOverride `yaml:"preflight,omitempty" json:"preflight,omitempty"`
	TargetSource    string             `yaml:"-" json:"target_source,omitempty"`
	EffectiveConfig *project.Config    `yaml:"-" json:"effective_config,omitempty"`

	Project     string `yaml:"project" json:"project"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Name is the job's renamable display name (the id never changes):
	// what this experiment is, readable at a glance. Free text.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// Section names an existing section of the project; empty = the
	// project's rules decide (no match = unsorted).
	Section string `yaml:"section,omitempty" json:"section,omitempty"`
	// SectionID is the resolved section (set by the service, "" = unsorted);
	// SourcePath is the job file as passed to submit (for path rules).
	SectionID  string `yaml:"-" json:"section_id,omitempty"`
	SourcePath string `yaml:"-" json:"source_path,omitempty"`
	// Note is the pre-rc6 key for Name; NormalizeLegacy folds it in.
	Note        string         `yaml:"note,omitempty" json:"note,omitempty"`
	FixedParams map[string]any `yaml:"fixed_params,omitempty" json:"fixed_params,omitempty"` // params with fixed values (not swept)
	Sweep       []SweepBlock   `yaml:"sweep" json:"sweep"`
	// Tasks lists the cells explicitly (the alternative to Sweep), written
	// cells: in job.yaml: each row is fixed_params overridden by the row's
	// own params. LegacyTasks is the older tasks: key, folded in on parse.
	Tasks          []TaskRow `yaml:"cells,omitempty" json:"tasks,omitempty"`
	LegacyTasks    []TaskRow `yaml:"tasks,omitempty" json:"-"`
	legacyTasksKey bool
	// Baseline names the reference run (default: the first row). Run names
	// of sweep rows and the plan table describe rows relative to it.
	Baseline string `yaml:"baseline,omitempty" json:"baseline,omitempty"`
	// DraftID is set by the service when a draft is submitted (provenance).
	DraftID string `yaml:"-" json:"draft_id,omitempty"`
	// Draft and Rev head a draft's editable YAML: `runq draft edit` checks
	// them; as input to `runq draft` they are ignored.
	Draft string `yaml:"draft,omitempty" json:"-"`
	Rev   int    `yaml:"rev,omitempty" json:"-"`
	// Into appends this plan to an existing job (jb…) instead of creating
	// one: a row whose run name the job has is a new version of that cell
	// when its selected version failed or was killed, and is skipped when it
	// succeeded or still runs. Rerun names succeeded cells to run again.
	Into  string   `yaml:"into,omitempty" json:"into,omitempty"`
	Rerun []string `yaml:"rerun,omitempty" json:"rerun,omitempty"`
	// RetiredRerunFailed is the old rerun_failed: failed cells now get a new
	// version by default, so setting it is an error that says so.
	RetiredRerunFailed bool `yaml:"rerun_failed,omitempty" json:"rerun_failed,omitempty"`
	// FromJob is written by `runq export` for provenance; ignored as input.
	FromJob   string     `yaml:"from_job,omitempty" json:"from_job,omitempty"`
	Overrides *Overrides `yaml:"overrides,omitempty" json:"overrides,omitempty"`
}

// SweepBlock is one block inside the sweep array.
// Blocks expand independently, then cross-product across blocks.
type SweepBlock struct {
	Method     string                   `yaml:"method" json:"method"` // "grid" or "list"
	Parameters map[string]ParameterSpec `yaml:"parameters" json:"parameters"`
}

// ParameterSpec supports three input forms via custom UnmarshalYAML:
//
//	shorthand:  lr: [0.001, 0.01, 0.1]          → Values = [0.001, 0.01, 0.1]
//	scalar:     lr: 0.01                         → Values = [0.01]
//	full:       lr: { values: [0.001, 0.01, 0.1] } → Values = [0.001, 0.01, 0.1]
type ParameterSpec struct {
	Values []any `yaml:"values" json:"values"`
	// Reserved for future sweep methods (random, bayesian):
	// Distribution string  `yaml:"distribution,omitempty"`
	// Min          float64 `yaml:"min,omitempty"`
	// Max          float64 `yaml:"max,omitempty"`
	// Count        int     `yaml:"count,omitempty"`
}

// Overrides are job-level overrides for project defaults.
type Overrides struct {
	GPUsPerTask *int              `yaml:"gpus_per_task,omitempty" json:"gpus_per_task,omitempty"`
	Timeout     *string           `yaml:"timeout,omitempty" json:"timeout,omitempty"` // human duration, e.g. "3h"
	Env         map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

func (p *ParameterSpec) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.SequenceNode:
		return value.Decode(&p.Values)
	case yaml.ScalarNode:
		var result any
		if err := value.Decode(&result); err != nil {
			return err
		}
		p.Values = []any{result}
		return nil
	case yaml.MappingNode:
		// Shadow type breaks recursion: raw has the same fields but no
		// UnmarshalYAML method, so yaml.v3 uses default struct decoding.
		type raw ParameterSpec
		var r raw
		if err := value.Decode(&r); err != nil {
			return err
		}
		*p = ParameterSpec(r)
		return nil
	default:
		return fmt.Errorf("unsupported YAML node kind %d for parameter spec", value.Kind)
	}
}

// CheckAppendFields rejects the retired rerun_failed and a rerun list
// without into.
func (c *JobConfig) CheckAppendFields() error {
	if c.RetiredRerunFailed {
		return fmt.Errorf("rerun_failed (--rerun-failed) is gone: with into: a failed or killed cell gets a new version by default; rerun: [cell] (--rerun <cell>) runs a succeeded one again")
	}
	if len(c.Rerun) > 0 && c.Into == "" {
		return fmt.Errorf("rerun needs into: <job>")
	}
	return nil
}

// MergeNames appends the names in more that names lacks.
func MergeNames(names, more []string) []string {
	have := make(map[string]bool, len(names))
	for _, n := range names {
		have[n] = true
	}
	for _, n := range more {
		if !have[n] {
			names = append(names, n)
			have[n] = true
		}
	}
	return names
}

// UnmarshalYAML reads cells: (and the older tasks:) into Tasks.
func (c *JobConfig) UnmarshalYAML(n *yaml.Node) error {
	type plain JobConfig
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*c = JobConfig(p)
	if len(c.LegacyTasks) > 0 {
		if len(c.Tasks) > 0 {
			return fmt.Errorf("cells: and tasks: are the same list; keep cells:")
		}
		c.Tasks, c.LegacyTasks, c.legacyTasksKey = c.LegacyTasks, nil, true
	}
	return nil
}
