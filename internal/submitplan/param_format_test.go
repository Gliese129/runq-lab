package submitplan

import (
	"testing"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/project"
)

func TestIntParamFromJSONPassesValidation(t *testing.T) {
	proj := &project.Config{Params: []project.ParamDef{{Name: "seed", Type: "int"}, {Name: "bs", Type: "int", Choices: []string{"32", "64"}}}}
	tasks := []job.TaskParams{{"seed": float64(20261009), "bs": float64(64)}}
	if err := validateParamTypes(proj, tasks); err != nil {
		t.Fatalf("int seed rejected: %v", err)
	}
	if err := validateChoices(proj, tasks); err != nil {
		t.Fatalf("int choice rejected: %v", err)
	}
}
