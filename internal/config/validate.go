package config

import (
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const apiVersion = "repowolf.dev/v1alpha1"

var (
	tokenEnvName   = regexp.MustCompile(`^REPOWOLF_TOKEN_[A-Z0-9_]+$`)
	identifier     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	ownerName      = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})?$`)
	repositoryName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
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
	for id, provider := range c.Providers {
		if !identifier.MatchString(id) {
			return fmt.Errorf("invalid provider id %q", id)
		}
		if err := validateProvider(id, provider); err != nil {
			return err
		}
	}
	for id, repository := range c.Repositories {
		if !identifier.MatchString(id) {
			return fmt.Errorf("invalid repository id %q", id)
		}
		if err := validateRepository(id, repository, c.Providers); err != nil {
			return err
		}
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
	if provider.Kind != ProviderGitHub {
		return fmt.Errorf("provider %q has unsupported kind %q", id, provider.Kind)
	}
	if !validHost(provider.APIHost) || !validHost(provider.GitHost) {
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
	if _, ok := providers[repository.Provider]; !ok {
		return fmt.Errorf("repository %q references undefined provider %q", id, repository.Provider)
	}
	if !ownerName.MatchString(repository.Owner) || !repositoryName.MatchString(repository.Name) {
		return fmt.Errorf("repository %q has invalid owner or name", id)
	}
	return validatePushPolicy(id, repository.Git)
}

func validatePrincipals(principals map[string]Principal, repositories map[string]Repository) error {
	tokenEnvs := make(map[string]string)
	for id, principal := range principals {
		if !identifier.MatchString(id) {
			return fmt.Errorf("invalid principal id %q", id)
		}
		if len(principal.TokenEnvs) == 0 || len(principal.Grants) == 0 {
			return fmt.Errorf("principal %q requires tokenEnvs and grants", id)
		}
		for _, tokenEnv := range principal.TokenEnvs {
			if !tokenEnvName.MatchString(tokenEnv) {
				return fmt.Errorf("principal %q has invalid token environment name", id)
			}
			if previous, exists := tokenEnvs[tokenEnv]; exists {
				return fmt.Errorf("principals %q and %q duplicate token environment %q", previous, id, tokenEnv)
			}
			tokenEnvs[tokenEnv] = id
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

func validHost(host string) bool {
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
