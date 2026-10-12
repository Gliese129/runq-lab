package dashboard

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"

	"strings"
	"time"

	"github.com/gliese129/runq-lab/internal/backend"

	"github.com/gliese129/runq-lab/internal/rfs"
)

// fs group — /targets/{name}/fs/* (spec §5.2, #44): the browser operates
// on the ADDRESSED TARGET's filesystem via rfs.FS (LocalFS/SSHFS — same
// code path, the handler doesn't know the difference).
//
// Protection is SOFT: path normalization only, no fence. The daemon runs
// with the user's own permissions and the OS is the real boundary
// (philosophy C4) — HPC data disks (/scratch, /data) outside home are a
// first-class use case, not an escape.

// targetFS resolves the addressed target's filesystem.
func (s *Server) targetFS(name string) (rfs.FS, error) {
	if tf, ok := s.backend.(interface {
		TargetFS(name string) (rfs.FS, error)
	}); ok {
		return tf.TargetFS(name)
	}
	return rfs.NewLocalFS(), nil
}

// isLocalFS reports whether the FS is the daemon's own disk — path
// normalization and env detection differ (Windows local paths; remote is
// always POSIX).
func isLocalFS(fsys rfs.FS) bool {
	_, ok := fsys.(*rfs.LocalFS)
	return ok
}

// cleanPath normalizes (soft protection: normalization is ALL it does).
// Empty path resolves to the FS's start dir (local home / remote home via
// the optional Home() interface).
func cleanPath(fsys rfs.FS, p string) string {
	if p == "" {
		return startDir(fsys)
	}
	if isLocalFS(fsys) {
		if abs, err := filepath.Abs(p); err == nil {
			return filepath.Clean(abs)
		}
		return filepath.Clean(p)
	}
	return path.Clean(p) // remote: POSIX rules, relative = sftp home-relative
}

// startDir is the browser's default entry point: the user's home on
// either side.
func startDir(fsys rfs.FS) string {
	if h, ok := fsys.(interface{ Home() (string, error) }); ok {
		if home, err := h.Home(); err == nil && home != "" {
			return home
		}
	}
	if isLocalFS(fsys) {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	return "."
}

// joinPath joins with the FS's path rules.
func joinPath(fsys rfs.FS, dir, name string) string {
	if isLocalFS(fsys) {
		return filepath.Join(dir, name)
	}
	return path.Join(dir, name)
}

func (s *Server) handleFSList(w http.ResponseWriter, r *http.Request) {
	fsys, err := s.targetFS(r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}
	dir := cleanPath(fsys, r.URL.Query().Get("path"))

	entries, err := fsys.ReadDir(dir)
	if err != nil {
		writeErr(w, http.StatusBadRequest, backend.CodeBadRequest, err.Error())
		return
	}
	out := make([]backend.FSEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		isDir := entry.IsDir()
		// Symlinks: DirEntry reports the LINK's type, so a symlinked
		// directory (~/fast -> /scratch/...) would show as a file and become
		// unenterable. Stat the target instead (rfs.Stat follows links on
		// both sides). Broken links stay files.
		if !isDir && info.Mode()&os.ModeSymlink != 0 {
			if target, terr := fsys.Stat(joinPath(fsys, dir, entry.Name())); terr == nil {
				isDir = target.IsDir()
			}
		}
		out = append(out, backend.FSEntry{
			Name:  entry.Name(),
			Path:  joinPath(fsys, dir, entry.Name()),
			IsDir: isDir,
			Size:  info.Size(),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	env := envelope(out)
	now := time.Now().Unix() // live readdir over FS: now IS the read time
	stale := false
	env.RefreshedAt, env.Stale = &now, &stale
	writeJSON(w, http.StatusOK, env)
}

// Glob resolution bounds. The default keeps a careless `**` from filling a
// picker with thousands of chips; the max is the hard ceiling a caller may
// ask for. Truncation is REPORTED (see the response's truncated flag) — a
// partial list presented as complete would silently shrink a sweep.
const (
	defaultGlobLimit = 500
	maxGlobLimit     = 2000
)
