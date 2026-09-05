// Package app assembles the immutable service runtime in fail-closed order.
package app

import (
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/credentials"
	"github.com/rochecompaan/repowolf/internal/gitservice"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/runner"
	"github.com/rochecompaan/repowolf/internal/server"
	"github.com/rochecompaan/repowolf/internal/tlsconfig"
)

const shutdownGracePeriod = 30 * time.Second

// Runtime is the immutable startup snapshot used for the process lifetime.
type Runtime struct {
	Config         config.Config
	Tokens         *auth.Index
	TLSConfig      *tls.Config
	Tools          runner.Toolset
	Policy         *policy.Snapshot
	SSHEnvironment []string
	GitHub         server.GitHubExecutor
	Git            *gitservice.Service
	Server         *server.Server
	providers      map[string]providerInstance
}

// NewRuntime validates and pins every runtime dependency before readiness.
func NewRuntime(configPath string, auditOutput io.Writer) (*Runtime, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("load configuration: %w", err)
	}
	credentialSnapshot, err := credentials.Load(cfg, os.LookupEnv)
	if err != nil {
		return nil, fmt.Errorf("load credentials: %w", err)
	}
	tlsConfig, err := tlsconfig.LoadServer(cfg.TLS.Certificate, cfg.TLS.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("load TLS: %w", err)
	}
	githubRequired := hasProviderKind(cfg.Providers, config.ProviderGitHub)
	tools, err := runner.ResolveTools(cfg.Tools, githubRequired, runner.LookPath)
	if err != nil {
		return nil, fmt.Errorf("resolve tools: %w", err)
	}
	policySnapshot, err := policy.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("build policy: %w", err)
	}
	tokenFreeEnvironment := runner.TokenFreeEnvironment(os.Environ(), credentialSnapshot.EnvironmentNames())
	providerRunner := &runner.Runner{}
	instances, err := buildProviderInstances(cfg, credentialSnapshot, tools, tokenFreeEnvironment, providerRunner)
	if err != nil {
		return nil, fmt.Errorf("create provider instances: %w", err)
	}
	githubExecutor := buildGitHubExecutor(instances)

	auditWriter := audit.NewWriter(auditOutput)
	git, err := gitservice.New(gitservice.Options{
		Policy: policySnapshot, SSHPath: tools.SSH, Environment: tokenFreeEnvironment,
		Limits: cfg.Limits, Runner: providerRunner, Audit: auditWriter,
	})
	if err != nil {
		return nil, fmt.Errorf("create Git service: %w", err)
	}
	var githubPolicy *policy.Snapshot
	var githubService server.GitHubExecutor
	if githubExecutor != nil {
		githubPolicy = policySnapshot
		githubService = githubExecutor
	}
	grpcServer, err := server.New(server.Options{
		TLSConfig: tlsConfig, Tokens: credentialSnapshot.AuthIndex(), AuditWriter: auditWriter,
		MaxConcurrentRequests:             cfg.Limits.MaxConcurrentRequests,
		MaxConcurrentRequestsPerPrincipal: cfg.Limits.MaxConcurrentRequestsPerPrincipal,
		OperationTimeout:                  cfg.Limits.OperationTimeout,
		GracePeriod:                       shutdownGracePeriod,
		Policy:                            githubPolicy,
		GitHub:                            githubService,
		Git:                               git,
		Cleanup:                           providerRunner.Cleanup,
	})
	if err != nil {
		return nil, fmt.Errorf("create server: %w", err)
	}
	runtime := &Runtime{
		Config:         cfg,
		Tokens:         credentialSnapshot.AuthIndex(),
		TLSConfig:      tlsConfig,
		Tools:          tools,
		Policy:         policySnapshot,
		SSHEnvironment: append([]string(nil), tokenFreeEnvironment...),
		GitHub:         githubService,
		Git:            git,
		Server:         grpcServer,
		providers:      instances,
	}
	runtime.Server.MarkReady()
	return runtime, nil
}

func hasProviderKind(providers map[string]config.Provider, kind config.ProviderKind) bool {
	for _, provider := range providers {
		if provider.Kind == kind {
			return true
		}
	}
	return false
}
