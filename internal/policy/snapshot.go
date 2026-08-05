// Package policy resolves repository authority and validates Git push policy.
package policy

import (
	"errors"
	"fmt"

	"github.com/rochecompaan/repowolf/internal/config"
)

var (
	// ErrDenied does not distinguish unknown from unauthorized repositories.
	ErrDenied = errors.New("repository access denied")
	// ErrRepository reports an invalid trusted repository reference at startup.
	ErrRepository = errors.New("invalid repository")
	// ErrRefPolicy reports a syntactically invalid or policy-denied ref update.
	ErrRefPolicy = errors.New("ref policy violation")
)

// Snapshot is an immutable, indexed copy of the configured repository policy.
type Snapshot struct {
	providers    map[string]config.Provider
	repositories map[string]config.Repository
	grants       map[string]map[string]map[config.Capability]struct{}
}

// ResolvedRepository is a repository and provider selected from trusted policy.
type ResolvedRepository struct {
	ID         string
	Repository config.Repository
	Provider   config.Provider
}

// New indexes a validated configuration and copies all mutable policy values.
func New(cfg config.Config) (*Snapshot, error) {
	snapshot := &Snapshot{
		providers:    make(map[string]config.Provider, len(cfg.Providers)),
		repositories: make(map[string]config.Repository, len(cfg.Repositories)),
		grants:       make(map[string]map[string]map[config.Capability]struct{}, len(cfg.Principals)),
	}
	for id, provider := range cfg.Providers {
		snapshot.providers[id] = provider
	}
	for id, repository := range cfg.Repositories {
		if _, ok := snapshot.providers[repository.Provider]; !ok {
			return nil, fmt.Errorf("%w: repository %q references provider %q", ErrRepository, id, repository.Provider)
		}
		snapshot.repositories[id] = copyRepository(repository)
	}
	for principal, configured := range cfg.Principals {
		grants := make(map[string]map[config.Capability]struct{}, len(configured.Grants))
		for _, grant := range configured.Grants {
			if _, ok := snapshot.repositories[grant.Repository]; !ok {
				return nil, fmt.Errorf("%w: principal %q references repository %q", ErrRepository, principal, grant.Repository)
			}
			capabilities := make(map[config.Capability]struct{}, len(grant.Capabilities))
			for _, capability := range grant.Capabilities {
				capabilities[capability] = struct{}{}
			}
			grants[grant.Repository] = capabilities
		}
		snapshot.grants[principal] = grants
	}
	return snapshot, nil
}

func copyRepository(repository config.Repository) config.Repository {
	repository.Git.DenyRefs = append([]string(nil), repository.Git.DenyRefs...)
	return repository
}
