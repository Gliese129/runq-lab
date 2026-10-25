package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRenameJobByPrefix(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for _, id := range []string{"jbaa000001", "jbaa000002", "jbbb000001"} {
		if err := s.InsertJob(ctx, &JobRow{ID: id, ProjectName: "p", Name: "old", ConfigJSON: "{}", Status: "done", CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	id, err := s.RenameJob(ctx, "jbbb", "A1 原始基线")
	if err != nil || id != "jbbb000001" {
		t.Fatalf("unique prefix should resolve: %q %v", id, err)
	}
	if j, _ := s.GetJob(ctx, id); j == nil || j.Name != "A1 原始基线" {
		t.Fatalf("name not stored: %+v", j)
	}
	if _, err := s.RenameJob(ctx, "jbaa", "x"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous prefix must list candidates, got %v", err)
	}
	if _, err := s.RenameJob(ctx, "jbbb000001", "  "); err == nil {
		t.Fatal("empty name must be rejected")
	}
}
