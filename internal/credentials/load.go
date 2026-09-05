package credentials

import (
	"crypto/sha256"
	"fmt"
	"sort"

	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
)

const (
	legacyGHToken     = "GH_TOKEN"
	legacyGitHubToken = "GITHUB_TOKEN"
)

// Load resolves every configured credential once into an immutable snapshot.
func Load(cfg config.Config, lookup LookupEnv) (*Snapshot, error) {
	read := cachedLookup(lookup)
	seen := make(map[[sha256.Size]byte]string)
	values := make(map[string][]string, len(cfg.Principals))
	environments := make([]string, 0)

	for _, id := range sortedIDs(cfg.Principals) {
		for _, name := range cfg.Principals[id].TokenEnvs {
			value, found := read(name)
			if !found {
				return nil, fmt.Errorf("token environment %q is missing", name)
			}
			if value == "" {
				return nil, fmt.Errorf("token environment %q is empty", name)
			}
			if !auth.ValidToken(value) {
				return nil, fmt.Errorf("token environment %q has an invalid token", name)
			}
			if err := addValue(seen, value, name); err != nil {
				return nil, err
			}
			values[id] = append(values[id], value)
			environments = append(environments, name)
		}
	}

	index, err := auth.NewIndex(values)
	if err != nil {
		return nil, err
	}

	snapshot := &Snapshot{auth: index, providers: make(map[string]providerCredential, len(cfg.Providers))}
	for _, id := range sortedIDs(cfg.Providers) {
		provider := cfg.Providers[id]
		if provider.Kind == config.ProviderGitHub && provider.TokenEnv == "" {
			credential, err := loadLegacyGitHubToken(id, read, seen)
			if err != nil {
				return nil, err
			}
			snapshot.providers[id] = credential
			environments = append(environments, credential.environment)
			continue
		}

		if provider.TokenEnv == "" {
			return nil, fmt.Errorf("provider %q has no token environment", id)
		}
		value, found := read(provider.TokenEnv)
		if !found {
			return nil, fmt.Errorf("provider %q token environment %q is missing", id, provider.TokenEnv)
		}
		if value == "" {
			return nil, fmt.Errorf("provider %q token environment %q is empty", id, provider.TokenEnv)
		}
		if err := addValue(seen, value, provider.TokenEnv); err != nil {
			return nil, err
		}
		snapshot.providers[id] = providerCredential{value: value, environment: provider.TokenEnv}
		environments = append(environments, provider.TokenEnv)
	}

	sort.Strings(environments)
	snapshot.environments = environments
	return snapshot, nil
}

func cachedLookup(lookup LookupEnv) LookupEnv {
	values := make(map[string]struct {
		value string
		found bool
	})
	return func(name string) (string, bool) {
		if result, ok := values[name]; ok {
			return result.value, result.found
		}
		value, found := "", false
		if lookup != nil {
			value, found = lookup(name)
		}
		values[name] = struct {
			value string
			found bool
		}{value: value, found: found}
		return value, found
	}
}

func loadLegacyGitHubToken(providerID string, lookup LookupEnv, seen map[[sha256.Size]byte]string) (providerCredential, error) {
	ghValue, ghFound := lookup(legacyGHToken)
	githubValue, githubFound := lookup(legacyGitHubToken)
	if ghFound && githubFound {
		return providerCredential{}, fmt.Errorf("provider %q has both legacy token environments %q and %q", providerID, legacyGHToken, legacyGitHubToken)
	}
	if ghFound {
		if ghValue == "" {
			return providerCredential{}, fmt.Errorf("provider %q token environment %q is empty", providerID, legacyGHToken)
		}
		if err := addValue(seen, ghValue, legacyGHToken); err != nil {
			return providerCredential{}, err
		}
		return providerCredential{value: ghValue, environment: legacyGHToken, legacy: true}, nil
	}
	if githubFound {
		if githubValue == "" {
			return providerCredential{}, fmt.Errorf("provider %q token environment %q is empty", providerID, legacyGitHubToken)
		}
		if err := addValue(seen, githubValue, legacyGitHubToken); err != nil {
			return providerCredential{}, err
		}
		return providerCredential{value: githubValue, environment: legacyGitHubToken, legacy: true}, nil
	}
	return providerCredential{}, fmt.Errorf("provider %q requires %q or %q", providerID, legacyGHToken, legacyGitHubToken)
}

func addValue(seen map[[sha256.Size]byte]string, value, source string) error {
	digest := sha256.Sum256([]byte(value))
	if previous, duplicate := seen[digest]; duplicate {
		return fmt.Errorf("credential source %q duplicates credential source %q", source, previous)
	}
	seen[digest] = source
	return nil
}

func sortedIDs[T any](values map[string]T) []string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
