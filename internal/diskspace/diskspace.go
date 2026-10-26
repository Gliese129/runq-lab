// Package diskspace answers "how full are the filesystems this job writes
// to, and how much does a job leave behind" on a target, through its rfs.FS.
//
// HPC clusters run out of space and inodes often, and a target may span
// several filesystems (TSUBAME: home on one, bulk data on another; a
// target's data_path is usually not on the filesystem of the project's
// working_dir), so every answer is per filesystem, keyed by mount point.
//
// Every query is ONE Exec on the target: a POSIX sh script that runs df/du
// per path and prints delimited sections, so the per-call cost over SSH is
// one round trip whatever the number of paths. Output is parsed
// defensively — GNU and BSD df headers differ, long device names wrap onto
// their own line, and some filesystems report "-" for inodes.
package diskspace

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/utils"
)

// Filesystem is one mounted filesystem and the requested paths on it.
// Inode fields are meaningful only when InodesKnown (NFS/GPFS/btrfs may
// report "-" or 0).
type Filesystem struct {
	Mount       string   `json:"mount"`
	Device      string   `json:"device,omitempty"`
	Paths       []string `json:"paths"`
	TotalBytes  int64    `json:"total_bytes"`
	FreeBytes   int64    `json:"free_bytes"`
	TotalInodes int64    `json:"total_inodes,omitempty"`
	FreeInodes  int64    `json:"free_inodes,omitempty"`
	InodesKnown bool     `json:"inodes_known"`
}

// FreeBytesFrac is the fraction of the filesystem still free (0..1); 1 when
// the size is unknown, so an unknown never reads as "full".
func (f Filesystem) FreeBytesFrac() float64 {
	if f.TotalBytes <= 0 {
		return 1
	}
	return float64(f.FreeBytes) / float64(f.TotalBytes)
}

// FreeInodesFrac is FreeBytesFrac for inodes; 1 when inodes are unknown.
func (f Filesystem) FreeInodesFrac() float64 {
	if !f.InodesKnown || f.TotalInodes <= 0 {
		return 1
	}
	return float64(f.FreeInodes) / float64(f.TotalInodes)
}

// dfStats is one parsed df data row.
type dfStats struct {
	device      string
	mount       string
	totalBytes  int64
	freeBytes   int64
	bytesKnown  bool
	totalInodes int64
	freeInodes  int64
	inodesKnown bool
}

// Probe reports the filesystems holding paths, one Exec for all of them.
// A path that does not exist yet is probed at its nearest existing parent
// (a job directory before the first submit). Paths that share a mount are
// grouped; a path df cannot answer for is left out. The error is a
// transport or exit failure with no usable answer at all.
func Probe(ctx context.Context, fsys rfs.FS, paths ...string) ([]Filesystem, error) {
	paths = dedupe(paths)
	if len(paths) == 0 {
		return nil, nil
	}
	out, _, _, err := fsys.Exec(ctx, "sh", "-c", ProbeScript(paths))
	if err != nil {
		return nil, err
	}
	fss := ParseProbe(string(out), paths)
	if len(fss) == 0 {
		return nil, fmt.Errorf("df returned no usable rows for %s", strings.Join(paths, ", "))
	}
	return fss, nil
}

// ProbeScript is the sh program Probe runs: for path i, walk up to the
// nearest existing ancestor, then print "@@k i" + `df -P -k` and
// "@@i i" + `df -P -i`. Per-path sections keep every answer attributable
// even when df fails for one path (its section is just empty).
func ProbeScript(paths []string) string {
	var s strings.Builder
	s.WriteString("export LC_ALL=C\n")
	for i, p := range paths {
		fmt.Fprintf(&s, "p=%s\n", utils.ShellQuote(p))
		s.WriteString(nearestExisting)
		fmt.Fprintf(&s, "echo '@@k %d'; df -P -k \"$p\" 2>/dev/null\n", i)
		fmt.Fprintf(&s, "echo '@@i %d'; df -P -i \"$p\" 2>/dev/null\n", i)
	}
	s.WriteString("exit 0\n")
	return s.String()
}

const nearestExisting = `while [ ! -e "$p" ] && [ "$p" != / ] && [ "$p" != . ]; do p=$(dirname "$p"); done` + "\n"

// ParseProbe turns ProbeScript output into filesystems, grouped by mount
// in first-seen order. paths[i] is the path behind section i.
func ParseProbe(out string, paths []string) []Filesystem {
	sections := splitSections(out)
	var fss []Filesystem
	index := map[string]int{}
	for i, p := range paths {
		k, kok := parseDF(sections[fmt.Sprintf("k %d", i)])
		in, iok := parseDF(sections[fmt.Sprintf("i %d", i)])
		if !kok && !iok {
			continue
		}
		row := k
		if !kok {
			row = in
		}
		if !row.bytesKnown && in.bytesKnown { // BSD df -i also carries blocks
			row.totalBytes, row.freeBytes, row.bytesKnown = in.totalBytes, in.freeBytes, true
		}
		if iok && in.inodesKnown {
			row.totalInodes, row.freeInodes, row.inodesKnown = in.totalInodes, in.freeInodes, true
		}
		if row.mount == "" {
			continue
		}
		if at, ok := index[row.mount]; ok {
			fss[at].Paths = append(fss[at].Paths, p)
			continue
		}
		index[row.mount] = len(fss)
		fss = append(fss, Filesystem{
			Mount: row.mount, Device: row.device, Paths: []string{p},
			TotalBytes: row.totalBytes, FreeBytes: row.freeBytes,
			TotalInodes: row.totalInodes, FreeInodes: row.freeInodes, InodesKnown: row.inodesKnown,
		})
	}
	return fss
}

// splitSections splits "@@<key>" delimited output into key → body.
func splitSections(out string) map[string]string {
	sections := map[string]string{}
	key := ""
	var body strings.Builder
	flush := func() {
		if key != "" {
			sections[key] = body.String()
		}
		body.Reset()
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "@@") {
			flush()
			key = strings.TrimSpace(strings.TrimPrefix(line, "@@"))
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	flush()
	return sections
}

// parseDF parses one `df -P -k` or `df -P -i` answer (GNU or BSD): a
// header row and one data row, which may be wrapped after a long device
// name. Columns are found by header name, not position:
//
//	GNU -k:  Filesystem 1024-blocks Used Available Capacity Mounted on
//	GNU -i:  Filesystem Inodes IUsed IFree IUse% Mounted on
//	BSD -i:  Filesystem 512-blocks Used Available Capacity iused ifree %iused Mounted on
func parseDF(body string) (dfStats, bool) {
	var header []string
	var data []string
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if header == nil {
			if !strings.EqualFold(fields[0], "Filesystem") {
				continue // stray noise before the header
			}
			header = headerColumns(fields)
			continue
		}
		data = append(data, fields...)
		if len(data) >= len(header) {
			break
		}
		// fewer fields than columns: a wrapped row, the rest is on the next line
	}
	if header == nil || len(data) < len(header) {
		return dfStats{}, false
	}
	n := len(header)
	st := dfStats{device: data[0], mount: strings.Join(data[n-1:], " ")}
	unit := int64(0) // bytes per block, from the "<N>-blocks" header
	var total, avail, iused, itotal, ifree int64
	var totalOK, availOK, iusedOK, itotalOK, ifreeOK bool
	for c := 1; c < n-1; c++ {
		name := strings.ToLower(header[c])
		v, ok := parseCount(data[c])
		switch {
		case strings.HasSuffix(name, "-blocks"):
			unit = blockUnit(strings.TrimSuffix(name, "-blocks"))
			total, totalOK = v, ok
		case name == "available" || name == "avail":
			avail, availOK = v, ok
		case name == "inodes":
			itotal, itotalOK = v, ok
		case name == "iused":
			iused, iusedOK = v, ok
		case name == "ifree":
			ifree, ifreeOK = v, ok
		}
	}
	if unit > 0 && totalOK && availOK {
		st.totalBytes, st.freeBytes, st.bytesKnown = total*unit, avail*unit, true
	}
	if !itotalOK && iusedOK && ifreeOK {
		itotal, itotalOK = iused+ifree, true // BSD: no total column
	}
	if itotalOK && ifreeOK && itotal > 0 {
		st.totalInodes, st.freeInodes, st.inodesKnown = itotal, ifree, true
	}
	return st, true
}

// headerColumns merges the two-word "Mounted on" into one column.
func headerColumns(fields []string) []string {
	out := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		if strings.EqualFold(fields[i], "Mounted") && i+1 < len(fields) && strings.EqualFold(fields[i+1], "on") {
			out = append(out, "Mounted on")
			i++
			continue
		}
		out = append(out, fields[i])
	}
	return out
}

// blockUnit reads the "1024" / "1K" / "512" prefix of a "-blocks" header.
func blockUnit(s string) int64 {
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "k"):
		mult, s = 1024, strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		mult, s = 1024*1024, strings.TrimSuffix(s, "m")
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 {
		return 0
	}
	return v * mult
}

// parseCount parses a df number; "-" (not reported) and garbage are !ok.
func parseCount(s string) (int64, bool) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

func dedupe(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// ── directory usage ──

// Usage is what a directory tree holds: bytes on disk and entries
// (files + directories — what it costs in inodes).
type Usage struct {
	Bytes int64 `json:"bytes"`
	Files int64 `json:"files"`
}

// Measure reports dirs' usage in one Exec: `du -sk` for bytes and
// `du -s --inodes` (GNU) or `find | wc -l` (BSD/macOS) for entries. A dir
// that does not exist is absent from the result; partially unreadable
// trees still count what du could read.
func Measure(ctx context.Context, fsys rfs.FS, dirs ...string) (map[string]Usage, error) {
	dirs = dedupe(dirs)
	if len(dirs) == 0 {
		return map[string]Usage{}, nil
	}
	out, _, _, err := fsys.Exec(ctx, "sh", "-c", MeasureScript(dirs))
	if err != nil {
		return nil, err
	}
	return ParseMeasure(string(out), dirs), nil
}

// MeasureScript prints "@@u i" then "<KiB> <entries>" for each existing dir.
func MeasureScript(dirs []string) string {
	var s strings.Builder
	s.WriteString("export LC_ALL=C\n")
	for i, d := range dirs {
		q := utils.ShellQuote(d)
		fmt.Fprintf(&s, "if [ -e %s ]; then\n", q)
		fmt.Fprintf(&s, "  k=$(du -sk %s 2>/dev/null | tail -n 1 | cut -f1)\n", q)
		fmt.Fprintf(&s, "  n=$(du -s --inodes %s 2>/dev/null | tail -n 1 | cut -f1)\n", q)
		fmt.Fprintf(&s, "  [ -n \"$n\" ] || n=$(find %s 2>/dev/null | wc -l)\n", q)
		fmt.Fprintf(&s, "  echo '@@u %d'; echo \"$k $n\"\n", i)
		s.WriteString("fi\n")
	}
	s.WriteString("exit 0\n")
	return s.String()
}

// ParseMeasure turns MeasureScript output into dir → usage.
func ParseMeasure(out string, dirs []string) map[string]Usage {
	sections := splitSections(out)
	res := map[string]Usage{}
	for i, d := range dirs {
		fields := strings.Fields(sections[fmt.Sprintf("u %d", i)])
		if len(fields) == 0 {
			continue
		}
		kib, ok := parseCount(fields[0])
		if !ok {
			continue
		}
		u := Usage{Bytes: kib * 1024}
		if len(fields) >= 2 {
			u.Files, _ = parseCount(fields[1])
		}
		res[d] = u
	}
	return res
}

// For returns the probed filesystem holding p, if any.
func For(fss []Filesystem, p string) (Filesystem, bool) {
	for _, f := range fss {
		for _, q := range f.Paths {
			if q == p {
				return f, true
			}
		}
	}
	return Filesystem{}, false
}
