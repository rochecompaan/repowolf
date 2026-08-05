// Package audit emits a deliberately narrow, safe operational event schema.
package audit

import "time"

type Outcome string

const (
	OutcomeAccepted  Outcome = "accepted"
	OutcomeDenied    Outcome = "denied"
	OutcomeCompleted Outcome = "completed"
	OutcomeCancelled Outcome = "cancelled"
	OutcomeFailed    Outcome = "failed"
)

// Event contains only metadata approved for the audit stream. Request bodies,
// process details, credentials, and provider output have no representation.
type Event struct {
	Timestamp   time.Time `json:"timestamp"`
	RequestID   string    `json:"request_id,omitempty"`
	Principal   string    `json:"principal,omitempty"`
	Provider    string    `json:"provider,omitempty"`
	Repository  string    `json:"repository,omitempty"`
	Operation   string    `json:"operation"`
	Outcome     Outcome   `json:"outcome"`
	Reason      string    `json:"reason,omitempty"`
	DurationMS  int64     `json:"duration_ms,omitempty"`
	InputBytes  int64     `json:"input_bytes,omitempty"`
	OutputBytes int64     `json:"output_bytes,omitempty"`
	Refs        []string  `json:"refs,omitempty"`
	UpdateCount int       `json:"update_count,omitempty"`
}
