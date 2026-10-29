package dashboard

import (
	"math"
	"testing"

	"github.com/gliese129/runq-lab/internal/workspace"
)

// Lines decimated (or logged) at different steps are averaged at the steps
// they share, never by position: step 4 is 5 in both, so its mean is 5.
func TestMeanLineAlignsBySteps(t *testing.T) {
	nan := math.NaN()
	a := curveLine{TaskID: "a", Step: []float64{0, 1, 4, 9}, Value: workspace.CurveValues{1, nan, 5, 7}}
	b := curveLine{TaskID: "b", Step: []float64{0, 4, 7, 9}, Value: workspace.CurveValues{3, 5, 6, 9}}
	mean, n, partial, ok := meanLine([]curveLine{a, b}, 400)
	if !ok || !partial {
		t.Fatalf("ok=%v partial=%v", ok, partial)
	}
	want := map[float64]float64{0: 2, 4: 5, 9: 8}
	if len(mean.Step) != 3 {
		t.Fatalf("steps = %v, want the shared 0, 4, 9", mean.Step)
	}
	for i, s := range mean.Step {
		if mean.Value[i] != want[s] || n[i] != 2 {
			t.Errorf("step %v: mean %v n %d, want %v", s, mean.Value[i], n[i], want[s])
		}
	}
	if lo, hi := *mean.Lo[1], *mean.Hi[1]; lo != 5 || hi != 5 {
		t.Errorf("equal values have no spread: %v..%v", lo, hi)
	}

	// a gap in any line is a gap in the mean
	b.Value[1] = nan
	if mean, _, _, _ := meanLine([]curveLine{a, b}, 400); !math.IsNaN(mean.Value[1]) || mean.Lo[1] != nil {
		t.Errorf("gap at step 4 lost: %v", mean.Value)
	}

	// nothing shared: no mean to draw
	c := curveLine{TaskID: "c", Step: []float64{2, 3}, Value: workspace.CurveValues{1, 1}}
	if _, _, _, ok := meanLine([]curveLine{a, c}, 400); ok {
		t.Error("lines sharing no step must not be averaged")
	}
}

func TestThinKeepsEndsAndGaps(t *testing.T) {
	l := curveLine{}
	for i := 0; i < 1000; i++ {
		l.Step = append(l.Step, float64(i))
		v := float64(i)
		if i == 333 {
			v = math.NaN()
		}
		l.Value = append(l.Value, v)
	}
	out, _ := thin(l, nil, 10)
	if out.Step[0] != 0 || out.Step[len(out.Step)-1] != 999 || len(out.Step) > 11 {
		t.Fatalf("thinned steps = %v", out.Step)
	}
	gap := false
	for _, v := range out.Value {
		gap = gap || math.IsNaN(v)
	}
	if !gap {
		t.Error("a gap must survive thinning")
	}
}
