package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDecodeFixturesAndDefaults(t *testing.T) {
	path := filepath.Join("testdata", "valid.yaml")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })

	cfg, err := Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers["github"].SSHPort != 22 {
		t.Fatalf("SSHPort = %d, want 22", cfg.Providers["github"].SSHPort)
	}
	if got, want := cfg.Repositories["clubhouse"].Git.DenyRefs, []string{"refs/heads/main"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("default DenyRefs = %v, want %v", got, want)
	}
	want := Limits{
		MaxConcurrentRequests:             8,
		MaxConcurrentRequestsPerPrincipal: 4,
		MaxMessageBytes:                   1 << 20,
		MaxStreamChunkBytes:               64 << 10,
		MaxPushPrefixBytes:                1 << 20,
		MaxGitBytesPerDirection:           8 << 30,
		InitialStreamTimeout:              5 * time.Second,
		OperationTimeout:                  10 * time.Minute,
		IdleStreamTimeout:                 2 * time.Minute,
	}
	if !reflect.DeepEqual(cfg.Limits, want) {
		t.Fatalf("Limits = %#v, want %#v", cfg.Limits, want)
	}
}

func TestLoadFile(t *testing.T) {
	cfg, err := LoadFile(filepath.Join("testdata", "valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIVersion != "repowolf.dev/v1alpha1" {
		t.Fatalf("APIVersion = %q", cfg.APIVersion)
	}
}

func TestDecodeNormalizesDurationOverrides(t *testing.T) {
	yaml := strings.Replace(validYAML(), "principals:", "limits:\n  initialStreamTimeout: 7s\n  operationTimeout: 11m\n  idleStreamTimeout: 3m\nprincipals:", 1)
	cfg, err := Decode(strings.NewReader(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.Limits.InitialStreamTimeout, 7*time.Second; got != want {
		t.Fatalf("InitialStreamTimeout = %s, want %s", got, want)
	}
	if got, want := cfg.Limits.OperationTimeout, 11*time.Minute; got != want {
		t.Fatalf("OperationTimeout = %s, want %s", got, want)
	}
	if got, want := cfg.Limits.IdleStreamTimeout, 3*time.Minute; got != want {
		t.Fatalf("IdleStreamTimeout = %s, want %s", got, want)
	}
}

func TestDecodeRejectsMalformedAndNonPositiveDuration(t *testing.T) {
	tests := []struct {
		name   string
		limits string
	}{
		{name: "malformed", limits: "initialStreamTimeout: eventually"},
		{name: "zero", limits: "operationTimeout: 0s"},
		{name: "negative", limits: "idleStreamTimeout: -1s"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			yaml := strings.Replace(validYAML(), "principals:", "limits:\n  "+test.limits+"\nprincipals:", 1)
			if _, err := Decode(strings.NewReader(yaml)); err == nil {
				t.Fatal("Decode accepted invalid duration")
			}
		})
	}
}

func TestDecodeRejectsStrictYAML(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "duplicate key", path: "duplicate-key.yaml", want: "duplicate"},
		{name: "unknown field", path: "unknown-field.yaml", want: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file, err := os.Open(filepath.Join("testdata", test.path))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = file.Close() })

			_, err = Decode(file)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.want) {
				t.Fatalf("Decode error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDecodeRejectsSecondDocumentAndNestedDuplicate(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "second document",
			yaml: "apiVersion: repowolf.dev/v1alpha1\nlisten: :8443\n---\napiVersion: repowolf.dev/v1alpha1\nlisten: :9443\n",
			want: "document",
		},
		{
			name: "nested duplicate",
			yaml: "apiVersion: repowolf.dev/v1alpha1\nlisten: :8443\ntls:\n  certificate: cert\n  certificate: other\n  privateKey: key\nproviders: {}\nrepositories: {}\nprincipals: {}\n",
			want: "duplicate",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(test.yaml))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.want) {
				t.Fatalf("Decode error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDecodeRejectsCyclicAlias(t *testing.T) {
	_, err := Decode(strings.NewReader("root: &cycle [*cycle]\n"))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "cyclic") {
		t.Fatalf("Decode error = %v, want cyclic-alias error", err)
	}
}

func TestDecodeAllowsNonCyclicAlias(t *testing.T) {
	yaml := strings.Replace(validYAML(), "certificate: /run/repowolf/tls.crt\n  privateKey: /run/repowolf/tls.key", "certificate: &certificate /run/repowolf/tls.crt\n  privateKey: *certificate", 1)
	if _, err := Decode(strings.NewReader(yaml)); err != nil {
		t.Fatalf("Decode error = %v, want non-cyclic alias accepted", err)
	}
}

func TestValidateRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"unsupported api version", func(cfg *Config) { cfg.APIVersion = "repowolf.dev/v9" }},
		{"invalid listen address", func(cfg *Config) { cfg.Listen = ":0" }},
		{"unsupported provider kind", func(cfg *Config) {
			provider := cfg.Providers["github"]
			provider.Kind = "gitlab"
			cfg.Providers["github"] = provider
		}},
		{"undefined provider", func(cfg *Config) {
			repo := cfg.Repositories["clubhouse"]
			repo.Provider = "missing"
			cfg.Repositories["clubhouse"] = repo
		}},
		{"undefined repository", func(cfg *Config) {
			principal := cfg.Principals["agent"]
			principal.Grants[0].Repository = "missing"
			cfg.Principals["agent"] = principal
		}},
		{"wildcard repository", func(cfg *Config) {
			repo := cfg.Repositories["clubhouse"]
			repo.Owner = "alpha*"
			cfg.Repositories["clubhouse"] = repo
		}},
		{"duplicate repository grant", func(cfg *Config) {
			principal := cfg.Principals["agent"]
			principal.Grants = append(principal.Grants, principal.Grants[0])
			cfg.Principals["agent"] = principal
		}},
		{"duplicate capability", func(cfg *Config) {
			principal := cfg.Principals["agent"]
			principal.Grants[0].Capabilities = append(principal.Grants[0].Capabilities, RepositoryRead)
			cfg.Principals["agent"] = principal
		}},
		{"git write without read", func(cfg *Config) {
			principal := cfg.Principals["agent"]
			principal.Grants[0].Capabilities = []Capability{GitWrite}
			cfg.Principals["agent"] = principal
		}},
		{"unknown capability", func(cfg *Config) {
			principal := cfg.Principals["agent"]
			principal.Grants[0].Capabilities = []Capability{"admin"}
			cfg.Principals["agent"] = principal
		}},
		{"invalid token environment", func(cfg *Config) {
			principal := cfg.Principals["agent"]
			principal.TokenEnvs = []string{"TOKEN"}
			cfg.Principals["agent"] = principal
		}},
		{"duplicate token environment", func(cfg *Config) {
			cfg.Principals["other"] = Principal{TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT"}, Grants: []Grant{{Repository: "clubhouse", Capabilities: []Capability{RepositoryRead}}}}
		}},
		{"relative gh override", func(cfg *Config) { path := "gh"; cfg.Tools.GH = &path }},
		{"zero ssh port", func(cfg *Config) {
			provider := cfg.Providers["github"]
			provider.SSHPort = 0
			cfg.Providers["github"] = provider
		}},
		{"invalid provider host", func(cfg *Config) {
			provider := cfg.Providers["github"]
			provider.APIHost = "https://github.com"
			cfg.Providers["github"] = provider
		}},
		{"invalid denied ref", func(cfg *Config) {
			repo := cfg.Repositories["clubhouse"]
			repo.Git.DenyRefs = []string{"main"}
			cfg.Repositories["clubhouse"] = repo
		}},
		{"newline in denied ref", func(cfg *Config) {
			repo := cfg.Repositories["clubhouse"]
			repo.Git.DenyRefs = []string{"refs/heads/main\nother"}
			cfg.Repositories["clubhouse"] = repo
		}},
		{"DEL in denied ref", func(cfg *Config) {
			repo := cfg.Repositories["clubhouse"]
			repo.Git.DenyRefs = []string{"refs/heads/main\x7f"}
			cfg.Repositories["clubhouse"] = repo
		}},
		{"zero limit", func(cfg *Config) { cfg.Limits.MaxConcurrentRequests = 0 }},
		{"message limit above hard cap", func(cfg *Config) { cfg.Limits.MaxMessageBytes = 1<<20 + 1 }},
		{"per principal exceeds global", func(cfg *Config) { cfg.Limits.MaxConcurrentRequestsPerPrincipal = cfg.Limits.MaxConcurrentRequests + 1 }},
		{"stream chunk exceeds message", func(cfg *Config) { cfg.Limits.MaxMessageBytes = cfg.Limits.MaxStreamChunkBytes - 1 }},
		{"push prefix exceeds git limit", func(cfg *Config) { cfg.Limits.MaxGitBytesPerDirection = int64(cfg.Limits.MaxPushPrefixBytes - 1) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validConfig()
			test.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate accepted invalid configuration")
			}
		})
	}
}

func TestValidateRejectsControlCharactersInRef(t *testing.T) {
	controls := make([]rune, 0, 0x21)
	for control := rune(0); control <= 0x1f; control++ {
		controls = append(controls, control)
	}
	controls = append(controls, 0x7f)

	for _, control := range controls {
		t.Run("control", func(t *testing.T) {
			cfg := validConfig()
			repo := cfg.Repositories["clubhouse"]
			repo.Git.DenyRefs = []string{"refs/heads/main" + string(control)}
			cfg.Repositories["clubhouse"] = repo
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate accepted ref containing control byte 0x%02x", control)
			}
		})
	}
}

func TestValidateDoesNotMutateConfig(t *testing.T) {
	cfg := validConfig()
	want := validConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("Validate mutated config: got %#v, want %#v", cfg, want)
	}
}

func TestRepositoryReturnsIndependentCopy(t *testing.T) {
	cfg := validConfig()
	repository, ok := cfg.Repository("clubhouse")
	if !ok {
		t.Fatal("Repository did not find clubhouse")
	}
	repository.Git.DenyRefs[0] = "refs/heads/other"
	if got := cfg.Repositories["clubhouse"].Git.DenyRefs[0]; got != "refs/heads/main" {
		t.Fatalf("config repository changed through lookup: %q", got)
	}
}

func TestDecodeDefaultsDoNotAliasRepositoryPolicies(t *testing.T) {
	yaml := strings.ReplaceAll(validYAML(), "repositories:\n  clubhouse:", "repositories:\n  other:\n    provider: github\n    owner: beta\n    name: other\n    git:\n      denyDeletes: true\n      maxRefUpdates: 16\n  clubhouse:")
	cfg, err := Decode(strings.NewReader(yaml))
	if err != nil {
		t.Fatal(err)
	}
	first := cfg.Repositories["clubhouse"]
	first.Git.DenyRefs[0] = "refs/heads/other"
	cfg.Repositories["clubhouse"] = first
	if got := cfg.Repositories["other"].Git.DenyRefs[0]; got != "refs/heads/main" {
		t.Fatalf("default policies alias: %q", got)
	}
}

func validConfig() Config {
	return Config{
		APIVersion: "repowolf.dev/v1alpha1",
		Listen:     ":8443",
		TLS:        TLS{Certificate: "/run/repowolf/tls.crt", PrivateKey: "/run/repowolf/tls.key"},
		Providers: map[string]Provider{
			"github": {Kind: ProviderGitHub, APIHost: "github.com", GitHost: "github.com", SSHUser: "git", SSHPort: 22},
		},
		Repositories: map[string]Repository{
			"clubhouse": {Provider: "github", Owner: "alpha", Name: "clubhouse", Git: PushPolicy{DenyRefs: []string{"refs/heads/main"}, DenyDeletes: true, MaxRefUpdates: 16}},
		},
		Principals: map[string]Principal{
			"agent": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT"}, Grants: []Grant{{Repository: "clubhouse", Capabilities: []Capability{RepositoryRead, GitRead, GitWrite}}}},
		},
		Limits: Limits{MaxConcurrentRequests: 8, MaxConcurrentRequestsPerPrincipal: 4, MaxMessageBytes: 1 << 20, MaxStreamChunkBytes: 64 << 10, MaxPushPrefixBytes: 1 << 20, MaxGitBytesPerDirection: 8 << 30, InitialStreamTimeout: 5 * time.Second, OperationTimeout: 10 * time.Minute, IdleStreamTimeout: 2 * time.Minute},
	}
}

func validYAML() string {
	return `apiVersion: repowolf.dev/v1alpha1
listen: :8443
tls:
  certificate: /run/repowolf/tls.crt
  privateKey: /run/repowolf/tls.key
providers:
  github:
    kind: github
    apiHost: github.com
    gitHost: github.com
    sshUser: git
repositories:
  clubhouse:
    provider: github
    owner: alpha
    name: clubhouse
    git:
      denyDeletes: true
      maxRefUpdates: 16
principals:
  agent:
    tokenEnvs:
      - REPOWOLF_TOKEN_AGENT
    grants:
      - repository: clubhouse
        capabilities:
          - repository:read
          - git:read
          - git:write
`
}
