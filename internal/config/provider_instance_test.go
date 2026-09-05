package config

import (
	"strings"
	"testing"
)

func TestDecodeDistinguishesProviderTokenEnvironmentStates(t *testing.T) {
	for _, test := range []struct {
		name      string
		tokenLine string
		wantError string
	}{
		{name: "explicit", tokenLine: "    tokenEnv: REPOWOLF_TOKEN_GITHUB\n"},
		{name: "omitted legacy", tokenLine: ""},
		{name: "empty", tokenLine: "    tokenEnv: \"\"\n", wantError: "tokenEnv"},
		{name: "null", tokenLine: "    tokenEnv: null\n", wantError: "tokenEnv"},
		{name: "non string", tokenLine: "    tokenEnv: 7\n", wantError: "tokenEnv"},
	} {
		t.Run(test.name, func(t *testing.T) {
			yaml := providerYAML("github", test.tokenLine)
			cfg, err := Decode(strings.NewReader(yaml))
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("Decode() error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.name == "explicit" && cfg.Providers["provider"].TokenEnv != "REPOWOLF_TOKEN_GITHUB" {
				t.Fatalf("explicit tokenEnv = %q", cfg.Providers["provider"].TokenEnv)
			}
			if test.tokenLine == "" && cfg.Providers["provider"].TokenEnv != "" {
				t.Fatalf("legacy tokenEnv = %q", cfg.Providers["provider"].TokenEnv)
			}
		})
	}
}

func TestValidateProviderTokenEnvironmentRules(t *testing.T) {
	for _, test := range []struct {
		name      string
		mutate    func(*Config)
		wantError string
	}{
		{
			name: "Gitea omits tokenEnv",
			mutate: func(cfg *Config) {
				provider := cfg.Providers["github"]
				provider.Kind = ProviderGitea
				provider.TokenEnv = ""
				cfg.Providers["github"] = provider
			},
			wantError: "requires tokenEnv",
		},
		{
			name: "two GitHub records omit tokenEnv",
			mutate: func(cfg *Config) {
				provider := cfg.Providers["github"]
				provider.TokenEnv = ""
				cfg.Providers["github"] = provider
				cfg.Providers["github-two"] = provider
			},
			wantError: "legacy",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := validConfig()
			test.mutate(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Validate() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestValidateRejectsDuplicateCredentialNames(t *testing.T) {
	for _, test := range []struct {
		name   string
		first  string
		second string
	}{
		{name: "provider and provider", first: "REPOWOLF_TOKEN_SHARED", second: "REPOWOLF_TOKEN_SHARED"},
		{name: "provider and principal", first: "REPOWOLF_TOKEN_AGENT", second: "REPOWOLF_TOKEN_AGENT"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := validConfig()
			provider := cfg.Providers["github"]
			provider.TokenEnv = test.first
			cfg.Providers["github"] = provider
			if test.name == "provider and provider" {
				provider.TokenEnv = test.second
				cfg.Providers["github-two"] = provider
			} else {
				principal := cfg.Principals["agent"]
				principal.TokenEnvs = []string{test.second}
				cfg.Principals["agent"] = principal
			}
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), test.first) {
				t.Fatalf("Validate() error = %v, want duplicate %q", err, test.first)
			}
		})
	}

	cfg := validConfig()
	principal := cfg.Principals["agent"]
	principal.TokenEnvs = []string{"REPOWOLF_TOKEN_AGENT", "REPOWOLF_TOKEN_AGENT"}
	cfg.Principals["agent"] = principal
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "REPOWOLF_TOKEN_AGENT") {
		t.Fatalf("Validate() error = %v, want duplicate principal token environment", err)
	}
}

func TestValidateGiteaRepositoryNames(t *testing.T) {
	for _, test := range []struct {
		owner      string
		repository string
		valid      bool
	}{
		{owner: "group.with-dot", repository: "repo_name", valid: true},
		{owner: ".", repository: "repo", valid: false},
		{owner: "..", repository: "repo", valid: false},
		{owner: "group", repository: "..", valid: false},
		{owner: "group", repository: "repo.git", valid: false},
	} {
		t.Run(test.owner+"/"+test.repository, func(t *testing.T) {
			cfg := validConfig()
			provider := cfg.Providers["github"]
			provider.Kind = ProviderGitea
			provider.TokenEnv = "REPOWOLF_TOKEN_GITEA"
			cfg.Providers["github"] = provider
			repository := cfg.Repositories["sample-project"]
			repository.Owner = test.owner
			repository.Name = test.repository
			cfg.Repositories["sample-project"] = repository

			err := cfg.Validate()
			if test.valid && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("Validate accepted invalid Gitea repository name")
			}
		})
	}
}

func TestValidateRejectsFoldedGiteaSlugCollisions(t *testing.T) {
	for _, test := range []struct {
		name   string
		second Repository
		setup  func(*Config)
	}{
		{
			name:   "same-record",
			second: Repository{Provider: "github", Owner: "group", Name: "repository", Git: PushPolicy{DenyRefs: []string{"refs/heads/main"}, DenyDeletes: true, MaxRefUpdates: 16}},
		},
		{
			name:   "cross-provider",
			second: Repository{Provider: "gitea-two", Owner: "group", Name: "repository", Git: PushPolicy{DenyRefs: []string{"refs/heads/main"}, DenyDeletes: true, MaxRefUpdates: 16}},
			setup: func(cfg *Config) {
				provider := cfg.Providers["github"]
				provider.TokenEnv = "REPOWOLF_TOKEN_GITEA_TWO"
				cfg.Providers["gitea-two"] = provider
			},
		},
		{
			name:   "case-only",
			second: Repository{Provider: "github", Owner: "GROUP", Name: "REPOSITORY", Git: PushPolicy{DenyRefs: []string{"refs/heads/main"}, DenyDeletes: true, MaxRefUpdates: 16}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := validConfig()
			provider := cfg.Providers["github"]
			provider.Kind = ProviderGitea
			provider.TokenEnv = "REPOWOLF_TOKEN_GITEA"
			cfg.Providers["github"] = provider
			first := cfg.Repositories["sample-project"]
			first.Owner = "group"
			first.Name = "repository"
			cfg.Repositories["sample-project"] = first
			if test.setup != nil {
				test.setup(&cfg)
			}
			cfg.Repositories["duplicate-project"] = test.second

			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), "sample-project") || !strings.Contains(err.Error(), "duplicate-project") {
				t.Fatalf("Validate() error = %v, want both repository IDs", err)
			}
		})
	}
}

func providerYAML(kind, tokenLine string) string {
	return `apiVersion: repowolf.dev/v1alpha1
listen: :8443
tls:
  certificate: /run/repowolf/tls.crt
  privateKey: /run/repowolf/tls.key
providers:
  provider:
    kind: ` + kind + `
    apiHost: github.com
    gitHost: github.com
    sshUser: git
` + tokenLine + `repositories:
  sample-project:
    provider: provider
    owner: alpha
    name: sample-project
    git:
      denyDeletes: true
      maxRefUpdates: 16
principals:
  agent:
    tokenEnvs:
      - REPOWOLF_TOKEN_AGENT
    grants:
      - repository: sample-project
        capabilities:
          - repository:read
          - git:read
          - git:write
`
}
