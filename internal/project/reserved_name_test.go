package project

import (
	"context"
	"github.com/gliese129/runq-lab/internal/store"
	"testing"
)

func TestFormerFormRouteNameAvailable(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := NewRegistry(st.DB())
	if err := r.Add(context.Background(), Config{ProjectName: "new", WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := r.Rename(context.Background(), "new", "experiment"); err != nil {
		t.Fatal(err)
	}
	if err := r.Rename(context.Background(), "experiment", "new"); err != nil {
		t.Fatal(err)
	}
}
