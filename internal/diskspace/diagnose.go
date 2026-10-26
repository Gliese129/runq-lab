package diskspace

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/utils"
)

// LogTailBytes bounds how much of a failed task's log Diagnose reads.
const LogTailBytes = 64 << 10

// diskFullPattern is what the kernel's ENOSPC / EDQUOT look like in a
// traceback (Python prints "[Errno 28] No space left on device").
const diskFullPattern = `No space left on device|Disk quota exceeded|ENOSPC|EDQUOT`

// Kind is what ran out.
type Kind string

const (
	KindInodes  Kind = "inodes"  // df -i at 100%: too many files
	KindSpace   Kind = "space"   // blocks at 100%
	KindQuota   Kind = "quota"   // per-user/group quota (df can't see it)
	KindUnclear Kind = "unclear" // ENOSPC, but df shows room now
)

// Diagnosis is a disk-full failure found in a log, with the filesystem
// state measured right after.
type Diagnosis struct {
	Match string // the log text that matched
	Kind  Kind
	FS    *Filesystem // nil when df failed
}

// Diagnose looks for a disk-full error in the last LogTailBytes of
// logPath and, only when one is there, probes dir's filesystem — all in
// one Exec. ok=false means no disk-full error in the log.
func Diagnose(ctx context.Context, fsys rfs.FS, logPath, dir string) (Diagnosis, bool, error) {
	out, _, _, err := fsys.Exec(ctx, "sh", "-c", DiagnoseScript(logPath, dir))
	if err != nil {
		return Diagnosis{}, false, err
	}
	d, ok := ParseDiagnose(string(out), dir)
	return d, ok, nil
}

// DiagnoseScript greps the log tail; on a match it prints "@@m" + the last
// matching text, then the ProbeScript sections for dir.
func DiagnoseScript(logPath, dir string) string {
	var s strings.Builder
	s.WriteString("export LC_ALL=C\n")
	fmt.Fprintf(&s, "m=$(tail -c %d %s 2>/dev/null | grep -E -o '%s' | tail -n 1)\n",
		LogTailBytes, utils.ShellQuote(logPath), diskFullPattern)
	s.WriteString("[ -n \"$m\" ] || exit 0\n")
	s.WriteString("echo '@@m'; echo \"$m\"\n")
	s.WriteString(ProbeScript([]string{dir}))
	return s.String()
}

// ParseDiagnose reads DiagnoseScript output.
func ParseDiagnose(out, dir string) (Diagnosis, bool) {
	sections := splitSections(out)
	m, ok := sections["m"]
	if !ok || strings.TrimSpace(m) == "" {
		return Diagnosis{}, false
	}
	d := Diagnosis{Match: strings.TrimSpace(m)}
	if fss := ParseProbe(out, []string{dir}); len(fss) > 0 {
		d.FS = &fss[0]
	}
	d.Kind = classify(d.Match, d.FS)
	return d, true
}

// fullFrac is "at 100%" as df rounds it.
const fullFrac = 0.005

func classify(match string, fs *Filesystem) Kind {
	if match == "Disk quota exceeded" || match == "EDQUOT" {
		return KindQuota
	}
	if fs == nil {
		return KindUnclear
	}
	if fs.InodesKnown && fs.FreeInodesFrac() < fullFrac {
		return KindInodes
	}
	if fs.TotalBytes > 0 && fs.FreeBytesFrac() < fullFrac {
		return KindSpace
	}
	return KindUnclear
}

// Detail is the task's failure reason: what ran out, where, and what to
// do about it. The suggested clean spans jobs: the failed job's own
// checkpoints may be what a rerun resumes from.
func (d Diagnosis) Detail() string {
	mount := "the task directory's filesystem"
	if d.FS != nil {
		mount = d.FS.Mount
	}
	var head string
	switch d.Kind {
	case KindInodes:
		head = fmt.Sprintf("disk full (inodes): %s has no free inodes (%s of %s free) — too many files, not too many bytes",
			mount, Count(d.FS.FreeInodes), Count(d.FS.TotalInodes))
	case KindSpace:
		head = fmt.Sprintf("disk full (space): %s has %s of %s free",
			mount, Bytes(d.FS.FreeBytes), Bytes(d.FS.TotalBytes))
	case KindQuota:
		head = fmt.Sprintf("disk full (quota): your quota on %s is used up — df does not show quotas; check the cluster's quota command", mount)
	default:
		now := ""
		if d.FS != nil {
			now = fmt.Sprintf(" (now %s and %s inodes free", Bytes(d.FS.FreeBytes), Count(d.FS.FreeInodes))
			if !d.FS.InodesKnown {
				now = fmt.Sprintf(" (now %s free", Bytes(d.FS.FreeBytes))
			}
			now += " — space may have been freed since, or a quota or a single Lustre OST was full)"
		}
		head = fmt.Sprintf("disk full: %q on %s%s", d.Match, mount, now)
	}
	return head + "\nlog: " + d.Match + "\nfree space: runq clean --checkpoints --older-than 7d --show, or remove files you no longer need on " + mount
}

// Bytes formats a byte count for messages (binary units).
func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + " " + string("KMGTP"[exp]) + "B"
}

// Count formats an inode/file count for messages (1.2M, 340k).
func Count(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return strconv.FormatFloat(float64(n)/1e9, 'f', 1, 64) + "G"
	case n >= 1_000_000:
		return strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64) + "M"
	case n >= 10_000:
		return strconv.FormatFloat(float64(n)/1e3, 'f', 0, 64) + "k"
	}
	return strconv.FormatInt(n, 10)
}
