package gitea

import (
	"context"

	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

// invokeWrite marks the non-idempotent boundary immediately before invocation.
// Every error after this point is intentionally content-free and uncertain.
func invokeWrite[T any](ctx context.Context, invoke func() (T, error), validate func(T) error) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	value, err := invoke()
	if err != nil {
		return zero, rpcstatus.ErrWriteOutcomeUnknown
	}
	if err := validate(value); err != nil {
		return zero, rpcstatus.ErrWriteOutcomeUnknown
	}
	return value, nil
}
