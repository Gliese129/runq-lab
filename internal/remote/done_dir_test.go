package remote

import (
	"testing"

	"github.com/gliese129/runq-lab/internal/config"
)

// The done-marker directory sits under the storage root: data_path, or the
// pre-rc6 workspace; an explicit done_dir wins.
func TestDoneDirUsesTheStorageRoot(t *testing.T) {
	for _, c := range []struct {
		cfg  config.TargetConfig
		want string
	}{
		{config.TargetConfig{DataPath: "/data"}, "/data/" + doneDirName},
		{config.TargetConfig{Workspace: "/ws"}, "/ws/" + doneDirName},
		{config.TargetConfig{DataPath: "/data", DoneDir: "/done"}, "/done"},
		{config.TargetConfig{}, ""},
	} {
		b := &Backend{Cfg: &c.cfg}
		if got := b.doneDir(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.cfg, got, c.want)
		}
	}
}
