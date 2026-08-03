package auth

import "context"

type principalContextKey struct{}

// WithPrincipal returns a child context that carries principal.
func WithPrincipal(ctx context.Context, principal string) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// Principal returns the authenticated principal carried by ctx.
func Principal(ctx context.Context) (string, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(string)
	return principal, ok
}
