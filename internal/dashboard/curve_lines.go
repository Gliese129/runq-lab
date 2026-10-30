package dashboard

import (
	"math"
	"slices"
	"sort"

	"github.com/gliese129/runq-lab/internal/workspace"
)

// curveLine is one version's curve: its points as logged (no
// interpolation), with the estimate band where runq.log wrote one. The
// experiment's curves tab and a saved view's curves chart both draw it.
type curveLine struct {
	TaskID string                 `json:"task_id"`
	Cell   string                 `json:"cell"`
	Step   []float64              `json:"step"`
	Value  workspace.CurveValues  `json:"value"` // null = a gap
	Lo     []*float64             `json:"lo,omitempty"`
	Hi     []*float64             `json:"hi,omitempty"`
	Series *workspace.CurveSeries `json:"series,omitempty"`
}

func lineOf(taskID, cell string, sr workspace.ExactSeries) curveLine {
	return curveLine{TaskID: taskID, Cell: cell, Step: sr.Step, Value: sr.Value, Lo: sr.Lo, Hi: sr.Hi, Series: sr.Series}
}

// upTo keeps the points at or before maxStep.
func (l curveLine) upTo(maxStep float64) curveLine {
	out := l
	out.Step, out.Value, out.Lo, out.Hi = nil, nil, nil, nil
	for i, step := range l.Step {
		if step > maxStep {
			continue
		}
		out.Step = append(out.Step, step)
		out.Value = append(out.Value, l.Value[i])
		if l.Lo != nil {
			out.Lo = append(out.Lo, l.Lo[i])
			out.Hi = append(out.Hi, l.Hi[i])
		}
	}
	return out
}

// meanLine averages lines at the steps every one of them logged — never at
// a step some line lacks, so lines decimated or logged differently are not
// misread by position. A step where any line logged a gap is a gap in the
// mean, whether or not the others logged that step: averaging only the
// shared steps must not join a line across its break. lo/hi is mean ± std
// (sample) where n ≥ 2. The result is thinned to points (gaps kept). ok is
// false when the lines share fewer than two steps: there is no honest mean
// to draw.
func meanLine(lines []curveLine, points int) (mean curveLine, n []int, partial, ok bool) {
	if len(lines) == 0 {
		return curveLine{}, nil, false, false
	}
	at := make([]map[float64]int, len(lines))
	for i, l := range lines {
		at[i] = make(map[float64]int, len(l.Step))
		for j, s := range l.Step {
			at[i][s] = j
		}
	}
	gapAt := map[float64]bool{}
	for _, l := range lines {
		for j, s := range l.Step {
			if v := l.Value[j]; math.IsNaN(v) || math.IsInf(v, 0) {
				gapAt[s] = true
			}
		}
	}
	var common []float64
	for _, s := range lines[0].Step {
		every := true
		for i := 1; i < len(lines); i++ {
			if _, has := at[i][s]; !has {
				every = false
				break
			}
		}
		if every {
			common = append(common, s)
		}
	}
	sort.Float64s(common)
	if len(common) < 2 && len(lines) > 1 {
		return curveLine{}, nil, false, false
	}
	for _, l := range lines {
		if len(l.Step) != len(common) {
			partial = true
		}
	}
	steps := slices.Clone(common)
	for s := range gapAt {
		if !slices.Contains(common, s) {
			steps = append(steps, s) // a break only some lines logged
		}
	}
	sort.Float64s(steps)
	k := float64(len(lines))
	for _, s := range steps {
		sum, gap := 0., gapAt[s]
		for i, l := range lines {
			if gap {
				break
			}
			sum += l.Value[at[i][s]]
		}
		mean.Step = append(mean.Step, s)
		n = append(n, len(lines))
		if gap {
			mean.Value = append(mean.Value, math.NaN())
			mean.Lo, mean.Hi = append(mean.Lo, nil), append(mean.Hi, nil)
			continue
		}
		m := sum / k
		mean.Value = append(mean.Value, m)
		if len(lines) < 2 {
			mean.Lo, mean.Hi = append(mean.Lo, nil), append(mean.Hi, nil)
			continue
		}
		ss := 0.
		for i, l := range lines {
			d := l.Value[at[i][s]] - m
			ss += d * d
		}
		sd := math.Sqrt(ss / (k - 1))
		lo, hi := m-sd, m+sd
		mean.Lo, mean.Hi = append(mean.Lo, &lo), append(mean.Hi, &hi)
	}
	mean, n = thin(mean, n, points)
	return mean, n, partial, true
}

// thin keeps about points evenly spaced points of a line, always keeping
// its first, its last and every gap (so a break never disappears).
func thin(l curveLine, n []int, points int) (curveLine, []int) {
	if points < 2 || len(l.Step) <= points {
		return l, n
	}
	keep := make([]bool, len(l.Step))
	last := len(l.Step) - 1
	for i := 0; i < points; i++ {
		keep[int(math.Round(float64(i)*float64(last)/float64(points-1)))] = true
	}
	for i, v := range l.Value {
		if math.IsNaN(v) {
			keep[i] = true
		}
	}
	out := curveLine{TaskID: l.TaskID, Cell: l.Cell, Series: l.Series}
	var outN []int
	for i, k := range keep {
		if !k {
			continue
		}
		out.Step = append(out.Step, l.Step[i])
		out.Value = append(out.Value, l.Value[i])
		if l.Lo != nil {
			out.Lo, out.Hi = append(out.Lo, l.Lo[i]), append(out.Hi, l.Hi[i])
		}
		if n != nil {
			outN = append(outN, n[i])
		}
	}
	return out, outN
}
