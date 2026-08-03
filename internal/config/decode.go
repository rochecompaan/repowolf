package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type rawConfig struct {
	APIVersion   string                   `yaml:"apiVersion"`
	Listen       string                   `yaml:"listen"`
	TLS          TLS                      `yaml:"tls"`
	Tools        Tools                    `yaml:"tools"`
	Providers    map[string]rawProvider   `yaml:"providers"`
	Repositories map[string]rawRepository `yaml:"repositories"`
	Principals   map[string]Principal     `yaml:"principals"`
	Limits       rawLimits                `yaml:"limits"`
}

type rawProvider struct {
	Kind    ProviderKind `yaml:"kind"`
	APIHost string       `yaml:"apiHost"`
	GitHost string       `yaml:"gitHost"`
	SSHUser string       `yaml:"sshUser"`
	SSHPort *uint16      `yaml:"sshPort"`
}

type rawRepository struct {
	Provider string        `yaml:"provider"`
	Owner    string        `yaml:"owner"`
	Name     string        `yaml:"name"`
	Git      rawPushPolicy `yaml:"git"`
}

type rawPushPolicy struct {
	DenyRefs      *[]string `yaml:"denyRefs"`
	DenyDeletes   *bool     `yaml:"denyDeletes"`
	MaxRefUpdates *int      `yaml:"maxRefUpdates"`
}

type rawLimits struct {
	MaxConcurrentRequests             *int    `yaml:"maxConcurrentRequests"`
	MaxConcurrentRequestsPerPrincipal *int    `yaml:"maxConcurrentRequestsPerPrincipal"`
	MaxMessageBytes                   *int    `yaml:"maxMessageBytes"`
	MaxStreamChunkBytes               *int    `yaml:"maxStreamChunkBytes"`
	MaxPushPrefixBytes                *int    `yaml:"maxPushPrefixBytes"`
	MaxGitBytesPerDirection           *int64  `yaml:"maxGitBytesPerDirection"`
	InitialStreamTimeout              *string `yaml:"initialStreamTimeout"`
	OperationTimeout                  *string `yaml:"operationTimeout"`
	IdleStreamTimeout                 *string `yaml:"idleStreamTimeout"`
}

// Decode parses one strict YAML configuration document and validates it.
func Decode(reader io.Reader) (Config, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return Config{}, fmt.Errorf("read configuration: %w", err)
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return Config{}, err
	}

	var raw rawConfig
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("decode YAML: %w", err)
	}
	var second yaml.Node
	if err := decoder.Decode(&second); err != io.EOF {
		if err != nil {
			return Config{}, fmt.Errorf("decode YAML: %w", err)
		}
		return Config{}, fmt.Errorf("configuration contains more than one YAML document")
	}

	cfg, err := normalize(raw)
	if err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// LoadFile decodes a configuration file.
func LoadFile(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open configuration: %w", err)
	}
	defer file.Close()
	return Decode(file)
}

func rejectDuplicateKeys(data []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("decode YAML: %w", err)
	}
	if err := duplicateKey(&document); err != nil {
		return err
	}
	var second yaml.Node
	if err := decoder.Decode(&second); err != io.EOF {
		if err != nil {
			return fmt.Errorf("decode YAML: %w", err)
		}
		return fmt.Errorf("configuration contains more than one YAML document")
	}
	return nil
}

func duplicateKey(node *yaml.Node) error {
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]struct{}, len(node.Content)/2)
		for index := 0; index < len(node.Content); index += 2 {
			key, value := node.Content[index], node.Content[index+1]
			if _, ok := seen[key.Value]; ok {
				return fmt.Errorf("duplicate YAML key %q", key.Value)
			}
			seen[key.Value] = struct{}{}
			if err := duplicateKey(value); err != nil {
				return err
			}
		}
		return nil
	}
	for _, child := range node.Content {
		if err := duplicateKey(child); err != nil {
			return err
		}
	}
	if node.Alias != nil {
		return duplicateKey(node.Alias)
	}
	return nil
}

func normalize(raw rawConfig) (Config, error) {
	cfg := Config{
		APIVersion:   raw.APIVersion,
		Listen:       raw.Listen,
		TLS:          raw.TLS,
		Tools:        raw.Tools,
		Providers:    make(map[string]Provider, len(raw.Providers)),
		Repositories: make(map[string]Repository, len(raw.Repositories)),
		Principals:   clonePrincipals(raw.Principals),
		Limits:       defaultLimits(),
	}
	for id, provider := range raw.Providers {
		port := uint16(defaultSSHPort)
		if provider.SSHPort != nil {
			port = *provider.SSHPort
		}
		cfg.Providers[id] = Provider{Kind: provider.Kind, APIHost: provider.APIHost, GitHost: provider.GitHost, SSHUser: provider.SSHUser, SSHPort: port}
	}
	for id, repository := range raw.Repositories {
		policy := defaultPushPolicy()
		if repository.Git.DenyRefs != nil {
			policy.DenyRefs = copyStrings(*repository.Git.DenyRefs)
		}
		if repository.Git.DenyDeletes != nil {
			policy.DenyDeletes = *repository.Git.DenyDeletes
		}
		if repository.Git.MaxRefUpdates != nil {
			policy.MaxRefUpdates = *repository.Git.MaxRefUpdates
		}
		cfg.Repositories[id] = Repository{Provider: repository.Provider, Owner: repository.Owner, Name: repository.Name, Git: policy}
	}
	if err := normalizeLimits(&cfg.Limits, raw.Limits); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func normalizeLimits(limits *Limits, raw rawLimits) error {
	if raw.MaxConcurrentRequests != nil {
		limits.MaxConcurrentRequests = *raw.MaxConcurrentRequests
	}
	if raw.MaxConcurrentRequestsPerPrincipal != nil {
		limits.MaxConcurrentRequestsPerPrincipal = *raw.MaxConcurrentRequestsPerPrincipal
	}
	if raw.MaxMessageBytes != nil {
		limits.MaxMessageBytes = *raw.MaxMessageBytes
	}
	if raw.MaxStreamChunkBytes != nil {
		limits.MaxStreamChunkBytes = *raw.MaxStreamChunkBytes
	}
	if raw.MaxPushPrefixBytes != nil {
		limits.MaxPushPrefixBytes = *raw.MaxPushPrefixBytes
	}
	if raw.MaxGitBytesPerDirection != nil {
		limits.MaxGitBytesPerDirection = *raw.MaxGitBytesPerDirection
	}
	for _, duration := range []struct {
		raw    *string
		target *time.Duration
	}{
		{raw.InitialStreamTimeout, &limits.InitialStreamTimeout}, {raw.OperationTimeout, &limits.OperationTimeout}, {raw.IdleStreamTimeout, &limits.IdleStreamTimeout},
	} {
		if duration.raw == nil {
			continue
		}
		parsed, err := time.ParseDuration(*duration.raw)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", *duration.raw, err)
		}
		*duration.target = parsed
	}
	return nil
}

func clonePrincipals(source map[string]Principal) map[string]Principal {
	principals := make(map[string]Principal, len(source))
	for id, principal := range source {
		principal.TokenEnvs = copyStrings(principal.TokenEnvs)
		principal.Grants = append([]Grant(nil), principal.Grants...)
		for index := range principal.Grants {
			principal.Grants[index].Capabilities = append([]Capability(nil), principal.Grants[index].Capabilities...)
		}
		principals[id] = principal
	}
	return principals
}
