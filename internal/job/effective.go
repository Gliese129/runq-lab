package job

import (
	"encoding/json"
	"gopkg.in/yaml.v3"

	"github.com/gliese129/runq-lab/internal/project"
)

// Pointer fields retain absent versus explicitly empty overrides on both wires.
type PreflightOverride struct {
	Enabled  *bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Imports  *bool     `yaml:"imports,omitempty" json:"imports,omitempty"`
	Wandb    *bool     `yaml:"wandb,omitempty" json:"wandb,omitempty"`
	HF       *[]string `yaml:"hf,omitempty" json:"hf,omitempty"`
	ExtraRun *string   `yaml:"extra_run,omitempty" json:"extra_run,omitempty"`
}

// EffectiveProject returns an independent configuration for one submission.
func EffectiveProject(cfg JobConfig, source *project.Config) (*project.Config, error) {
	data, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	var out project.Config
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if cfg.Command != "" {
		out.CmdTemplate = cfg.Command
	}
	if p := cfg.Preflight; p != nil {
		if out.Preflight == nil {
			out.Preflight = &project.PreflightConfig{}
		}
		if p.Enabled != nil {
			out.Preflight.Enabled = p.Enabled
		}
		if p.Imports != nil {
			out.Preflight.Imports = p.Imports
		}
		if p.Wandb != nil {
			out.Preflight.Wandb = p.Wandb
		}
		if p.HF != nil {
			out.Preflight.HF = append([]string{}, (*p.HF)...)
		}
		if p.ExtraRun != nil {
			out.Preflight.ExtraRun = *p.ExtraRun
		}
	}
	if o := cfg.Overrides; o != nil {
		if o.GPUsPerTask != nil {
			out.Defaults.GPUsPerTask = *o.GPUsPerTask
		}
		if o.Timeout != nil {
			out.Defaults.Timeout = *o.Timeout
		}
		if out.Environment == nil {
			out.Environment = map[string]string{}
		}
		for k, v := range o.Env {
			out.Environment[k] = v
		}
	}
	return &out, nil
}

// A scalar false disables preflight; object fields override individually.
func (p *PreflightOverride) UnmarshalJSON(data []byte) error {
	if string(data) == "false" || string(data) == "true" {
		var enabled bool
		if err := json.Unmarshal(data, &enabled); err != nil {
			return err
		}
		p.Enabled = &enabled
		return nil
	}
	type plain PreflightOverride
	return json.Unmarshal(data, (*plain)(p))
}

func (p *PreflightOverride) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!bool" {
		var enabled bool
		if err := n.Decode(&enabled); err != nil {
			return err
		}
		p.Enabled = &enabled
		return nil
	}
	type plain PreflightOverride
	return n.Decode((*plain)(p))
}
