package project

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// `gpus_per_task: 0` is a legal CPU-only setting and must survive the wire
// round trip instead of being hidden by omitempty.
func TestDefaultsExplicitZeroSurvivesJSONRoundTrip(t *testing.T) {
	var cfg Config
	yamlSrc := strings.Join([]string{
		"project_name: p1",
		"working_dir: /w",
		"command_template: python train.py {{args}}",
		"defaults:",
		"  gpus_per_task: 0",
	}, "\n")
	if err := yaml.Unmarshal([]byte(yamlSrc), &cfg); err != nil {
		t.Fatalf("yaml: %v", err)
	}

	wire, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `"gpus_per_task":0`; !strings.Contains(string(wire), want) {
		t.Errorf("wire JSON dropped explicit zero — missing %s in %s", want, wire)
	}

	// The dashboard PUTs the same shape back — it must decode losslessly.
	var back Config
	if err := json.Unmarshal(wire, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Defaults.GPUsPerTask != 0 {
		t.Errorf("round trip mutated defaults: %+v", back.Defaults)
	}
}

// An OMITTED yaml key decodes to the same 0 the scheduler uses (unset ==
// 0 is the established semantic; submitplan consumes the zero value
// directly). The wire now reports that 0 explicitly instead of hiding it.
func TestDefaultsOmittedYAMLKeyIsExplicitZeroOnWire(t *testing.T) {
	var cfg Config
	yamlSrc := "project_name: p1\nworking_dir: /w\ncommand_template: x\n"
	if err := yaml.Unmarshal([]byte(yamlSrc), &cfg); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	wire, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(wire), `"gpus_per_task":0`) {
		t.Errorf("unset defaults must still serialize explicitly: %s", wire)
	}
}
