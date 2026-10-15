package job

import "testing"

// Params reach the service through JSON, so ints arrive as float64; an
// 8-digit seed must render as an integer, never 2.0261009e+07.
func TestFormatValueIntegralFloats(t *testing.T) {
	for v, want := range map[any]string{float64(20261009): "20261009", 0.001: "0.001", 1e-6: "0.000001", "adam": "adam", true: "true", 3: "3"} {
		if got := FormatValue(v); got != want {
			t.Errorf("FormatValue(%#v) = %q, want %q", v, got, want)
		}
	}
	out, err := Render("python train.py --seed {{seed}} {{args}}", TaskParams{"seed": float64(20261009), "lr": 1e-6})
	if err != nil {
		t.Fatal(err)
	}
	if want := "python train.py --seed 20261009 --lr=0.000001"; out != want {
		t.Errorf("Render = %q, want %q", out, want)
	}
}
