package policy

import (
	"strings"

	"github.com/rochecompaan/repowolf/internal/config"
)

// Selector describes optional exact repository identity fields supplied by a client.
type Selector struct {
	Kind    config.ProviderKind
	Host    string
	SSHPort uint16
	Owner   string
	Name    string
}

// GitSelector identifies the untrusted SSH authority and repository slug used
// to resolve a granted repository through trusted provider configuration.
type GitSelector struct {
	SSHUser string
	Host    string
	SSHPort uint16
	Owner   string
	Name    string
}

// Resolve returns the one granted repository matching selector and capability.
func (snapshot *Snapshot) Resolve(principal string, selector Selector, capability config.Capability) (ResolvedRepository, error) {
	return snapshot.resolve(principal, capability, func(repository config.Repository, provider config.Provider) bool {
		return matches(selector, repository, provider)
	})
}

// ResolveGit returns the one granted Git repository matching selector and capability.
func (snapshot *Snapshot) ResolveGit(principal string, selector GitSelector, capability config.Capability) (ResolvedRepository, error) {
	return snapshot.resolve(principal, capability, func(repository config.Repository, provider config.Provider) bool {
		return matchesGit(selector, repository, provider)
	})
}

func (snapshot *Snapshot) resolve(principal string, capability config.Capability, matchesRepository func(config.Repository, config.Provider) bool) (ResolvedRepository, error) {
	grants, ok := snapshot.grants[principal]
	if !ok {
		return ResolvedRepository{}, ErrDenied
	}

	var match ResolvedRepository
	matched := false
	for repositoryID := range grants {
		repository := snapshot.repositories[repositoryID]
		provider := snapshot.providers[repository.Provider]
		if !matchesRepository(repository, provider) {
			continue
		}
		if matched {
			return ResolvedRepository{}, ErrDenied
		}
		match = ResolvedRepository{ID: repositoryID, Repository: repository, Provider: provider}
		matched = true
	}
	if !matched {
		return ResolvedRepository{}, ErrDenied
	}
	if _, ok := grants[match.ID][capability]; !ok {
		return ResolvedRepository{}, ErrDenied
	}
	match.Repository = copyRepository(match.Repository)
	return match, nil
}

func matchesGit(selector GitSelector, repository config.Repository, provider config.Provider) bool {
	userMatches := selector.SSHUser == provider.SSHUser
	if selector.SSHUser == "" {
		userMatches = provider.Kind == config.ProviderGitHub && provider.SSHUser == "git"
	}
	slugMatches := selector.Owner == repository.Owner && selector.Name == repository.Name
	if provider.Kind == config.ProviderGitea {
		slugMatches = asciiEqualFold(selector.Owner, repository.Owner) && asciiEqualFold(selector.Name, repository.Name)
	}
	return userMatches && asciiEqualFold(selector.Host, provider.GitHost) &&
		(selector.SSHPort == 0 || selector.SSHPort == provider.SSHPort) && slugMatches
}

func asciiEqualFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range len(left) {
		leftByte, rightByte := left[index], right[index]
		if leftByte >= 0x80 || rightByte >= 0x80 {
			return false
		}
		if leftByte >= 'A' && leftByte <= 'Z' {
			leftByte += 'a' - 'A'
		}
		if rightByte >= 'A' && rightByte <= 'Z' {
			rightByte += 'a' - 'A'
		}
		if leftByte != rightByte {
			return false
		}
	}
	return true
}

func matches(selector Selector, repository config.Repository, provider config.Provider) bool {
	return (selector.Kind == "" || selector.Kind == provider.Kind) &&
		(selector.Host == "" || selector.Host == provider.GitHost) &&
		(selector.SSHPort == 0 || selector.SSHPort == provider.SSHPort) &&
		matchesRepositoryIdentity(selector, repository, provider.Kind)
}

func matchesRepositoryIdentity(selector Selector, repository config.Repository, kind config.ProviderKind) bool {
	if kind == config.ProviderGitea {
		return (selector.Owner == "" || strings.EqualFold(selector.Owner, repository.Owner)) &&
			(selector.Name == "" || strings.EqualFold(selector.Name, repository.Name))
	}
	return (selector.Owner == "" || selector.Owner == repository.Owner) &&
		(selector.Name == "" || selector.Name == repository.Name)
}
