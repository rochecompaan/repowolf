package auth

import "context"

type principalContextKey struct{}
type requestIDContextKey struct{}

// WithPrincipal returns a child context that carries principal.
func WithPrincipal(ctx context.Context, principal string) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// Principal returns the authenticated principal carried by ctx.
func Principal(ctx context.Context) (string, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(string)
	return principal, ok
}

// WithRequestID returns a child context that carries a safe request identifier.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

// RequestID returns the request identifier carried by ctx.
func RequestID(ctx context.Context) (string, bool) {
	requestID, ok := ctx.Value(requestIDContextKey{}).(string)
	return requestID, ok
}
