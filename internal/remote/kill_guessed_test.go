package remote

import (
	"context"
	"errors"
	"testing"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/store"
)

// A guessed failure (inferred, not settled) may hide a live job: kill sends
// its cancel. A cancel that succeeds proves it was alive (killed); one that
// fails means it ended as guessed (left alone, not an error).
func TestKillCancelsAGuessedFailure(t *testing.T) {
	for _, c := range []struct {
		name      string
		cancelErr error
		want      string
	}{
		{"alive", nil, "killed"},
		{"ended", errors.New("invalid job id"), "failed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			st, err := store.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			_, taskID := seedTask(t, st, t.TempDir(), "failed", SourceInferred)
			var ran []string
			b := &Backend{Cfg: &config.TargetConfig{KillTemplate: "qdel {{ext_id}}"}, Store: st,
				FS: newTestFSFromRunner(func(_ context.Context, cmd string) (string, error) {
					ran = append(ran, cmd)
					return "", c.cancelErr
				})}
			if _, err := b.Kill(context.Background(), taskID); err != nil {
				t.Fatalf("kill: %v", err)
			}
			if len(ran) != 1 || ran[0] != "qdel '55'" {
				t.Fatalf("cancel commands = %q", ran)
			}
			got, _ := st.GetTask(context.Background(), taskID)
			if got.Status != c.want {
				t.Fatalf("status = %s, want %s", got.Status, c.want)
			}
		})
	}
}
