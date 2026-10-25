package job

import (
	"fmt"
	"github.com/gliese129/runq-lab/internal/consumer"

	"github.com/gliese129/runq-lab/internal/project"
	"gopkg.in/yaml.v3"
)

// JobConfig represents a parsed job.yaml file.
type JobConfig struct {
	Consumers       []consumer.Config  `yaml:"consumers,omitempty" json:"consumers,omitempty"`
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
	// Note is the pre-rc6 key for Name; NormalizeLegacy folds it in.
	Note        string         `yaml:"note,omitempty" json:"note,omitempty"`
	FixedParams map[string]any `yaml:"fixed_params,omitempty" json:"fixed_params,omitempty"` // params with fixed values (not swept)
	Sweep       []SweepBlock   `yaml:"sweep" json:"sweep"`
	Overrides   *Overrides     `yaml:"overrides,omitempty" json:"overrides,omitempty"`
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
