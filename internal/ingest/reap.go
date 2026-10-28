package ingest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/workspace"
)

// Target identifies the task workspace whose SDK outputs should be ingested.
type Target struct {
	TaskID string
	JobID  string
	Dir    string

	// FS is the filesystem Dir lives on. nil = local (os semantics). Remote
	// backends pass their rfs.SSHFS so metrics.jsonl on the cluster is
	// actually readable — a bare os.Open of a remote path silently reads
	// nothing.
	FS rfs.FS
}

// Result summarizes what was ingested from the task's SDK output files.
// Counts are informational; no decision logic depends on them.
type Result struct {
	LatestStep  *float64
	MetricCount int
	ResultCount int // result records parsed from results.jsonl
}

// SDK file contract — each stream has its own file so the semantics split
// at the API boundary and ingest never disambiguates:
//
//	metrics.jsonl — {"type":"metric","key":"loss","value":0.42,"step":100,"ts":...}
//	results.jsonl — {"ts":...,"axes":{"model":"a","step":2000},"metrics":{"math":24.8}}
//	                (a metric may be an est object; "index":["k"] names series axes)
//
// Lifecycle events (checkpoint / preempted / loop_break, written by SDKs
// before v0.6.0 to events.jsonl or a mixed metrics.jsonl) are no longer
// ingested; old lines are skipped as unknown types.
//
// `task_id` / `job_id` are NOT in the SDK payload. The caller supplies them
// through Target, keeping the SDK ignorant of internal DB schemas.
type metricEvent struct {
	Key   string   `json:"key"`
	Value *float64 `json:"value"` // null: a NaN/Inf (SDK convention), kept out of min/max/last
	Step  *int64   `json:"step"`
	TS    int64    `json:"ts"`
}

// NOTE: the legacy ReapOutputs (markless full-file reap) is GONE. Its
// "idempotent via INSERT OR IGNORE" contract silently broke when the
// point table died: streaming summaries ACCUMULATE, so a markless re-read
// double-counts. ReapIncremental's (size, parsed_offset) mark is what
// makes re-reading safe now — every caller must go through it.
//
// SDK event vocabulary and error posture (unchanged):
//   - unknown event types are debug-logged and skipped (forward
//     compatibility), bad JSON lines warn + skip.
//   - Reap MUST NOT propagate errors that would change a task's terminal
//     status: verdict-path callers treat returned errors as warn-only.

// collector performs the STREAMING REDUCTION over metrics.jsonl lines.
// Raw metric points are never stored (a chatty 3-day task is 10M+ points):
// each point folds into its (key) summary on the fly; charts read the raw
// tail window or the on-target pyramid index instead.
type collector struct {
	target Target
	path   string
	lineN  int
	aggs   map[string]*store.MetricSummaryRow
	result Result
}

func (c *collector) line(lineBytes []byte) {
	c.lineN++
	if len(lineBytes) == 0 {
		return
	}
	var t struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(lineBytes, &t); err != nil {
		logger.Warnf("reap %s line %d: parse type discriminator failed: %v", c.path, c.lineN, err)
		return
	}
	switch t.Type {
	case "metric":
		var e metricEvent
		if err := json.Unmarshal(lineBytes, &e); err != nil {
			logger.Warnf("reap %s line %d: parse metric event failed: %v", c.path, c.lineN, err)
			return
		}
		if c.aggs == nil {
			c.aggs = make(map[string]*store.MetricSummaryRow)
		}
		if v := e.Value; v != nil {
			agg, ok := c.aggs[e.Key]
			if !ok {
				c.aggs[e.Key] = &store.MetricSummaryRow{
					TaskID: c.target.TaskID, JobID: c.target.JobID, Key: e.Key,
					Min: *v, Max: *v, Last: *v, LastTS: e.TS, Count: 1,
				}
			} else {
				agg.Min = min(agg.Min, *v)
				agg.Max = max(agg.Max, *v)
				if e.TS >= agg.LastTS {
					agg.Last, agg.LastTS = *v, e.TS
				}
				agg.Count++
			}
		}
		if e.Step != nil {
			v := float64(*e.Step)
			c.result.LatestStep = &v
		}
		c.result.MetricCount++
	default:
		logger.Debugf("reap %s line %d: unknown event type %q (skipped)", c.path, c.lineN, t.Type)
	}
}

// summaryRows materializes the per-key reductions for the atomic apply.
func (c *collector) summaryRows() []store.MetricSummaryRow {
	rows := make([]store.MetricSummaryRow, 0, len(c.aggs))
	for _, a := range c.aggs {
		rows = append(rows, *a)
	}
	return rows
}

// resultLine maps one results.jsonl line. Axes/metrics stay raw JSON —
// key classification is the results endpoint's concern.
type resultLine struct {
	Section string          `json:"section"`
	TS      int64           `json:"ts"`
	Axes    json.RawMessage `json:"axes"`
	Index   []string        `json:"index"` // axes from a runq.stats series
	Metrics json.RawMessage `json:"metrics"`
}

type resultCollector struct {
	target Target
	path   string
	lineN  int
	parsed int // valid records seen
	rows   []store.ResultRecordRow
}

func (c *resultCollector) line(lineBytes []byte) {
	c.lineN++
	if len(lineBytes) == 0 {
		return
	}
	var rl resultLine
	if err := json.Unmarshal(lineBytes, &rl); err != nil {
		logger.Warnf("reap %s line %d: parse result record failed: %v", c.path, c.lineN, err)
		return
	}
	axes, ok := normalizeJSONObject(rl.Axes)
	if !ok {
		logger.Warnf("reap %s line %d: axes is not a JSON object (skipped)", c.path, c.lineN)
		return
	}
	metrics, ok := normalizeJSONObject(rl.Metrics)
	if !ok {
		logger.Warnf("reap %s line %d: metrics is not a JSON object (skipped)", c.path, c.lineN)
		return
	}
	c.parsed++
	index := ""
	if len(rl.Index) > 0 {
		raw, _ := json.Marshal(rl.Index)
		index = string(raw)
	}
	// Every snapshot participates in the latest-record projection.
	c.rows = append(c.rows, store.ResultRecordRow{
		TaskID: c.target.TaskID, JobID: c.target.JobID,
		TS: rl.TS, AxesJSON: axes, MetricsJSON: metrics, IndexJSON: index, Section: rl.Section,
	})
}

// normalizeJSONObject validates raw as a JSON object and compacts it.
// Absent / null → "{}" (the SDK always writes both fields; tolerate
// hand-written lines that omit one).
func normalizeJSONObject(raw json.RawMessage) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "{}", true
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &m); err != nil {
		return "", false
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trimmed); err != nil {
		return "", false
	}
	return buf.String(), true
}

// scanOutcome reports one incremental pass over a single jsonl file.
type scanOutcome struct {
	missing  bool  // file does not exist
	skip     bool  // no new bytes (and not a final pass)
	rebuild  bool  // file shrank — caller must rebuild derived rows
	size     int64 // stat size at this pass
	consumed int64 // parsed up to here (complete lines; tail included when final)
}

// scanIncremental applies the (size, parsed_offset) incremental-read
// doctrine (spec §8.1.4) to ONE file, feeding each complete line to onLine:
//
//	size unchanged → zero transfer, zero parse (the common idle case)
//	size grew      → seek to parsed_offset, parse the delta's COMPLETE
//	                 lines (an unterminated tail waits for the next pass)
//	size shrank    → the file was rewritten: rebuild from 0
//
// final consumes the tail even without a trailing newline. The function
// performs NO store writes — pairing the parsed delta with its mark update
// atomically is the caller's job.
func scanIncremental(fsys rfs.FS, path string, markSize, markOffset int64, final bool, onLine func([]byte)) (scanOutcome, error) {
	info, err := fsys.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return scanOutcome{missing: true}, nil
	}
	if err != nil {
		return scanOutcome{}, err
	}

	size := info.Size()
	offset := markOffset
	rebuild := false
	if size < markSize {
		rebuild = true
		offset = 0
	} else if size == markSize && !final {
		return scanOutcome{skip: true, size: size}, nil
	}

	f, err := fsys.Open(path)
	if err != nil {
		return scanOutcome{}, err
	}
	defer f.Close()
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return scanOutcome{}, err
		}
	}

	br := bufio.NewReaderSize(f, 64*1024)
	consumed := offset
	for {
		raw, rerr := br.ReadBytes('\n')
		complete := rerr == nil
		if len(raw) > 0 && (complete || final) {
			onLine(bytes.TrimRight(raw, "\r\n"))
			consumed += int64(len(raw))
		}
		if rerr != nil {
			if rerr != io.EOF {
				logger.Warnf("reap %s: read error (partial result): %v", path, rerr)
			}
			break
		}
	}
	return scanOutcome{rebuild: rebuild, size: size, consumed: consumed}, nil
}

// ReapIncremental ingests the bytes appended to the task's SDK output files
// (metrics.jsonl / results.jsonl, plus the small links.json) since the last pass, each
// under its own (size, parsed_offset) mark. final marks the terminal pass:
// tails are consumed and every mark is frozen — settled tasks are never
// stat'ed again. Per-file failures don't block the other files; errors are
// joined, and remain warn-only for callers on verdict paths (same contract
// as always).
func ReapIncremental(ctx context.Context, st *store.Store, target Target, final bool) (Result, error) {
	fsys := target.FS
	if fsys == nil {
		fsys = rfs.NewLocalFS()
	}
	var res Result
	merr := reapMetricsFile(ctx, st, target, fsys, final, &res)
	rerr := reapResultsFile(ctx, st, target, fsys, final, &res)
	lerr := reapLinksFile(ctx, st, target, fsys, final)
	var observationErr error
	if merr == nil && rerr == nil && lerr == nil {
		var output, heartbeat *int64
		logPath := path.Join(target.Dir, "log.txt")
		if task, err := st.GetTask(ctx, target.TaskID); err == nil && task != nil && task.LogPath != "" {
			logPath = task.LogPath
		}
		if fi, err := fsys.Stat(logPath); err == nil {
			v := fi.ModTime().Unix()
			output = &v
		}
		if fi, err := fsys.Stat(workspace.ActivityPath(target.Dir)); err == nil {
			v := fi.ModTime().Unix()
			heartbeat = &v
		}
		observationErr = st.ObserveSource(ctx, target.TaskID, res.LatestStep, output, heartbeat)
	}
	return res, errors.Join(merr, rerr, lerr, observationErr)
}

func reapMetricsFile(ctx context.Context, st *store.Store, target Target, fsys rfs.FS, final bool, res *Result) error {
	mark, err := st.GetIngestMark(ctx, target.TaskID)
	if err != nil {
		return err
	}
	if mark.Final {
		return nil
	}
	path := workspace.MetricsPath(target.Dir)
	c := collector{target: target, path: path}
	sc, err := scanIncremental(fsys, path, mark.Size, mark.Offset, final, c.line)
	if err != nil {
		return err
	}
	if sc.missing {
		if final { // no-SDK task: freeze so we never look again
			_ = st.SetIngestMark(ctx, target.TaskID, store.IngestMark{Final: true})
		}
		return nil
	}
	if sc.skip {
		return nil
	}
	// ONE transaction: rebuild delete + summary merges + the
	// mark. "Counted" and "accounted" are inseparable — a crash between
	// them would replay this delta next pass and inflate count/sum.
	if err := st.ApplyIngestDelta(ctx, target.TaskID, sc.rebuild,
		c.summaryRows(),
		store.IngestMark{Size: sc.size, Offset: sc.consumed, Final: final}); err != nil {
		return err
	}
	res.MetricCount += c.result.MetricCount
	res.LatestStep = c.result.LatestStep
	return nil
}

func reapResultsFile(ctx context.Context, st *store.Store, target Target, fsys rfs.FS, final bool, res *Result) error {
	mark, err := st.GetFileIngestMark(ctx, target.TaskID, store.IngestFileResults)
	if err != nil {
		return err
	}
	if mark.Final {
		return nil
	}
	path := workspace.ResultsPath(target.Dir)
	c := resultCollector{target: target, path: path}
	sc, err := scanIncremental(fsys, path, mark.Size, mark.Offset, final, c.line)
	if err != nil {
		return err
	}
	if sc.missing {
		if final {
			_ = st.SetFileIngestMark(ctx, target.TaskID, store.IngestFileResults, store.FileIngestMark{Final: true})
		}
		return nil
	}
	if sc.skip {
		return nil
	}
	if err := st.ApplyResultsIngestDelta(ctx, target.TaskID, sc.rebuild,
		c.rows, c.parsed,
		store.FileIngestMark{Size: sc.size, Offset: sc.consumed, Final: final}); err != nil {
		return err
	}
	res.ResultCount += c.parsed
	return nil
}

// maxLinksFileBytes bounds links.json: it holds a handful of URLs, so
// anything larger is misuse and is skipped rather than slurped over SSH.
const maxLinksFileBytes = 64 << 10

// reapLinksFile ingests <task_dir>/links.json (SDK runq.link) when its size
// changed. The file is a whole JSON object rewritten atomically by the SDK,
// so a size-gated full read is exact; a parse failure leaves the old rows
// (the SDK may be mid-rename) and retries next pass.
func reapLinksFile(ctx context.Context, st *store.Store, target Target, fsys rfs.FS, final bool) error {
	mark, err := st.GetFileIngestMark(ctx, target.TaskID, store.IngestFileLinks)
	if err != nil {
		return err
	}
	if mark.Final {
		return nil
	}
	path := workspace.LinksPath(target.Dir)
	info, err := fsys.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if final {
			_ = st.SetFileIngestMark(ctx, target.TaskID, store.IngestFileLinks, store.FileIngestMark{Final: true})
		}
		return nil
	}
	if err != nil {
		return err
	}
	// The SDK rewrites links.json atomically, so a changed link can keep the
	// size: the mark's Offset holds the file's mtime (ns) for links.
	mtime := info.ModTime().UnixNano()
	if info.Size() == mark.Size && mtime == mark.Offset && !final {
		return nil
	}
	if info.Size() > maxLinksFileBytes {
		logger.Warnf("reap %s: %d bytes exceeds the links cap; skipped", path, info.Size())
		return st.SetFileIngestMark(ctx, target.TaskID, store.IngestFileLinks, store.FileIngestMark{Size: info.Size(), Offset: mtime, Final: final})
	}
	data, err := fsys.ReadFile(path)
	if err != nil {
		return err
	}
	raw := map[string]any{}
	if err := json.Unmarshal(data, &raw); err != nil {
		logger.Warnf("reap %s: parse failed: %v", path, err)
		return nil
	}
	links := make(map[string]string, len(raw))
	for name, v := range raw {
		if url, ok := v.(string); ok && name != "" && url != "" {
			links[name] = url
		}
	}
	return st.ApplyLinksIngest(ctx, target.TaskID, links,
		store.FileIngestMark{Size: int64(len(data)), Offset: mtime, Final: final})
}
