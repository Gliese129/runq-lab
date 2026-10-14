package ingest

import (
	"context"
	"encoding/json"
	"path"

	"github.com/gliese129/runq-lab/internal/consumer"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
)

// ReapConsumer shares the task JSONL scanner and latest-record projection.
func ReapConsumer(ctx context.Context, st *store.Store, c consumer.Consumer, fsys rfs.FS, final bool) error {
	for _, file := range []string{"results", "metrics"} {
		mark, err := st.ConsumerFileMark(ctx, c.ID, file)
		if err != nil {
			return err
		}
		// A consumer can append again after a terminal state. Keep its byte
		// cursor, but never treat a previous final ingest as permanent.
		collector := resultCollector{target: Target{TaskID: c.ID, JobID: c.JobID}, path: path.Join(c.Dir, file+".jsonl")}
		metrics := []store.ConsumerMetric{}
		line := collector.line
		if file == "metrics" {
			line = func(raw []byte) {
				var m struct {
					Key   string   `json:"key"`
					Value *float64 `json:"value"`
					TS    int64    `json:"ts"`
					Step  *float64 `json:"step"`
				}
				if json.Unmarshal(raw, &m) == nil && m.Value != nil && m.Key != "" {
					metrics = append(metrics, store.ConsumerMetric{Key: m.Key, Value: *m.Value, TS: m.TS, Step: m.Step})
				}
			}
		}
		sc, err := scanIncremental(fsys, collector.path, mark.Size, mark.Offset, final, line)
		if err != nil {
			return err
		}
		if sc.skip {
			continue
		}
		if sc.missing && !final {
			continue
		}
		if err := st.ApplyConsumerIngest(ctx, c, file, sc.rebuild, collector.rows, metrics, store.FileIngestMark{Size: sc.size, Offset: sc.consumed, Final: final}); err != nil {
			return err
		}
	}
	return nil
}
