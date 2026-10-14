package proxytarget

import (
	"context"
	"net/url"
)

type contextKey struct{}

// WithTarget returns a child context containing the selected upstream target.
func WithTarget(ctx context.Context, target url.URL) context.Context {
	return context.WithValue(ctx, contextKey{}, target)
}

// FromContext returns the selected upstream target stored in ctx and reports
// whether it was present.
func FromContext(ctx context.Context) (url.URL, bool) {
	value, ok := ctx.Value(contextKey{}).(url.URL)
	return value, ok
}
