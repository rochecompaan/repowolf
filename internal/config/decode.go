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
	Kind     ProviderKind      `yaml:"kind"`
	APIHost  string            `yaml:"apiHost"`
	GitHost  string            `yaml:"gitHost"`
	SSHUser  string            `yaml:"sshUser"`
	SSHPort  *uint16           `yaml:"sshPort"`
	TokenEnv rawOptionalString `yaml:"tokenEnv"`
	CAFile   rawCAFile         `yaml:"caFile"`
}

type rawOptionalString struct {
	present bool
	value   string
}

func (value *rawOptionalString) UnmarshalYAML(node *yaml.Node) error {
	value.present = true
	if node.Tag != "!!str" {
		return fmt.Errorf("tokenEnv must be a string")
	}
	value.value = node.Value
	return nil
}

type rawCAFile rawOptionalString

func (value *rawCAFile) UnmarshalYAML(node *yaml.Node) error {
	value.present = true
	if node.Tag != "!!str" {
		return fmt.Errorf("caFile must be a string")
	}
	value.value = node.Value
	return nil
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
	if err := rejectNullProviderFields(&document); err != nil {
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

func rejectNullProviderFields(document *yaml.Node) error {
	// yaml.v3 bypasses UnmarshalYAML for explicit null values.
	if len(document.Content) == 0 {
		return nil
	}
	root, err := resolveEffectiveMapping(document.Content[0])
	if err != nil {
		return err
	}
	providers, ok := effectiveMappingValue(root, "providers")
	if !ok {
		return nil
	}
	providerEntries, err := resolveEffectiveMapping(providers)
	if err != nil {
		return err
	}
	for _, provider := range providerEntries {
		fields, err := resolveEffectiveMapping(provider.value)
		if err != nil {
			return err
		}
		for _, field := range []string{"tokenEnv", "caFile"} {
			value, ok := effectiveMappingValue(fields, field)
			if ok && isNullNode(value) {
				return fmt.Errorf("%s must be a string", field)
			}
		}
	}
	return nil
}

type effectiveMappingEntry struct {
	key   string
	value *yaml.Node
}

func resolveEffectiveMapping(node *yaml.Node) ([]effectiveMappingEntry, error) {
	var entries []effectiveMappingEntry
	err := collectEffectiveMapping(node, false, make(map[*yaml.Node]struct{}), make(map[string]struct{}), &entries)
	return entries, err
}

func collectEffectiveMapping(node *yaml.Node, allowSequence bool, active map[*yaml.Node]struct{}, seen map[string]struct{}, entries *[]effectiveMappingEntry) error {
	if node == nil {
		return nil
	}
	if _, exists := active[node]; exists {
		return fmt.Errorf("cyclic YAML alias")
	}
	active[node] = struct{}{}
	defer delete(active, node)

	if node.Kind == yaml.AliasNode {
		return collectEffectiveMapping(node.Alias, allowSequence, active, seen, entries)
	}
	if node.Kind == yaml.SequenceNode && allowSequence {
		for _, mapping := range node.Content {
			if err := collectEffectiveMapping(mapping, false, active, seen, entries); err != nil {
				return err
			}
		}
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}

	for index := 0; index < len(node.Content); index += 2 {
		key, value := node.Content[index], node.Content[index+1]
		if isMergeKey(key) {
			continue
		}
		if _, exists := seen[key.Value]; exists {
			continue
		}
		seen[key.Value] = struct{}{}
		*entries = append(*entries, effectiveMappingEntry{key: key.Value, value: value})
	}
	for index := 0; index < len(node.Content); index += 2 {
		key, value := node.Content[index], node.Content[index+1]
		if isMergeKey(key) {
			if err := collectEffectiveMapping(value, true, active, seen, entries); err != nil {
				return err
			}
		}
	}
	return nil
}

func effectiveMappingValue(entries []effectiveMappingEntry, field string) (*yaml.Node, bool) {
	for _, entry := range entries {
		if entry.key == field {
			return entry.value, true
		}
	}
	return nil, false
}

func isMergeKey(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && node.Value == "<<" && (node.Tag == "" || node.Tag == "!" || node.ShortTag() == "!!merge")
}

func isNullNode(node *yaml.Node) bool {
	for node != nil && node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	return node != nil && node.ShortTag() == "!!null"
}

func duplicateKey(node *yaml.Node) error {
	return duplicateKeyPath(node, make(map[*yaml.Node]struct{}))
}

func duplicateKeyPath(node *yaml.Node, active map[*yaml.Node]struct{}) error {
	if node == nil {
		return nil
	}
	if _, exists := active[node]; exists {
		return fmt.Errorf("cyclic YAML alias")
	}
	active[node] = struct{}{}
	defer delete(active, node)

	if node.Kind == yaml.MappingNode {
		seen := make(map[string]struct{}, len(node.Content)/2)
		for index := 0; index < len(node.Content); index += 2 {
			key, value := node.Content[index], node.Content[index+1]
			if _, ok := seen[key.Value]; ok {
				return fmt.Errorf("duplicate YAML key %q", key.Value)
			}
			seen[key.Value] = struct{}{}
			if err := duplicateKeyPath(value, active); err != nil {
				return err
			}
		}
		return nil
	}
	for _, child := range node.Content {
		if err := duplicateKeyPath(child, active); err != nil {
			return err
		}
	}
	if node.Alias != nil {
		return duplicateKeyPath(node.Alias, active)
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
		if provider.TokenEnv.present && provider.TokenEnv.value == "" {
			return Config{}, fmt.Errorf("provider %q tokenEnv must not be empty", id)
		}
		if provider.CAFile.present && provider.CAFile.value == "" {
			return Config{}, fmt.Errorf("provider %q caFile must not be empty", id)
		}
		port := uint16(defaultSSHPort)
		if provider.SSHPort != nil {
			port = *provider.SSHPort
		}
		cfg.Providers[id] = Provider{Kind: provider.Kind, APIHost: provider.APIHost, GitHost: provider.GitHost, SSHUser: provider.SSHUser, SSHPort: port, TokenEnv: provider.TokenEnv.value, CAFile: provider.CAFile.value}
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
