package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// An id prefix matches with its case, as the dashboard matches it.
func TestIDPrefixKeepsCase(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.DB().Exec(`INSERT INTO projects(name, config_json) VALUES('p','{}')`); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertJob(ctx, &JobRow{ID: "jb65aa", ProjectName: "p", ConfigJSON: "{}", Status: "done", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if j, err := st.ResolveJobRef(ctx, "jb65"); err != nil || j.ID != "jb65aa" {
		t.Fatalf("jb65: %v %v", j, err)
	}
	if _, err := st.ResolveJobRef(ctx, "JB65"); !errors.Is(err, ErrRefNotFound) {
		t.Fatalf("JB65 must not match jb65aa: %v", err)
	}
}
