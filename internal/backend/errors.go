package backend

import (
	"errors"
	"strings"
)

var (
	ErrForbidden          = errors.New("forbidden")
	ErrNotSupported       = errors.New("not supported")
	ErrNotFound           = errors.New("not found")
	ErrNoTargetConfigured = errors.New("no target configured")
	// ErrFilesElsewhere: the version's files are on a filesystem no
	// configured lane reads (its target moved host).
	ErrFilesElsewhere = errors.New("files on another filesystem")
)

// IsNotFound reports whether err represents a "not found" condition.
// Prefers errors.Is for sentinel-based checks; falls back to string
// matching for errors originating outside this package.
func IsNotFound(err error) bool {
	if errors.Is(err, ErrNotFound) {
		return true
	}
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not found")
}
