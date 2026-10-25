package utils

import (
	"fmt"
	"regexp"
	"strings"
)

// Optional segments in submit_template: [[...]] is kept or dropped as a
// whole, so a scheduler flag can depend on a param without every job
// having to set it (e.g. an advance reservation).
//
//   - [[-ar {{param.ar_id}}]] — value segment: exactly one {{param.*}};
//     dropped when that param is missing or empty.
//   - [[--no-requeue]] — switch segment: a bare flag bound to the param of
//     the same name (leading dashes stripped, inner dashes kept); emitted
//     only when the param is truthy (see Truthy).
//
// No nesting, no escaping. A dropped segment also takes one adjacent space
// with it so the rendered command has no double spaces.

var (
	segmentParamRe = regexp.MustCompile(`\{\{\s*param\.([\w\-]+)\s*}}`)
	segmentFlagRe  = regexp.MustCompile(`^\s*(-{1,2}([A-Za-z0-9_][\w\-]*))\s*$`)
)

type templateSegment struct {
	start, end int    // byte span of "[[...]]" in the template
	inner      string // text between the brackets
	param      string // value segment: the one {{param.*}} name
	flag       string // switch segment: the literal flag, e.g. "--no-requeue"
	flagParam  string // switch segment: its param name, e.g. "no-requeue"
}

func parseSegments(tmpl string) ([]templateSegment, error) {
	var segs []templateSegment
	for i := 0; i < len(tmpl); {
		open := strings.Index(tmpl[i:], "[[")
		closing := strings.Index(tmpl[i:], "]]")
		if closing >= 0 && (open < 0 || closing < open) {
			return nil, fmt.Errorf("optional segment: \"]]\" without a matching \"[[\"")
		}
		if open < 0 {
			break
		}
		start := i + open
		rest := tmpl[start+2:]
		c := strings.Index(rest, "]]")
		if c < 0 {
			return nil, fmt.Errorf("optional segment: \"[[\" without a closing \"]]\"")
		}
		inner := rest[:c]
		if strings.Contains(inner, "[[") {
			return nil, fmt.Errorf("optional segment [[%s]]: segments cannot be nested", inner)
		}
		seg := templateSegment{start: start, end: start + 2 + c + 2, inner: inner}
		refs := segmentParamRe.FindAllStringSubmatch(inner, -1)
		switch {
		case len(refs) == 1:
			seg.param = refs[0][1]
		case len(refs) > 1:
			var split []string
			for _, r := range refs {
				split = append(split, "[[... {{param."+r[1]+"}}]]")
			}
			return nil, fmt.Errorf("optional segment [[%s]] references %d params; use one {{param.*}} per segment: %s",
				inner, len(refs), strings.Join(split, " "))
		default:
			m := segmentFlagRe.FindStringSubmatch(inner)
			if m == nil {
				return nil, fmt.Errorf("optional segment [[%s]] needs one {{param.NAME}} (e.g. [[-ar {{param.ar_id}}]]) or a bare switch (e.g. [[--no-requeue]])", inner)
			}
			seg.flag, seg.flagParam = m[1], m[2]
		}
		segs = append(segs, seg)
		i = seg.end
	}
	return segs, nil
}

// SegmentSwitchParams returns the param names bound by switch segments
// ([[--flag]]) in tmpl. Malformed templates yield nil; rendering reports
// the error.
func SegmentSwitchParams(tmpl string) []string {
	segs, err := parseSegments(tmpl)
	if err != nil {
		return nil
	}
	var names []string
	for _, s := range segs {
		if s.flagParam != "" {
			names = append(names, s.flagParam)
		}
	}
	return names
}

// RenderSubmit is Render with optional [[...]] segments resolved first.
// Params are looked up as vars["param.NAME"].
func RenderSubmit(tmpl string, vars map[string]string) (string, error) {
	segs, err := parseSegments(tmpl)
	if err != nil {
		return "", err
	}
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' }
	var out []byte
	last := 0
	for _, s := range segs {
		out = append(out, tmpl[last:s.start]...)
		last = s.end
		switch {
		case s.flag != "" && Truthy(vars["param."+s.flagParam]):
			out = append(out, s.flag...)
			continue
		case s.param != "" && vars["param."+s.param] != "":
			out = append(out, s.inner...)
			continue
		}
		// Dropped: swallow one adjacent space.
		prevSpace := len(out) > 0 && isSpace(out[len(out)-1])
		nextSpace := last < len(tmpl) && isSpace(tmpl[last])
		switch {
		case prevSpace && (nextSpace || last == len(tmpl)):
			out = out[:len(out)-1]
		case len(out) == 0 && nextSpace:
			last++
		}
	}
	out = append(out, tmpl[last:]...)
	return Render(string(out), vars)
}

// Truthy reports whether a switch-style value means "on". Anything else —
// false/0/no/None/empty — means off.
func Truthy(s string) bool {
	switch strings.ToLower(s) {
	case "true", "1", "yes", "y", "on":
		return true
	}
	return false
}
