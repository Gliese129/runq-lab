package project

import (
	"encoding/json"
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
)

func TestRemovedMetadataIgnored(t *testing.T) {
	for _, format := range []string{"yaml", "json"} {
		var cfg Config
		var err error
		if format == "yaml" {
			err = yaml.Unmarshal([]byte("project_name: p\ncommand_template: echo hi\nparams:\n  - {name: lr, type: float, default: 42, min: 0, max: 1, strict: false, include: true, glob: '*', choices: ['0.1']}"), &cfg)
		} else {
			err = json.Unmarshal([]byte(`{"project_name":"p","command_template":"echo hi","params":[{"name":"lr","type":"float","default":42,"min":0,"max":1,"strict":false,"include":true,"glob":"*","choices":["0.1"]}]}`), &cfg)
		}
		if err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"default", "min", "max", "strict", "include", "glob", "command_template"} {
			if strings.Contains(string(wire), `"`+key+`":`) {
				t.Fatalf("%s survived: %s", key, wire)
			}
		}
		if cfg.CmdTemplate != "echo hi" || len(cfg.Params[0].Choices) != 1 {
			t.Fatalf("submission fields lost: %+v", cfg)
		}
	}
}

func TestCommandAliasConflict(t *testing.T) {
	for _, src := range []string{`{"command":"echo a","command_template":"echo b"}`, `{"command":"","command_template":""}`} {
		var cfg Config
		if json.Unmarshal([]byte(src), &cfg) == nil {
			t.Fatal("ambiguous JSON accepted")
		}
	}
	var cfg Config
	if yaml.Unmarshal([]byte("command: echo a\ncommand_template: echo b"), &cfg) == nil {
		t.Fatal("ambiguous YAML accepted")
	}
}
