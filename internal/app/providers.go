package app

import (
	"fmt"
	"sort"

	giteasdk "code.gitea.io/sdk/gitea"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/credentials"
	providergithub "github.com/rochecompaan/repowolf/internal/provider/github"
	"github.com/rochecompaan/repowolf/internal/runner"
	"github.com/rochecompaan/repowolf/internal/server"
)

// providerInstance holds the configuration and credential for one provider ID.
type providerInstance struct {
	provider config.Provider
	token    string
	legacy   bool
	github   server.GitHubExecutor
	gitea    *giteasdk.Client
}

type giteaClientConstructor func(config.Provider, string) (*giteasdk.Client, error)

func buildProviderInstances(
	cfg config.Config,
	snapshot *credentials.Snapshot,
	tools runner.Toolset,
	tokenFreeEnvironment []string,
	caller providergithub.Caller,
	newGiteaClient giteaClientConstructor,
) (map[string]providerInstance, error) {
	instances := make(map[string]providerInstance, len(cfg.Providers))
	for _, id := range sortedProviderIDs(cfg.Providers) {
		provider := cfg.Providers[id]
		token, ok := snapshot.ProviderToken(id)
		if !ok {
			return nil, fmt.Errorf("create provider %q: credential unavailable", id)
		}
		instance := providerInstance{
			provider: provider,
			token:    token,
			legacy:   snapshot.UsesLegacyProviderToken(id),
		}

		switch provider.Kind {
		case config.ProviderGitHub:
			environment := runner.GitHubEnvironment(tokenFreeEnvironment, token)
			adapter, err := providergithub.New(providergithub.AdapterOptions{
				Path:        tools.GH,
				Environment: environment,
				Timeout:     cfg.Limits.OperationTimeout,
				Caller:      caller,
			})
			if err != nil {
				return nil, fmt.Errorf("create GitHub provider %q: %w", id, err)
			}
			instance.github = adapter
		case config.ProviderGitea:
			client, err := newGiteaClient(provider, token)
			if err != nil {
				return nil, fmt.Errorf("create Gitea provider %q: %w", id, err)
			}
			instance.gitea = client
		default:
			return nil, fmt.Errorf("create provider %q: unsupported kind", id)
		}
		instances[id] = instance
	}
	return instances, nil
}

func buildGitHubExecutor(instances map[string]providerInstance) *githubExecutor {
	adapters := make(map[string]server.GitHubExecutor)
	for id, instance := range instances {
		if instance.github != nil {
			adapters[id] = instance.github
		}
	}
	if len(adapters) == 0 {
		return nil
	}
	return &githubExecutor{adapters: adapters}
}

func sortedProviderIDs(providers map[string]config.Provider) []string {
	ids := make([]string, 0, len(providers))
	for id := range providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
