// Package app assembles the immutable service runtime in fail-closed order.
package app

import (
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/gitservice"
	"github.com/rochecompaan/repowolf/internal/policy"
	providergithub "github.com/rochecompaan/repowolf/internal/provider/github"
	"github.com/rochecompaan/repowolf/internal/runner"
	"github.com/rochecompaan/repowolf/internal/server"
	"github.com/rochecompaan/repowolf/internal/tlsconfig"
)

const shutdownGracePeriod = 30 * time.Second

// Runtime is the immutable startup snapshot used for the process lifetime.
type Runtime struct {
	Config              config.Config
	Tokens              *auth.Index
	TLSConfig           *tls.Config
	Tools               runner.Toolset
	Policy              *policy.Snapshot
	ProviderEnvironment []string
	GitHub              *providergithub.Adapter
	Git                 *gitservice.Service
	Server              *server.Server
}

// NewRuntime validates and pins every runtime dependency before readiness.
func NewRuntime(configPath string, auditOutput io.Writer) (*Runtime, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("load configuration: %w", err)
	}
	tokens, err := auth.Load(cfg.Principals, os.LookupEnv)
	if err != nil {
		return nil, fmt.Errorf("load authentication: %w", err)
	}
	tlsConfig, err := tlsconfig.LoadServer(cfg.TLS.Certificate, cfg.TLS.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("load TLS: %w", err)
	}
	tools, err := runner.ResolveTools(cfg.Tools, runner.LookPath)
	if err != nil {
		return nil, fmt.Errorf("resolve tools: %w", err)
	}
	policySnapshot, err := policy.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("build policy: %w", err)
	}
	providerEnvironment := runner.ProviderEnvironment(os.Environ(), tokenEnvironmentNames(cfg))
	providerRunner := &runner.Runner{}
	githubAdapter, err := providergithub.New(providergithub.AdapterOptions{
		Path: tools.GH, Environment: providerEnvironment,
		Timeout: cfg.Limits.OperationTimeout, Caller: providerRunner,
	})
	if err != nil {
		return nil, fmt.Errorf("create GitHub adapter: %w", err)
	}
	auditWriter := audit.NewWriter(auditOutput)
	git, err := gitservice.New(gitservice.Options{
		Policy: policySnapshot, SSHPath: tools.SSH, Environment: providerEnvironment,
		Limits: cfg.Limits, Runner: providerRunner, Audit: auditWriter,
	})
	if err != nil {
		return nil, fmt.Errorf("create Git service: %w", err)
	}
	grpcServer, err := server.New(server.Options{
		TLSConfig: tlsConfig, Tokens: tokens, AuditWriter: auditWriter,
		MaxConcurrentRequests:             cfg.Limits.MaxConcurrentRequests,
		MaxConcurrentRequestsPerPrincipal: cfg.Limits.MaxConcurrentRequestsPerPrincipal,
		OperationTimeout:                  cfg.Limits.OperationTimeout, GracePeriod: shutdownGracePeriod,
		Policy: policySnapshot, GitHub: githubAdapter, Git: git, Cleanup: providerRunner.Cleanup,
	})
	if err != nil {
		return nil, fmt.Errorf("create server: %w", err)
	}
	runtime := &Runtime{
		Config: cfg, Tokens: tokens, TLSConfig: tlsConfig, Tools: tools,
		Policy: policySnapshot, ProviderEnvironment: providerEnvironment, GitHub: githubAdapter, Git: git, Server: grpcServer,
	}
	runtime.Server.MarkReady()
	return runtime, nil
}

func tokenEnvironmentNames(cfg config.Config) []string {
	names := make([]string, 0)
	for _, principal := range cfg.Principals {
		names = append(names, principal.TokenEnvs...)
	}
	sort.Strings(names)
	return names
}
