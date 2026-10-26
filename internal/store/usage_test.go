package store

import (
	"context"
	"testing"
	"time"
)

func TestJobUsageUpsertKeepsLargerAndFootprint(t *testing.T) {
	s := batchTestStore(t)
	ctx := context.Background()
	insertBatchTestJob(t, s, "jbsmall001", "success", "success")
	insertBatchTestJob(t, s, "jbbig00001", "success", "failed")
	if err := s.InsertJob(ctx, &JobRow{ID: "jbother001", ProjectName: "q", Name: "x", ConfigJSON: "{}", Status: "done", Target: "c", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if u, err := s.GetJobUsage(ctx, "jbsmall001"); err != nil || u != nil {
		t.Fatalf("unmeasured = %+v %v", u, err)
	}
	if _, ok, err := s.ProjectTaskFootprint(ctx, "p"); ok || err != nil {
		t.Fatalf("no measurements yet: ok=%v err=%v", ok, err)
	}
	now := time.Now()
	for _, u := range []JobUsage{
		{JobID: "jbsmall001", Bytes: 2000, Files: 900, TaskCount: 2, MeasuredAt: now},
		{JobID: "jbbig00001", Bytes: 10000, Files: 40, TaskCount: 2, MeasuredAt: now},
		{JobID: "jbbig00001", Bytes: 100, Files: 10, TaskCount: 2, MeasuredAt: now}, // after clean --checkpoints
		{JobID: "jbother001", Bytes: 1 << 40, Files: 1 << 30, TaskCount: 1, MeasuredAt: now},
	} {
		if err := s.UpsertJobUsage(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	if u, _ := s.GetJobUsage(ctx, "jbbig00001"); u == nil || u.Bytes != 10000 || u.Files != 40 {
		t.Fatalf("re-measure must keep the larger footprint: %+v", u)
	}
	fp, ok, err := s.ProjectTaskFootprint(ctx, "p")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	want := TaskFootprint{BytesPerTask: 5000, BytesJobID: "jbbig00001", FilesPerTask: 450, FilesJobID: "jbsmall001", Jobs: 2}
	if fp != want {
		t.Fatalf("footprint = %+v, want %+v", fp, want)
	}
	if _, err := s.DB().Exec(`DELETE FROM jobs WHERE id = 'jbother001'`); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.GetJobUsage(ctx, "jbother001"); u != nil {
		t.Fatal("usage must go with its job")
	}
}
