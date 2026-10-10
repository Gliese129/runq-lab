package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// accessLogMaxBytes caps access.log before it is rotated to access.log.1.
// The dashboard polls, so a busy week is tens of MB; one rotated generation
// is enough for usage audits ("which pages/endpoints are actually used").
const accessLogMaxBytes = 32 << 20

// accessEntry is one JSON line. route is the mux pattern (ids stay out of
// the log), client separates the browser from the CLI on the same mux.
type accessEntry struct {
	TS     string `json:"ts"`
	Client string `json:"client"` // "web" | "cli"
	Method string `json:"method"`
	Route  string `json:"route"`
	Status int    `json:"status"`
	MS     int64  `json:"ms"`
	Bytes  int64  `json:"bytes"`
}

// RotatingFile is an append-only log file that rotates once it exceeds
// accessLogMaxBytes. Safe for concurrent writers.
type RotatingFile struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

// OpenRotatingFile opens (or creates) path for appending.
func OpenRotatingFile(path string) (*RotatingFile, error) {
	rf := &RotatingFile{path: path}
	if err := rf.open(); err != nil {
		return nil, err
	}
	return rf, nil
}

func (rf *RotatingFile) open() error {
	f, err := os.OpenFile(rf.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	rf.f, rf.size = f, info.Size()
	return nil
}

func (rf *RotatingFile) Write(p []byte) (int, error) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.f == nil {
		return 0, os.ErrClosed
	}
	if rf.size+int64(len(p)) > accessLogMaxBytes {
		_ = rf.f.Close()
		_ = os.Rename(rf.path, rf.path+".1")
		if err := rf.open(); err != nil {
			rf.f = nil
			return 0, err
		}
	}
	n, err := rf.f.Write(p)
	rf.size += int64(n)
	return n, err
}

func (rf *RotatingFile) Close() error {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.f == nil {
		return nil
	}
	err := rf.f.Close()
	rf.f = nil
	return err
}

// SetAccessLog enables per-request access logging for /api/ routes. nil
// disables it. Call before serving.
func (s *Server) SetAccessLog(w io.Writer) { s.accessLog = w }

// accessLogMiddleware records one line per /api/ request after it finishes
// (SSE streams are logged when they close, so ms is the stream lifetime).
// Static assets are skipped: the question this log answers is which API
// surfaces the dashboard and CLI actually exercise, and how slow they are.
func accessLogMiddleware(w io.Writer, next http.Handler) http.Handler {
	if w == nil {
		return next
	}
	var mu sync.Mutex
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(rw, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: rw, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		route := r.Pattern // set in place by ServeMux; keeps ids out of the log
		if route == "" {
			route = r.Method + " " + r.URL.Path
		}
		client := "web"
		if r.Header.Get("X-Runq-Version") != "" {
			client = "cli"
		}
		line, err := json.Marshal(accessEntry{
			TS:     start.Format(time.RFC3339),
			Client: client,
			Method: r.Method,
			Route:  route,
			Status: rec.status,
			MS:     time.Since(start).Milliseconds(),
			Bytes:  rec.bytes,
		})
		if err != nil {
			return
		}
		mu.Lock()
		_, _ = w.Write(append(line, '\n'))
		mu.Unlock()
	})
}

// statusRecorder captures status and body size. It forwards Flush and
// exposes Unwrap so SSE handlers and http.ResponseController keep working.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status, r.wroteHeader = code, true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	r.wroteHeader = true
	n, err := r.ResponseWriter.Write(p)
	r.bytes += int64(n)
	return n, err
}

func (r *statusRecorder) Flush() {
	r.wroteHeader = true // a flush commits the implicit 200
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
