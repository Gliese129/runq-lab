package job

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gliese129/runq-lab/internal/utils"
)

// placeholderRe matches {{name}} style placeholders in command templates.
var placeholderRe = regexp.MustCompile(`\{\{(\w+)}}`)

// Render takes a command template and TaskParams, returns the final command string.
//
// Two-pass replacement:
//  1. Replace named placeholders {{key}} (key != "args") with param values.
//     A key may appear multiple times in the template (e.g. used in two flags).
//  2. Replace {{args}} with remaining unconsumed params as --key=value pairs (sorted).
//
// Errors:
//   - Named placeholder references a key not in params.
//
// Parameters without command placeholders remain available through ctx.params.
func Render(template string, params TaskParams) (string, error) {
	return RenderExcluding(template, params, nil)
}

// RenderExcluding is Render with a set of params treated as ALREADY consumed
// elsewhere (e.g. scheduler knobs like h_rt referenced by the HPC
// submit_template as {{param.h_rt}}). Excluded params neither trigger the
// unconsumed-param error nor leak into {{args}} — a walltime is for the
// scheduler, not the training script.
func RenderExcluding(template string, params TaskParams, exclude map[string]bool) (string, error) {
	return RenderWithFlags(template, params, exclude, nil)
}

// TruthyFlag reports whether a flag-style param value means "the switch
// is on". Anything else — false/0/no/None/empty — means off.
func TruthyFlag(v any) bool { return utils.Truthy(FormatValue(v)) }

// RenderWithFlags is RenderExcluding plus store_true semantics:
// `flagParams` names params declared `style: flag` (argparse store_true
// switches). In {{args}}, a truthy flag renders as a bare `--name` and a
// falsy one is OMITTED entirely — `--name=false` is not "off" to
// argparse, it is the switch firing (feedback group 2: `-sample=false`
// silently enabled sampling). Explicit {{name}} placeholders are
// untouched: referencing a flag by name means the template author wants
// the raw value.
func RenderWithFlags(template string, params TaskParams, exclude, flagParams map[string]bool) (string, error) {
	consumed := make(map[string]bool)
	for name := range exclude {
		consumed[name] = true
	}

	// ── Pass 1: named placeholders ──
	var errMissing []string
	result := placeholderRe.ReplaceAllStringFunc(template, func(match string) string {
		key := match[2 : len(match)-2] // strip {{ and }}

		if key == "args" {
			return match // leave for pass 2
		}

		val, ok := params[key]
		if !ok {
			errMissing = append(errMissing, key)
			return ""
		}
		consumed[key] = true
		return formatValue(val)
	})

	if len(errMissing) > 0 {
		return "", fmt.Errorf(
			"template references unknown parameter(s): %s\nAvailable: %s",
			strings.Join(errMissing, ", "),
			strings.Join(sortedKeys(params), ", "),
		)
	}

	// ── Pass 2: {{args}} ──
	unconsumed := make([]string, 0)
	for _, key := range sortedKeys(params) {
		if !consumed[key] {
			unconsumed = append(unconsumed, key)
		}
	}

	parts := make([]string, 0, len(unconsumed))
	for _, key := range unconsumed {
		if flagParams[key] {
			if TruthyFlag(params[key]) {
				parts = append(parts, "--"+key)
			}
			continue // falsy switch: absence IS the value
		}
		parts = append(parts, fmt.Sprintf("--%s=%s", key, formatValue(params[key])))
	}
	argsStr := strings.Join(parts, " ")

	result = strings.ReplaceAll(result, "{{args}}", argsStr)

	// Clean up extra whitespace (e.g. trailing space when {{args}} is empty).
	result = strings.Join(strings.Fields(result), " ")

	return result, nil
}

// FormatValue is how a parameter value is written into commands, names and
// checks. Params cross JSON on the way from the CLI to the service, so an
// int arrives as float64 — and %v prints float64(20261009) as
// "2.0261009e+07", which argparse rejects. Integral floats print as
// integers and other floats in full decimal:
//
//	float64(20261009) → "20261009"
//	float64(0.001)    → "0.001"
//	float64(1e-6)     → "0.000001"
//	string("adam")    → "adam"
//	bool(true)        → "true"
func FormatValue(v any) string {
	switch n := v.(type) {
	case float64:
		if n == math.Trunc(n) && math.Abs(n) < 1e15 {
			return strconv.FormatInt(int64(n), 10)
		}
		return strconv.FormatFloat(n, 'f', -1, 64)
	case float32:
		return FormatValue(float64(n))
	}
	return fmt.Sprintf("%v", v)
}

func formatValue(v any) string { return FormatValue(v) }

// sortedKeys returns the keys of a TaskParams map in sorted order.
func sortedKeys(params TaskParams) []string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// UnconsumedParams reports values passed only through params.json.
func UnconsumedParams(template string, params TaskParams, exclude map[string]bool) []string {
	if strings.Contains(template, "{{args}}") {
		return nil
	}
	used := map[string]bool{}
	for _, match := range placeholderRe.FindAllStringSubmatch(template, -1) {
		used[match[1]] = true
	}
	var keys []string
	for key := range params {
		if !used[key] && !exclude[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
