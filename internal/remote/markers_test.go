package remote

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
)

type readDirErrorFS struct {
	*rfs.LocalFS
	err error
}

func (f *readDirErrorFS) ReadDir(string) ([]fs.DirEntry, error) {
	return nil, f.err
}

func TestScanDoneMarkersPropagatesReadDirFailure(t *testing.T) {
	want := errors.New("injected marker transport failure")
	b := &Backend{
		Cfg: &config.TargetConfig{Name: "lab", DoneDir: "/remote/.runq-done"},
		FS:  &readDirErrorFS{LocalFS: rfs.NewLocalFS(), err: want},
	}

	err := b.ScanDoneMarkers(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("ScanDoneMarkers error = %v, want wrapped %v", err, want)
	}
}

func TestScanDoneMarkersDisabledIsNotSuccessfulObservation(t *testing.T) {
	b := &Backend{
		Cfg: &config.TargetConfig{Name: "lab"},
		FS:  rfs.NewLocalFS(),
	}

	err := b.ScanDoneMarkers(context.Background())
	if !errors.Is(err, ErrMarkerDetectionDisabled) {
		t.Fatalf("ScanDoneMarkers error = %v, want %v", err, ErrMarkerDetectionDisabled)
	}
	if at, _, _ := b.LastContact(); !at.IsZero() {
		t.Fatalf("disabled marker detection recorded contact at %v", at)
	}
}

func TestScanDoneMarkersPropagatesOwnershipLookupFailure(t *testing.T) {
	doneDir := t.TempDir()
	markerID := "task-with-failed-lookup"
	if err := os.WriteFile(filepath.Join(doneDir, markerID), nil, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := &Backend{
		Cfg:   &config.TargetConfig{Name: "lab", DoneDir: doneDir},
		Store: s,
		FS:    rfs.NewLocalFS(),
	}

	err = b.ScanDoneMarkers(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ScanDoneMarkers error = %v, want wrapped context cancellation", err)
	}
	if !strings.Contains(err.Error(), markerID) {
		t.Fatalf("ScanDoneMarkers error = %q, want marker id", err)
	}
}

func TestScanDoneMarkersFinalIngestsShortTask(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	taskDir := t.TempDir()
	jobID, taskID := seedTask(t, st, taskDir, "running", SourceScheduler)
	writeFile(t, filepath.Join(taskDir, statusFileName),
		`{"status":"success","exit_code":0,"finished_at":1730000000}`)
	writeFile(t, filepath.Join(taskDir, "results.jsonl"),
		"{\"ts\":1,\"axes\":{\"step\":1},\"metrics\":{\"score\":9}}\n")
	doneDir := t.TempDir()
	writeFile(t, filepath.Join(doneDir, taskID), "")
	loaded, err := st.GetTask(ctx, taskID)
	if err != nil || loaded == nil {
		t.Fatalf("GetTask: %v", err)
	}

	b := &Backend{
		Cfg:   &config.TargetConfig{Name: loaded.Target, DoneDir: doneDir},
		Store: st,
		FS:    rfs.NewLocalFS(),
	}
	if err := b.ScanDoneMarkers(ctx); err != nil {
		t.Fatalf("ScanDoneMarkers: %v", err)
	}
	rows, err := st.ListResultRecords(ctx, jobID)
	if err != nil {
		t.Fatalf("ListResultRecords: %v", err)
	}
	if len(rows) != 1 || rows[0].MetricsJSON != `{"score":9}` {
		mark, markErr := st.GetFileIngestMark(ctx, taskID, store.IngestFileResults)
		t.Fatalf("short task results were not ingested before completion: rows=%+v mark=%+v markErr=%v", rows, mark, markErr)
	}
	got, err := st.GetTask(ctx, taskID)
	if err != nil || got == nil || got.Status != "success" {
		t.Fatalf("completed task = %#v, %v; want success", got, err)
	}
	if _, err := os.Stat(filepath.Join(doneDir, taskID)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("settled marker still exists: %v", err)
	}
}

func TestHasInFlightIsGenerationScoped(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.InsertJob(ctx, &store.JobRow{
		ID: "job", ProjectName: "project", ConfigJSON: "{}", Status: "running",
		TotalTasks: 1, Target: "lab", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertTask(ctx, &store.TaskRow{
		ID: "task", JobID: "job", ProjectName: "project", Command: "true",
		ParamsJSON: "{}", Status: "pending", ExternalID: "external-1",
		Target: "lab", TargetGeneration: "old", EnqueuedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertRetiredGeneration(ctx, &store.TargetGenerationRow{
		Target: "lab", Generation: "old", ConfigJSON: "{}", Reason: "changed", RetiredAt: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	activeScope := store.NewLaneScope("lab", "new")
	active := &Backend{Cfg: &config.TargetConfig{Name: "lab"}, Store: s, Scope: activeScope}
	if got, err := active.HasInFlight(ctx); err != nil || got {
		t.Fatalf("active lane HasInFlight = %v, %v; want false", got, err)
	}

	retiringScope := store.NewLaneScope("lab", "old")
	retiringScope.MarkRetiring()
	retiring := &Backend{Cfg: &config.TargetConfig{Name: "lab"}, Store: s, Scope: retiringScope}
	if got, err := retiring.HasInFlight(ctx); err != nil || !got {
		t.Fatalf("retiring lane HasInFlight = %v, %v; want true", got, err)
	}
}

func TestTerminalExternalIDDoesNotKeepSensorAwake(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.InsertJob(ctx, &store.JobRow{ID: "done", ProjectName: "project", ConfigJSON: "{}", Status: "success", Target: "lab", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err = s.InsertTask(ctx, &store.TaskRow{ID: "done-1", JobID: "done", ProjectName: "project", Command: "true", ParamsJSON: "{}", Status: "success", ExternalID: "external-1", Target: "lab", EnqueuedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	b := &Backend{Cfg: &config.TargetConfig{Name: "lab"}, Store: s}
	if active, err := b.HasInFlight(ctx); err != nil || active {
		t.Fatalf("terminal external ID caused background polling: %v %v", active, err)
	}
}
