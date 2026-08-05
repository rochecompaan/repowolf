// Package config loads and validates the service's startup policy.
package config

import "time"

type Capability string

const (
	RepositoryRead    Capability = "repository:read"
	IssuesRead        Capability = "issues:read"
	IssuesWrite       Capability = "issues:write"
	PullRequestsRead  Capability = "pull_requests:read"
	PullRequestsWrite Capability = "pull_requests:write"
	ActionsRead       Capability = "actions:read"
	StatusesRead      Capability = "statuses:read"
	GitRead           Capability = "git:read"
	GitWrite          Capability = "git:write"
)

type Config struct {
	APIVersion   string                `yaml:"apiVersion"`
	Listen       string                `yaml:"listen"`
	TLS          TLS                   `yaml:"tls"`
	Tools        Tools                 `yaml:"tools"`
	Providers    map[string]Provider   `yaml:"providers"`
	Repositories map[string]Repository `yaml:"repositories"`
	Principals   map[string]Principal  `yaml:"principals"`
	Limits       Limits                `yaml:"limits"`
}

type TLS struct {
	Certificate string `yaml:"certificate"`
	PrivateKey  string `yaml:"privateKey"`
}

type Tools struct {
	GH  *string `yaml:"gh"`
	SSH *string `yaml:"ssh"`
}

type ProviderKind string

const ProviderGitHub ProviderKind = "github"

type Provider struct {
	Kind    ProviderKind `yaml:"kind"`
	APIHost string       `yaml:"apiHost"`
	GitHost string       `yaml:"gitHost"`
	SSHUser string       `yaml:"sshUser"`
	SSHPort uint16       `yaml:"sshPort"`
}

type Repository struct {
	Provider string     `yaml:"provider"`
	Owner    string     `yaml:"owner"`
	Name     string     `yaml:"name"`
	Git      PushPolicy `yaml:"git"`
}

type Principal struct {
	TokenEnvs []string `yaml:"tokenEnvs"`
	Grants    []Grant  `yaml:"grants"`
}

type Grant struct {
	Repository   string       `yaml:"repository"`
	Capabilities []Capability `yaml:"capabilities"`
}

type PushPolicy struct {
	DenyRefs      []string `yaml:"denyRefs"`
	DenyDeletes   bool     `yaml:"denyDeletes"`
	MaxRefUpdates int      `yaml:"maxRefUpdates"`
}

type Limits struct {
	MaxConcurrentRequests             int           `yaml:"maxConcurrentRequests"`
	MaxConcurrentRequestsPerPrincipal int           `yaml:"maxConcurrentRequestsPerPrincipal"`
	MaxMessageBytes                   int           `yaml:"maxMessageBytes"`
	MaxStreamChunkBytes               int           `yaml:"maxStreamChunkBytes"`
	MaxPushPrefixBytes                int           `yaml:"maxPushPrefixBytes"`
	MaxGitBytesPerDirection           int64         `yaml:"maxGitBytesPerDirection"`
	InitialStreamTimeout              time.Duration `yaml:"-"`
	OperationTimeout                  time.Duration `yaml:"-"`
	IdleStreamTimeout                 time.Duration `yaml:"-"`
}

// Repository returns a copy whose mutable policy slice is independent from c.
func (c Config) Repository(id string) (Repository, bool) {
	repository, ok := c.Repositories[id]
	if !ok {
		return Repository{}, false
	}
	repository.Git.DenyRefs = append([]string(nil), repository.Git.DenyRefs...)
	return repository, true
}
