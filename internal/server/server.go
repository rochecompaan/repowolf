package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
)

// Server is a single-use TLS gRPC server with bounded graceful shutdown.
type Server struct {
	grpc        *grpc.Server
	health      *health.Server
	audit       audit.Sink
	tokens      *auth.Index
	gracePeriod time.Duration
	cleanup     func() error

	stopping atomic.Bool
	ready    atomic.Bool
	started  atomic.Bool
	activeMu sync.Mutex
	active   map[uint64]context.CancelFunc
	nextID   uint64
	limits   concurrencyLimits
	timeout  time.Duration
}

// New validates immutable dependencies and constructs a not-ready server.
func New(options Options) (*Server, error) {
	if err := validateOptions(options); err != nil {
		return nil, err
	}
	service := &Server{
		audit: options.AuditWriter, tokens: options.Tokens, gracePeriod: options.GracePeriod,
		cleanup: options.Cleanup, active: make(map[uint64]context.CancelFunc),
		timeout: options.OperationTimeout,
		limits:  newConcurrencyLimits(options.MaxConcurrentRequests, options.MaxConcurrentRequestsPerPrincipal),
	}
	service.health = health.NewServer()
	service.health.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	service.grpc = grpc.NewServer(
		grpc.Creds(credentials.NewTLS(options.TLSConfig.Clone())),
		grpc.MaxRecvMsgSize(messageLimitBytes),
		grpc.MaxSendMsgSize(messageLimitBytes),
		grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionIdle: 30 * time.Minute, Time: 2 * time.Hour, Timeout: 20 * time.Second}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: time.Minute, PermitWithoutStream: false}),
		grpc.ChainUnaryInterceptor(service.unaryInterceptors()...),
		grpc.ChainStreamInterceptor(service.streamInterceptors()...),
	)
	grpc_health_v1.RegisterHealthServer(service.grpc, service.health)
	if options.Policy != nil {
		repowolfv1.RegisterGitHubServiceServer(service.grpc, newGitHubService(options.Policy, options.GitHub, options.AuditWriter))
	}
	if options.Git != nil {
		repowolfv1.RegisterGitServiceServer(service.grpc, options.Git)
	}
	if options.Register != nil {
		options.Register(service.grpc)
	}
	return service, nil
}

// MarkReady publishes serving state after the complete runtime snapshot exists.
func (service *Server) MarkReady() {
	if service == nil || service.stopping.Load() {
		return
	}
	service.ready.Store(true)
	service.health.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
}

func validateOptions(options Options) error {
	if options.TLSConfig == nil || len(options.TLSConfig.Certificates) == 0 || options.TLSConfig.MinVersion < tls.VersionTLS13 {
		return fmt.Errorf("TLS 1.3 server configuration is required")
	}
	if options.Tokens == nil {
		return fmt.Errorf("authentication index is required")
	}
	if options.AuditWriter == nil {
		return fmt.Errorf("audit writer is required")
	}
	if options.MaxConcurrentRequests <= 0 || options.MaxConcurrentRequestsPerPrincipal <= 0 || options.MaxConcurrentRequestsPerPrincipal > options.MaxConcurrentRequests {
		return fmt.Errorf("invalid concurrency limits")
	}
	if options.OperationTimeout <= 0 || options.GracePeriod <= 0 {
		return fmt.Errorf("invalid server time limits")
	}
	if (options.Policy == nil) != (options.GitHub == nil) {
		return fmt.Errorf("incomplete GitHub service dependencies")
	}
	return nil
}
