package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/consumer"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
)

func TestReapConsumerContinuesAfterFinalizedRun(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.InsertJob(ctx, &store.JobRow{
		ID: "job", ProjectName: "project", ConfigJSON: "{}", Status: "done",
		TotalTasks: 1, Target: "local", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	c := consumer.Consumer{
		ID: "consumer", JobID: "job", Name: "score", Command: "python score.py",
		Target: "local", Generation: "generation", Dir: dir, Status: "stopped",
	}
	if err := st.InsertConsumer(ctx, &c); err != nil {
		t.Fatal(err)
	}
	results := filepath.Join(dir, "results.jsonl")
	metrics := filepath.Join(dir, "metrics.jsonl")
	if err := os.WriteFile(results, []byte("{\"section\":\"eval\",\"ts\":1,\"axes\":{\"step\":1},\"metrics\":{\"score\":1}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metrics, []byte("{\"key\":\"seen\",\"value\":1,\"step\":1,\"ts\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReapConsumer(ctx, st, c, rfs.NewLocalFS(), true); err != nil {
		t.Fatalf("first final ingest: %v", err)
	}
	for path, line := range map[string]string{
		results: "{\"section\":\"eval\",\"ts\":2,\"axes\":{\"step\":2},\"metrics\":{\"score\":2}}\n",
		metrics: "{\"key\":\"seen\",\"value\":2,\"step\":2,\"ts\":2}\n",
	} {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.WriteString(line); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
	}

	// A restarted consumer can finish between controller polls. The next
	// terminal pass must continue from the preserved byte offsets.
	if err := ReapConsumer(ctx, st, c, rfs.NewLocalFS(), true); err != nil {
		t.Fatalf("restarted final ingest: %v", err)
	}
	rows, err := st.ListResultRecords(ctx, c.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TS != 2 || rows[0].MetricsJSON != `{"score":2}` {
		t.Fatalf("restarted result progress was not ingested: %+v", rows)
	}
	var value float64
	var ts int64
	if err := st.DB().QueryRowContext(ctx,
		"SELECT last,last_ts FROM consumer_metrics WHERE source_id=? AND key=?", c.ID, "seen",
	).Scan(&value, &ts); err != nil {
		t.Fatal(err)
	}
	if value != 2 || ts != 2 {
		t.Fatalf("restarted metric progress = (%v,%d), want (2,2)", value, ts)
	}
}
