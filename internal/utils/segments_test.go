package utils

import (
	"reflect"
	"strings"
	"testing"
)

func TestRenderSubmitSegments(t *testing.T) {
	const tmpl = "qsub -g {{param.group}} [[-ar {{param.ar_id}}]] [[--no-requeue]] -N {{name}} {{run_sh}}"
	base := map[string]string{"param.group": "tga-x", "name": "rq-1", "run_sh": "/r.sh"}
	with := func(kv ...string) map[string]string {
		vars := map[string]string{}
		for k, v := range base {
			vars[k] = v
		}
		for i := 0; i < len(kv); i += 2 {
			vars[kv[i]] = kv[i+1]
		}
		return vars
	}

	cases := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"both on", with("param.ar_id", "9245", "param.no-requeue", "true"),
			"qsub -g 'tga-x' -ar '9245' --no-requeue -N 'rq-1' '/r.sh'"},
		{"both missing", base,
			"qsub -g 'tga-x' -N 'rq-1' '/r.sh'"},
		{"empty value and falsy switch drop", with("param.ar_id", "", "param.no-requeue", "false"),
			"qsub -g 'tga-x' -N 'rq-1' '/r.sh'"},
		{"switch truthiness is case-insensitive", with("param.no-requeue", "Yes"),
			"qsub -g 'tga-x' --no-requeue -N 'rq-1' '/r.sh'"},
		{"value is shell-quoted", with("param.ar_id", "1; rm -rf ~"),
			"qsub -g 'tga-x' -ar '1; rm -rf ~' -N 'rq-1' '/r.sh'"},
	}
	for _, c := range cases {
		got, err := RenderSubmit(tmpl, c.vars)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestRenderSubmitDroppedSegmentSpacing(t *testing.T) {
	for tmpl, want := range map[string]string{
		"[[-ar {{param.x}}]] qsub a":                   "qsub a",
		"qsub a [[-ar {{param.x}}]]":                   "qsub a",
		"qsub [[-a {{param.x}}]] [[-b {{param.y}}]] a": "qsub a",
		"qsub -l h=1[[,g={{param.x}}]] a":              "qsub -l h=1 a",
	} {
		got, err := RenderSubmit(tmpl, map[string]string{})
		if err != nil {
			t.Fatalf("%q: %v", tmpl, err)
		}
		if got != want {
			t.Errorf("%q: got %q want %q", tmpl, got, want)
		}
	}
}

func TestRenderSubmitSegmentErrors(t *testing.T) {
	for tmpl, wantSub := range map[string]string{
		"qsub [[-l h_rt={{param.h_rt}},gpu={{param.g}}]] x": "use one {{param.*}} per segment: [[... {{param.h_rt}}]] [[... {{param.g}}]]",
		"qsub [[-ar {{param.x}} x":                          "without a closing",
		"qsub -ar x]] y":                                    "without a matching",
		"qsub [[a [[-ar {{param.x}}]] b]]":                  "cannot be nested",
		"qsub [[-N {{name}}]] x":                            "needs one {{param.NAME}}",
		"qsub [[-l exclusive]] x":                           "needs one {{param.NAME}}",
	} {
		_, err := RenderSubmit(tmpl, map[string]string{"name": "n", "param.x": "1"})
		if err == nil || !strings.Contains(err.Error(), wantSub) {
			t.Errorf("%q: got err %v, want it to contain %q", tmpl, err, wantSub)
		}
	}
}

func TestRenderSubmitParamOutsideSegmentStillRequired(t *testing.T) {
	_, err := RenderSubmit("qsub [[-ar {{param.ar_id}}]] -l h_rt={{param.ar_id}} x", map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "param.ar_id not found") {
		t.Fatalf("param referenced outside a segment must stay required, got %v", err)
	}
}

func TestSegmentSwitchParams(t *testing.T) {
	got := SegmentSwitchParams("qsub [[--no-requeue]] [[-notify]] [[-ar {{param.ar}}]] x")
	if want := []string{"no-requeue", "notify"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
