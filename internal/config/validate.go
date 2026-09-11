package config

import (
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const apiVersion = "repowolf.dev/v1alpha1"

var (
	tokenEnvName   = regexp.MustCompile(`^REPOWOLF_TOKEN_[A-Z0-9_]+$`)
	identifier     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	ownerName      = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})?$`)
	repositoryName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	giteaName      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	sshUserName    = regexp.MustCompile(`^[a-z_][a-z0-9_-]*\$?$`)
)

// Validate checks configuration syntax and references without modifying c.
func (c Config) Validate() error {
	if c.APIVersion != apiVersion {
		return fmt.Errorf("unsupported apiVersion %q", c.APIVersion)
	}
	if err := validateListen(c.Listen); err != nil {
		return err
	}
	if c.TLS.Certificate == "" || c.TLS.PrivateKey == "" {
		return fmt.Errorf("tls certificate and privateKey are required")
	}
	if err := validateTools(c.Tools); err != nil {
		return err
	}
	if len(c.Providers) == 0 || len(c.Repositories) == 0 || len(c.Principals) == 0 {
		return fmt.Errorf("providers, repositories, and principals are required")
	}
	legacyGitHubProviders := 0
	for id, provider := range c.Providers {
		if !identifier.MatchString(id) {
			return fmt.Errorf("invalid provider id %q", id)
		}
		if err := validateProvider(id, provider); err != nil {
			return err
		}
		if provider.Kind == ProviderGitHub && provider.TokenEnv == "" {
			legacyGitHubProviders++
		}
	}
	if legacyGitHubProviders > 1 {
		return fmt.Errorf("only one GitHub provider may omit tokenEnv for legacy configuration")
	}
	if err := validateCredentialNames(c.Principals, c.Providers); err != nil {
		return err
	}
	for id, repository := range c.Repositories {
		if !identifier.MatchString(id) {
			return fmt.Errorf("invalid repository id %q", id)
		}
		if err := validateRepository(id, repository, c.Providers); err != nil {
			return err
		}
	}
	if err := validateGiteaRepositoryCollisions(c.Repositories, c.Providers); err != nil {
		return err
	}
	if err := validatePrincipals(c.Principals, c.Repositories); err != nil {
		return err
	}
	return validateLimits(c.Limits)
}

func validateListen(listen string) error {
	if listen == "" {
		return fmt.Errorf("listen is required")
	}
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("invalid listen address %q", listen)
	}
	value, err := strconv.ParseUint(port, 10, 16)
	if err != nil || value == 0 {
		return fmt.Errorf("invalid listen address %q", listen)
	}
	return nil
}

func validateTools(tools Tools) error {
	for _, tool := range []struct {
		name string
		path *string
	}{{"gh", tools.GH}, {"ssh", tools.SSH}} {
		if tool.path != nil && (!filepath.IsAbs(*tool.path) || *tool.path == "/") {
			return fmt.Errorf("%s override must be an absolute executable path", tool.name)
		}
	}
	return nil
}

func validateProvider(id string, provider Provider) error {
	switch provider.Kind {
	case ProviderGitHub, ProviderGitea:
	default:
		return fmt.Errorf("provider %q has unsupported kind %q", id, provider.Kind)
	}
	if provider.TokenEnv == "" {
		if provider.Kind == ProviderGitea {
			return fmt.Errorf("provider %q requires tokenEnv", id)
		}
	} else if !tokenEnvName.MatchString(provider.TokenEnv) {
		return fmt.Errorf("provider %q has invalid token environment name", id)
	}
	if provider.CAFile != "" && provider.Kind != ProviderGitea {
		return fmt.Errorf("provider %q caFile is supported only for Gitea", id)
	}
	if !ValidProviderHost(provider.APIHost) || !ValidProviderHost(provider.GitHost) {
		return fmt.Errorf("provider %q has invalid host", id)
	}
	if !sshUserName.MatchString(provider.SSHUser) {
		return fmt.Errorf("provider %q has invalid ssh user", id)
	}
	if provider.SSHPort == 0 {
		return fmt.Errorf("provider %q has invalid ssh port", id)
	}
	return nil
}

func validateRepository(id string, repository Repository, providers map[string]Provider) error {
	provider, ok := providers[repository.Provider]
	if !ok {
		return fmt.Errorf("repository %q references undefined provider %q", id, repository.Provider)
	}
	if !ValidRepositoryIdentity(provider.Kind, repository.Owner, repository.Name) {
		return fmt.Errorf("repository %q has invalid owner or name", id)
	}
	return validatePushPolicy(id, repository.Git)
}

// ValidRepositoryIdentity reports whether owner and name satisfy kind's
// configured repository grammar.
func ValidRepositoryIdentity(kind ProviderKind, owner, name string) bool {
	switch kind {
	case ProviderGitHub:
		return ownerName.MatchString(owner) && repositoryName.MatchString(name)
	case ProviderGitea:
		return validGiteaName(owner) && validGiteaName(name) && !strings.HasSuffix(name, ".git")
	default:
		return false
	}
}

func validGiteaName(value string) bool {
	return giteaName.MatchString(value) && value != "." && value != ".."
}

func validateCredentialNames(principals map[string]Principal, providers map[string]Provider) error {
	owners := make(map[string]string)
	for _, id := range sortedIDs(principals) {
		for _, name := range principals[id].TokenEnvs {
			if !tokenEnvName.MatchString(name) {
				return fmt.Errorf("principal %q has invalid token environment name", id)
			}
			if err := addCredentialName(owners, name, fmt.Sprintf("principal %q", id)); err != nil {
				return err
			}
		}
	}
	for _, id := range sortedIDs(providers) {
		name := providers[id].TokenEnv
		if name == "" {
			continue
		}
		if err := addCredentialName(owners, name, fmt.Sprintf("provider %q", id)); err != nil {
			return err
		}
	}
	return nil
}

func addCredentialName(owners map[string]string, name, owner string) error {
	if previous, exists := owners[name]; exists {
		return fmt.Errorf("%s and %s duplicate token environment %q", previous, owner, name)
	}
	owners[name] = owner
	return nil
}

func validateGiteaRepositoryCollisions(repositories map[string]Repository, providers map[string]Provider) error {
	ids := sortedIDs(repositories)
	slugs := make(map[string]string)
	for _, id := range ids {
		repository := repositories[id]
		if providers[repository.Provider].Kind != ProviderGitea {
			continue
		}
		key := strings.ToLower(repository.Owner) + "/" + strings.ToLower(repository.Name)
		if previous, exists := slugs[key]; exists {
			return fmt.Errorf("repositories %q and %q have colliding Gitea repository slug", previous, id)
		}
		slugs[key] = id
	}
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

func validatePrincipals(principals map[string]Principal, repositories map[string]Repository) error {
	for id, principal := range principals {
		if !identifier.MatchString(id) {
			return fmt.Errorf("invalid principal id %q", id)
		}
		if len(principal.TokenEnvs) == 0 || len(principal.Grants) == 0 {
			return fmt.Errorf("principal %q requires tokenEnvs and grants", id)
		}
		grantedRepositories := make(map[string]struct{}, len(principal.Grants))
		for index, grant := range principal.Grants {
			if _, ok := repositories[grant.Repository]; !ok {
				return fmt.Errorf("principal %q grant %d references undefined repository %q", id, index, grant.Repository)
			}
			if _, exists := grantedRepositories[grant.Repository]; exists {
				return fmt.Errorf("principal %q has duplicate grant for repository %q", id, grant.Repository)
			}
			grantedRepositories[grant.Repository] = struct{}{}
			if err := validateCapabilities(id, index, grant.Capabilities); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateCapabilities(principal string, index int, capabilities []Capability) error {
	if len(capabilities) == 0 {
		return fmt.Errorf("principal %q grant %d has no capabilities", principal, index)
	}
	seen := make(map[Capability]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if !validCapability(capability) {
			return fmt.Errorf("principal %q grant %d has unsupported capability %q", principal, index, capability)
		}
		if _, ok := seen[capability]; ok {
			return fmt.Errorf("principal %q grant %d duplicates capability %q", principal, index, capability)
		}
		seen[capability] = struct{}{}
	}
	if _, writes := seen[GitWrite]; writes {
		if _, reads := seen[GitRead]; !reads {
			return fmt.Errorf("principal %q grant %d grants git:write without git:read", principal, index)
		}
	}
	return nil
}

func validCapability(capability Capability) bool {
	switch capability {
	case RepositoryRead, IssuesRead, IssuesWrite, PullRequestsRead, PullRequestsWrite, ActionsRead, StatusesRead, GitRead, GitWrite:
		return true
	default:
		return false
	}
}

func validatePushPolicy(id string, policy PushPolicy) error {
	if policy.MaxRefUpdates <= 0 {
		return fmt.Errorf("repository %q has invalid maxRefUpdates", id)
	}
	seen := make(map[string]struct{}, len(policy.DenyRefs))
	for _, ref := range policy.DenyRefs {
		if !validRef(ref) {
			return fmt.Errorf("repository %q has invalid denied ref %q", id, ref)
		}
		if _, ok := seen[ref]; ok {
			return fmt.Errorf("repository %q duplicates denied ref %q", id, ref)
		}
		seen[ref] = struct{}{}
	}
	return nil
}

func validateLimits(limits Limits) error {
	values := []struct {
		name  string
		value int64
	}{
		{"maxConcurrentRequests", int64(limits.MaxConcurrentRequests)},
		{"maxConcurrentRequestsPerPrincipal", int64(limits.MaxConcurrentRequestsPerPrincipal)},
		{"maxMessageBytes", int64(limits.MaxMessageBytes)},
		{"maxStreamChunkBytes", int64(limits.MaxStreamChunkBytes)},
		{"maxPushPrefixBytes", int64(limits.MaxPushPrefixBytes)},
		{"maxGitBytesPerDirection", limits.MaxGitBytesPerDirection},
		{"initialStreamTimeout", int64(limits.InitialStreamTimeout)},
		{"operationTimeout", int64(limits.OperationTimeout)},
		{"idleStreamTimeout", int64(limits.IdleStreamTimeout)},
	}
	for _, value := range values {
		if value.value <= 0 {
			return fmt.Errorf("%s must be positive", value.name)
		}
	}
	if limits.MaxConcurrentRequestsPerPrincipal > limits.MaxConcurrentRequests {
		return fmt.Errorf("maxConcurrentRequestsPerPrincipal exceeds maxConcurrentRequests")
	}
	if limits.MaxMessageBytes > maxMessageBytes {
		return fmt.Errorf("maxMessageBytes exceeds hard cap")
	}
	if limits.MaxStreamChunkBytes > maxStreamChunkBytes || limits.MaxStreamChunkBytes > limits.MaxMessageBytes {
		return fmt.Errorf("maxStreamChunkBytes exceeds its cap")
	}
	if limits.MaxPushPrefixBytes > maxPushPrefixBytes || int64(limits.MaxPushPrefixBytes) > limits.MaxGitBytesPerDirection {
		return fmt.Errorf("maxPushPrefixBytes exceeds its cap")
	}
	return nil
}

// ValidProviderHost reports whether host uses the host-only provider grammar.
func ValidProviderHost(host string) bool {
	if host == "" || len(host) > 253 || strings.ContainsAny(host, ":/@ ") {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-') {
				return false
			}
		}
	}
	return true
}

func validRef(ref string) bool {
	for _, character := range ref {
		if character <= 0x1f || character == 0x7f {
			return false
		}
	}
	if !strings.HasPrefix(ref, "refs/") || len(ref) == len("refs/") || strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".") || strings.HasSuffix(ref, ".lock") || strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.Contains(ref, "//") || strings.ContainsAny(ref, " ~^:?*[\\") {
		return false
	}
	for _, component := range strings.Split(ref, "/") {
		if component == "" || strings.HasPrefix(component, ".") {
			return false
		}
	}
	return true
}
