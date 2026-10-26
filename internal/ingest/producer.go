package ingest

import (
	"context"
	"path"
	"regexp"
	"strings"

	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
)

// Producer results: a script other than the task (a scoring script reading
// the job with runq.job()) writes <task_dir>/results.<producer>.jsonl in the
// results.jsonl line format. Each file is its own record source,
// store.ProducerSourceID(task, producer), so views tell producers apart and a
// producer's sections freeze independently of the task's own.
//
// Unlike the task's files these marks never freeze: a script may score a
// task long after the task settled.

var producerName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ValidProducer reports whether name can be a producer (the SDK enforces the
// same pattern).
func ValidProducer(name string) bool { return producerName.MatchString(name) }

// ProducerFromFile maps "results.<producer>.jsonl" to its producer.
func ProducerFromFile(name string) (string, bool) {
	p, ok := strings.CutPrefix(name, "results.")
	if !ok {
		return "", false
	}
	p, ok = strings.CutSuffix(p, ".jsonl")
	if !ok || !ValidProducer(p) {
		return "", false
	}
	return p, true
}

// ProducerResultsPath is <task_dir>/results.<producer>.jsonl.
func ProducerResultsPath(taskDir, producer string) string {
	return path.Join(taskDir, "results."+producer+".jsonl")
}

// ReapProducerResults ingests the bytes appended to one producer file since
// the last pass, exactly like results.jsonl (complete lines only, a shrunk
// file rebuilds the producer's records). size is the caller's fresh stat (a
// directory listing); an unchanged size costs nothing.
func ReapProducerResults(ctx context.Context, st *store.Store, target Target, producer string, size int64) (int, error) {
	file := store.ProducerResultsFile(producer)
	mark, err := st.GetFileIngestMark(ctx, target.TaskID, file)
	if err != nil {
		return 0, err
	}
	if size == mark.Size {
		return 0, nil
	}
	fsys := target.FS
	if fsys == nil {
		fsys = rfs.NewLocalFS()
	}
	src := target
	src.TaskID = store.ProducerSourceID(target.TaskID, producer)
	c := resultCollector{target: src, path: ProducerResultsPath(target.Dir, producer)}
	sc, err := scanIncremental(fsys, c.path, mark.Size, mark.Offset, false, c.line)
	if err != nil || sc.missing || sc.skip {
		return 0, err
	}
	err = st.ApplyProducerResultsDelta(ctx, target.TaskID, producer, sc.rebuild, c.rows,
		store.FileIngestMark{Size: sc.size, Offset: sc.consumed})
	return c.parsed, err
}
