package diskspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gliese129/runq-lab/internal/rfs"
)

// Real-looking answers: TSUBAME-style Lustre, a wrapped long device, NFS
// home without inode counts, and macOS (BSD) df.
const (
	lustreK = `Filesystem                          1024-blocks          Used    Available Capacity Mounted on
10.0.2.11@o2ib:10.0.2.12@o2ib:/gs 7346045788160 6982155362304 363890425856      96% /gs/bs
`
	lustreI = `Filesystem                           Inodes       IUsed      IFree IUse% Mounted on
10.0.2.11@o2ib:10.0.2.12@o2ib:/gs 2147483648 2147480000       3648  100% /gs/bs
`
	wrappedK = `Filesystem                                       1024-blocks       Used Available Capacity Mounted on
192.168.10.1@o2ib4:192.168.10.2@o2ib4:/lustre01/project
                                                 1953124999168 1953000000000 124999168     100% /lustre01
`
	nfsK = `Filesystem          1024-blocks      Used Available Capacity Mounted on
nfs01:/export/home   104857600  52428800  52428800      50% /home
`
	nfsI = `Filesystem         Inodes IUsed IFree IUse% Mounted on
nfs01:/export/home      -     -     -     - /home
`
	macK = `Filesystem   1024-blocks      Used Available Capacity  Mounted on
/dev/disk3s5   482797652 423736032  20718888    96%    /System/Volumes/Data
`
	macI = `Filesystem   512-blocks      Used Available Capacity iused     ifree %iused  Mounted on
/dev/disk3s5  965595304 847472064  41437768    96% 5137933 207188840    2%   /System/Volumes/Data
`
)

func probeOut(sections ...string) string {
	var s strings.Builder
	for _, sec := range sections {
		s.WriteString(sec)
	}
	return s.String()
}

func TestParseProbeLustreGroupsPathsByMount(t *testing.T) {
	out := probeOut("@@k 0\n", lustreK, "@@i 0\n", lustreI, "@@k 1\n", lustreK, "@@i 1\n", lustreI)
	fss := ParseProbe(out, []string{"/gs/bs/tga-lab/runq/pj1", "/gs/bs/tga-lab/code"})
	if len(fss) != 1 {
		t.Fatalf("want one filesystem, got %+v", fss)
	}
	f := fss[0]
	if f.Mount != "/gs/bs" || len(f.Paths) != 2 {
		t.Fatalf("mount/paths = %q %v", f.Mount, f.Paths)
	}
	if f.TotalBytes != 7346045788160*1024 || f.FreeBytes != 363890425856*1024 {
		t.Fatalf("bytes = %d/%d", f.FreeBytes, f.TotalBytes)
	}
	if !f.InodesKnown || f.TotalInodes != 2147483648 || f.FreeInodes != 3648 {
		t.Fatalf("inodes = %+v", f)
	}
	if f.FreeInodesFrac() >= 0.005 {
		t.Fatalf("inodes should read as full: %v", f.FreeInodesFrac())
	}
}

func TestParseProbeWrappedDeviceLine(t *testing.T) {
	fss := ParseProbe(probeOut("@@k 0\n", wrappedK, "@@i 0\n"), []string{"/lustre01/x"})
	if len(fss) != 1 || fss[0].Mount != "/lustre01" {
		t.Fatalf("got %+v", fss)
	}
	if fss[0].Device != "192.168.10.1@o2ib4:192.168.10.2@o2ib4:/lustre01/project" || fss[0].FreeBytes != 124999168*1024 {
		t.Fatalf("got %+v", fss[0])
	}
	if fss[0].InodesKnown {
		t.Fatal("empty -i section must leave inodes unknown")
	}
}

func TestParseProbeNFSDashInodesAndTwoFilesystems(t *testing.T) {
	out := probeOut("@@k 0\n", nfsK, "@@i 0\n", nfsI, "@@k 1\n", lustreK, "@@i 1\n", lustreI)
	fss := ParseProbe(out, []string{"/home/u/proj", "/gs/bs/runq/pj1"})
	if len(fss) != 2 || fss[0].Mount != "/home" || fss[1].Mount != "/gs/bs" {
		t.Fatalf("got %+v", fss)
	}
	if fss[0].InodesKnown || fss[0].FreeInodesFrac() != 1 {
		t.Fatalf("'-' inodes must be unknown, not full: %+v", fss[0])
	}
	if fss[0].FreeBytesFrac() != 0.5 {
		t.Fatalf("free frac = %v", fss[0].FreeBytesFrac())
	}
	if f, ok := For(fss, "/gs/bs/runq/pj1"); !ok || f.Mount != "/gs/bs" {
		t.Fatalf("For = %+v %v", f, ok)
	}
}

func TestParseProbeMacOS(t *testing.T) {
	fss := ParseProbe(probeOut("@@k 0\n", macK, "@@i 0\n", macI), []string{"/Users/u/p"})
	if len(fss) != 1 {
		t.Fatalf("got %+v", fss)
	}
	f := fss[0]
	if f.Mount != "/System/Volumes/Data" || f.FreeBytes != 20718888*1024 {
		t.Fatalf("got %+v", f)
	}
	if !f.InodesKnown || f.FreeInodes != 207188840 || f.TotalInodes != 5137933+207188840 {
		t.Fatalf("BSD inodes: %+v", f)
	}
}

func TestParseProbeSkipsFailedAndGarbage(t *testing.T) {
	out := "motd noise\n@@k 0\n@@i 0\n@@k 1\nFilesystem 1024-blocks Used Available Capacity Mounted on\n/dev/x 10 5\n"
	if fss := ParseProbe(out, []string{"/a", "/b"}); len(fss) != 0 {
		t.Fatalf("want nothing, got %+v", fss)
	}
}

func TestParseMeasure(t *testing.T) {
	out := "@@u 0\n2048 17\n@@u 2\n12 \n"
	got := ParseMeasure(out, []string{"/a", "/missing", "/c"})
	if got["/a"] != (Usage{Bytes: 2048 * 1024, Files: 17}) {
		t.Fatalf("/a = %+v", got["/a"])
	}
	if _, ok := got["/missing"]; ok {
		t.Fatal("a missing dir has no usage")
	}
	if got["/c"].Bytes != 12*1024 {
		t.Fatalf("/c = %+v", got["/c"])
	}
}

// The scripts run for real on this machine (macOS laptop or Linux CI).
func TestProbeAndMeasureLocal(t *testing.T) {
	ctx := context.Background()
	fsys := rfs.NewLocalFS()
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "sub/c"} {
		p := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, make([]byte, 10000), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	notYet := filepath.Join(dir, "pj1", "jb1")
	fss, err := Probe(ctx, fsys, notYet, dir)
	if err != nil || len(fss) != 1 || len(fss[0].Paths) != 2 {
		t.Fatalf("Probe = %+v, %v", fss, err)
	}
	if fss[0].TotalBytes <= 0 || fss[0].FreeBytes <= 0 {
		t.Fatalf("no sizes: %+v", fss[0])
	}
	use, err := Measure(ctx, fsys, dir, notYet)
	if err != nil {
		t.Fatal(err)
	}
	u := use[dir]
	if u.Bytes < 30000 || u.Files < 5 { // 3 files + 2 dirs
		t.Fatalf("usage = %+v", u)
	}
	if _, ok := use[notYet]; ok {
		t.Fatal("missing dir measured")
	}
}

func TestDiagnose(t *testing.T) {
	cases := []struct {
		name string
		out  string
		kind Kind
		want string
	}{
		{"inodes", "@@m\nNo space left on device\n@@k 0\n" + lustreK + "@@i 0\n" + lustreI, KindInodes, "disk full (inodes): /gs/bs"},
		{"space", "@@m\nNo space left on device\n@@k 0\n" + wrappedK + "@@i 0\n", KindSpace, "disk full (space): /lustre01"},
		{"quota", "@@m\nDisk quota exceeded\n@@k 0\n" + nfsK + "@@i 0\n" + nfsI, KindQuota, "disk full (quota): your quota on /home"},
		{"unclear", "@@m\nENOSPC\n@@k 0\n" + nfsK + "@@i 0\n" + nfsI, KindUnclear, `disk full: "ENOSPC" on /home (now 50.0 GB free`},
		{"no df", "@@m\nNo space left on device\n", KindUnclear, "the task directory's filesystem"},
	}
	for _, c := range cases {
		d, ok := ParseDiagnose(c.out, "/task")
		if !ok || d.Kind != c.kind {
			t.Fatalf("%s: %+v %v", c.name, d, ok)
		}
		if detail := d.Detail(); !strings.Contains(detail, c.want) || !strings.Contains(detail, "runq clean --checkpoints") {
			t.Fatalf("%s: detail = %q", c.name, detail)
		}
	}
	if _, ok := ParseDiagnose("", "/task"); ok {
		t.Fatal("no match must be ok=false")
	}
}

func TestDiagnoseLocalLog(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "task.log")
	body := strings.Repeat("step ok\n", 20000) + "OSError: [Errno 28] No space left on device: '/x/ckpt.pt'\n"
	if err := os.WriteFile(log, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	d, ok, err := Diagnose(context.Background(), rfs.NewLocalFS(), log, dir)
	if err != nil || !ok || d.Match != "No space left on device" || d.FS == nil {
		t.Fatalf("Diagnose = %+v %v %v", d, ok, err)
	}
	if err := os.WriteFile(log, []byte("Traceback: KeyError\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Diagnose(context.Background(), rfs.NewLocalFS(), log, dir); ok || err != nil {
		t.Fatalf("plain failure diagnosed as disk full: %v %v", ok, err)
	}
}
