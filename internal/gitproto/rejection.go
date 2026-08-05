package gitproto

import (
	"errors"

	"github.com/rochecompaan/repowolf/internal/policy"
)

// rejectedUpdatesError preserves only parsed update metadata after policy
// denial. It deliberately carries no forwardable receive-pack bytes.
type rejectedUpdatesError struct {
	updates []policy.Update
}

func (err *rejectedUpdatesError) Error() string { return "receive-pack update policy denied" }
func (err *rejectedUpdatesError) Unwrap() error { return policy.ErrRefPolicy }

func newRejectedUpdatesError(updates []policy.Update) error {
	return &rejectedUpdatesError{updates: append([]policy.Update(nil), updates...)}
}

// RejectedUpdates returns a defensive copy of safely parsed updates carried by
// a receive-pack policy denial. Other errors return nil.
func RejectedUpdates(err error) []policy.Update {
	var rejected *rejectedUpdatesError
	if !errors.As(err, &rejected) {
		return nil
	}
	return append([]policy.Update(nil), rejected.updates...)
}
