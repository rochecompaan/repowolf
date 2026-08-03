package policy

import "github.com/rochecompaan/repowolf/internal/config"

// Selector describes optional exact repository identity fields supplied by a client.
type Selector struct {
	Kind    config.ProviderKind
	Host    string
	SSHPort uint16
	Owner   string
	Name    string
}

// Resolve returns the one granted repository matching selector and capability.
func (snapshot *Snapshot) Resolve(principal string, selector Selector, capability config.Capability) (ResolvedRepository, error) {
	grants, ok := snapshot.grants[principal]
	if !ok {
		return ResolvedRepository{}, ErrDenied
	}

	var match ResolvedRepository
	matched := false
	for repositoryID := range grants {
		repository := snapshot.repositories[repositoryID]
		provider := snapshot.providers[repository.Provider]
		if !matches(selector, repository, provider) {
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

func matches(selector Selector, repository config.Repository, provider config.Provider) bool {
	return (selector.Kind == "" || selector.Kind == provider.Kind) &&
		(selector.Host == "" || selector.Host == provider.GitHost) &&
		(selector.SSHPort == 0 || selector.SSHPort == provider.SSHPort) &&
		(selector.Owner == "" || selector.Owner == repository.Owner) &&
		(selector.Name == "" || selector.Name == repository.Name)
}
