package identity

import "context"

// Method identifies the mechanism that authenticated a request.
type Method string

// MethodAPIKey identifies API-key authentication.
const MethodAPIKey Method = "api_key"

type contextKey struct{}

// Identity describes the authenticated subject associated with a request.
type Identity struct {
	Subject string
	Method  Method
}

// NewContext returns a child context carrying identity.
func NewContext(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, identity)
}

// FromContext returns the authenticated identity stored in ctx.
func FromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(contextKey{}).(Identity)
	return identity, ok
}
