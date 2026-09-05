// Package credentials loads immutable startup credentials.
package credentials

import "github.com/rochecompaan/repowolf/internal/auth"

type LookupEnv func(string) (string, bool)

type Snapshot struct {
	auth         *auth.Index
	providers    map[string]providerCredential
	environments []string
}

type providerCredential struct {
	value       string
	environment string
	legacy      bool
}

func (snapshot *Snapshot) AuthIndex() *auth.Index {
	if snapshot == nil {
		return nil
	}
	return snapshot.auth
}

func (snapshot *Snapshot) ProviderToken(providerID string) (string, bool) {
	if snapshot == nil {
		return "", false
	}
	credential, ok := snapshot.providers[providerID]
	return credential.value, ok
}

func (snapshot *Snapshot) EnvironmentNames() []string {
	if snapshot == nil {
		return nil
	}
	return append([]string(nil), snapshot.environments...)
}

func (snapshot *Snapshot) UsesLegacyProviderToken(providerID string) bool {
	if snapshot == nil {
		return false
	}
	credential, ok := snapshot.providers[providerID]
	return ok && credential.legacy
}
