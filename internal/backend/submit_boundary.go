package backend

import "context"

type submitBoundaryKey struct{}

// WithSubmitTargetBoundary binds the final resolution to a remote forward.
// It is request context, never an editable field in the JSON body.
func WithSubmitTargetBoundary(ctx context.Context, target string) context.Context {
	return context.WithValue(ctx, submitBoundaryKey{}, target)
}
