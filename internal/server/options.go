// Package server owns the authenticated TLS gRPC transport and its lifecycle.
package server

import (
	"crypto/tls"
	"time"

	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"google.golang.org/grpc"
)

const messageLimitBytes = 1_048_576

// Options are immutable service dependencies assembled before listener bind.
type Options struct {
	TLSConfig                         *tls.Config
	Tokens                            *auth.Index
	AuditWriter                       audit.Sink
	MaxConcurrentRequests             int
	MaxConcurrentRequestsPerPrincipal int
	OperationTimeout                  time.Duration
	GracePeriod                       time.Duration
	Register                          func(grpc.ServiceRegistrar)
	Cleanup                           func() error
}
