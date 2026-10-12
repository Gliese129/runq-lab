package workspace

// InspectPyramid moved here from pyramid.go when `runq metrics-index inspect`
// was removed: production never reads pyramid headers this way, but the
// pyramid tests use it to look inside the built index.

import (
	"context"
	"sort"

	"github.com/gliese129/runq-lab/internal/rfs"
)

// InspectPyramid parses only the header and reports every key's shape.
func InspectPyramid(ctx context.Context, fsys rfs.FS, taskDir string) ([]PyramidInfo, error) {
	if fsys == nil {
		fsys = rfs.NewLocalFS()
	}
	f, err := fsys.Open(PyramidPath(taskDir))
	if err != nil {
		return nil, ErrPyramidNotBuilt
	}
	defer f.Close()
	metas, err := parseHeader(f)
	if err != nil {
		return nil, err
	}
	out := make([]PyramidInfo, 0, len(metas))
	for key, m := range metas {
		out = append(out, PyramidInfo{Key: key, PointCount: m.pointCount, LayerCounts: m.layerCounts})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}
