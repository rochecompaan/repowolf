package credentials_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/credentials"
)

type countingLookup struct {
	values map[string]string
	calls  map[string]int
	order  []string
}

func (lookup *countingLookup) get(name string) (string, bool) {
	lookup.calls[name]++
	lookup.order = append(lookup.order, name)
	value, ok := lookup.values[name]
	return value, ok
}

func TestLoadReadsEachNamedSourceOnce(t *testing.T) {
	lookup := newCountingLookup(map[string]string{
		"REPOWOLF_TOKEN_AGENT_A_SECOND": testToken(1),
		"REPOWOLF_TOKEN_AGENT_A_FIRST":  testToken(2),
		"REPOWOLF_TOKEN_AGENT_B":        testToken(3),
		"REPOWOLF_TOKEN_GITEA":          "gitea-token",
		"REPOWOLF_TOKEN_GITHUB":         "github-token",
		"GH_TOKEN":                      "legacy-token",
	})
	cfg := config.Config{
		Principals: map[string]config.Principal{
			"agent-b": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT_B"}},
			"agent-a": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT_A_SECOND", "REPOWOLF_TOKEN_AGENT_A_FIRST"}},
		},
		Providers: map[string]config.Provider{
			"github-legacy":   {Kind: config.ProviderGitHub},
			"github-explicit": {Kind: config.ProviderGitHub, TokenEnv: "REPOWOLF_TOKEN_GITHUB"},
			"gitea":           {Kind: config.ProviderGitea, TokenEnv: "REPOWOLF_TOKEN_GITEA"},
		},
	}

	if _, err := credentials.Load(cfg, lookup.get); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"REPOWOLF_TOKEN_AGENT_A_SECOND",
		"REPOWOLF_TOKEN_AGENT_A_FIRST",
		"REPOWOLF_TOKEN_AGENT_B",
		"REPOWOLF_TOKEN_GITEA",
		"REPOWOLF_TOKEN_GITHUB",
		"GH_TOKEN",
		"GITHUB_TOKEN",
	}
	if !reflect.DeepEqual(lookup.order, want) {
		t.Fatalf("lookup order = %v, want %v", lookup.order, want)
	}
	assertCallsOnce(t, lookup, want...)
}

func TestLoadReturnsCopiedSortedEnvironmentNames(t *testing.T) {
	lookup := newCountingLookup(map[string]string{
		"REPOWOLF_TOKEN_AGENT":  testToken(1),
		"REPOWOLF_TOKEN_GITEA":  "gitea-token",
		"REPOWOLF_TOKEN_GITHUB": "github-token",
	})
	cfg := config.Config{
		Principals: map[string]config.Principal{"agent": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT"}}},
		Providers: map[string]config.Provider{
			"github": {Kind: config.ProviderGitHub, TokenEnv: "REPOWOLF_TOKEN_GITHUB"},
			"gitea":  {Kind: config.ProviderGitea, TokenEnv: "REPOWOLF_TOKEN_GITEA"},
		},
	}

	snapshot, err := credentials.Load(cfg, lookup.get)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"REPOWOLF_TOKEN_AGENT", "REPOWOLF_TOKEN_GITEA", "REPOWOLF_TOKEN_GITHUB"}
	if got := snapshot.EnvironmentNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("EnvironmentNames() = %v, want %v", got, want)
	}
	names := snapshot.EnvironmentNames()
	names[0] = "changed"
	if got := snapshot.EnvironmentNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("EnvironmentNames() after mutation = %v, want %v", got, want)
	}
	assertCallsOnce(t, lookup, want...)
}

func TestLoadBindsProviderTokensByProviderID(t *testing.T) {
	lookup := newCountingLookup(map[string]string{
		"REPOWOLF_TOKEN_AGENT":  testToken(1),
		"REPOWOLF_TOKEN_GITEA":  "gitea-token",
		"REPOWOLF_TOKEN_GITHUB": "github-token",
	})
	cfg := config.Config{
		Principals: map[string]config.Principal{"agent": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT"}}},
		Providers: map[string]config.Provider{
			"github": {Kind: config.ProviderGitHub, TokenEnv: "REPOWOLF_TOKEN_GITHUB"},
			"gitea":  {Kind: config.ProviderGitea, TokenEnv: "REPOWOLF_TOKEN_GITEA"},
		},
	}

	snapshot, err := credentials.Load(cfg, lookup.get)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"github": "github-token", "gitea": "gitea-token"} {
		if got, ok := snapshot.ProviderToken(id); !ok || got != want {
			t.Fatalf("ProviderToken(%q) = %q, %v, want %q, true", id, got, ok, want)
		}
	}
	if _, ok := snapshot.ProviderToken("missing"); ok {
		t.Fatal("ProviderToken() found an unconfigured provider")
	}
	if principal, ok := snapshot.AuthIndex().Authenticate(testToken(1)); !ok || principal != "agent" {
		t.Fatalf("AuthIndex().Authenticate() = %q, %v, want agent, true", principal, ok)
	}
}

func TestLoadRejectsMissingAndEmptyProviderTokensWithoutDisclosure(t *testing.T) {
	for _, test := range []struct {
		name   string
		values map[string]string
		secret string
	}{
		{name: "missing", values: map[string]string{}},
		{name: "empty", values: map[string]string{"REPOWOLF_TOKEN_GITEA": ""}},
	} {
		t.Run(test.name, func(t *testing.T) {
			lookup := newCountingLookup(test.values)
			cfg := config.Config{Providers: map[string]config.Provider{"gitea": {Kind: config.ProviderGitea, TokenEnv: "REPOWOLF_TOKEN_GITEA"}}}
			_, err := credentials.Load(cfg, lookup.get)
			assertSafeCredentialError(t, err, "REPOWOLF_TOKEN_GITEA", test.secret)
			assertCallsOnce(t, lookup, "REPOWOLF_TOKEN_GITEA")
		})
	}
}

func TestLoadRejectsDuplicateValuesAcrossEveryCredentialCategory(t *testing.T) {
	principalToken := testToken(9)
	for _, test := range []struct {
		name        string
		cfg         config.Config
		values      map[string]string
		sources     []string
		errorSource string
		secret      string
	}{
		{
			name:    "within one principal",
			cfg:     config.Config{Principals: map[string]config.Principal{"agent": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT_A", "REPOWOLF_TOKEN_AGENT_B"}}}},
			values:  map[string]string{"REPOWOLF_TOKEN_AGENT_A": testToken(1), "REPOWOLF_TOKEN_AGENT_B": testToken(1)},
			sources: []string{"REPOWOLF_TOKEN_AGENT_A", "REPOWOLF_TOKEN_AGENT_B"}, errorSource: "REPOWOLF_TOKEN_AGENT_B", secret: testToken(1),
		},
		{
			name:    "across principals",
			cfg:     config.Config{Principals: map[string]config.Principal{"agent-a": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT_A"}}, "agent-b": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT_B"}}}},
			values:  map[string]string{"REPOWOLF_TOKEN_AGENT_A": testToken(2), "REPOWOLF_TOKEN_AGENT_B": testToken(2)},
			sources: []string{"REPOWOLF_TOKEN_AGENT_A", "REPOWOLF_TOKEN_AGENT_B"}, errorSource: "REPOWOLF_TOKEN_AGENT_B", secret: testToken(2),
		},
		{
			name:    "across explicit providers",
			cfg:     config.Config{Providers: map[string]config.Provider{"gitea": {Kind: config.ProviderGitea, TokenEnv: "REPOWOLF_TOKEN_GITEA"}, "github": {Kind: config.ProviderGitHub, TokenEnv: "REPOWOLF_TOKEN_GITHUB"}}},
			values:  map[string]string{"REPOWOLF_TOKEN_GITEA": "provider-secret", "REPOWOLF_TOKEN_GITHUB": "provider-secret"},
			sources: []string{"REPOWOLF_TOKEN_GITEA", "REPOWOLF_TOKEN_GITHUB"}, errorSource: "REPOWOLF_TOKEN_GITHUB", secret: "provider-secret",
		},
		{
			name:    "principal and explicit provider",
			cfg:     config.Config{Principals: map[string]config.Principal{"agent": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT"}}}, Providers: map[string]config.Provider{"gitea": {Kind: config.ProviderGitea, TokenEnv: "REPOWOLF_TOKEN_GITEA"}}},
			values:  map[string]string{"REPOWOLF_TOKEN_AGENT": principalToken, "REPOWOLF_TOKEN_GITEA": principalToken},
			sources: []string{"REPOWOLF_TOKEN_AGENT", "REPOWOLF_TOKEN_GITEA"}, errorSource: "REPOWOLF_TOKEN_GITEA", secret: principalToken,
		},
		{
			name:    "explicit and legacy provider",
			cfg:     config.Config{Providers: map[string]config.Provider{"gitea": {Kind: config.ProviderGitea, TokenEnv: "REPOWOLF_TOKEN_GITEA"}, "github": {Kind: config.ProviderGitHub}}},
			values:  map[string]string{"REPOWOLF_TOKEN_GITEA": "provider-secret", "GH_TOKEN": "provider-secret"},
			sources: []string{"REPOWOLF_TOKEN_GITEA", "GH_TOKEN", "GITHUB_TOKEN"}, errorSource: "GH_TOKEN", secret: "provider-secret",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			lookup := newCountingLookup(test.values)
			_, err := credentials.Load(test.cfg, lookup.get)
			assertSafeCredentialError(t, err, test.errorSource, test.secret)
			for _, source := range test.sources[:2] {
				if !strings.Contains(err.Error(), source) {
					t.Fatalf("Load() error = %v, want source %q", err, source)
				}
			}
			assertCallsOnce(t, lookup, test.sources...)
		})
	}
}

func TestLoadLegacyGitHubTokenMatrix(t *testing.T) {
	for _, test := range []struct {
		name       string
		values     map[string]string
		wantSource string
		wantError  bool
	}{
		{name: "GH_TOKEN only", values: map[string]string{"GH_TOKEN": "gh-token"}, wantSource: "GH_TOKEN"},
		{name: "GITHUB_TOKEN only", values: map[string]string{"GITHUB_TOKEN": "github-token"}, wantSource: "GITHUB_TOKEN"},
		{name: "both absent", values: map[string]string{}, wantError: true},
		{name: "both present with one empty", values: map[string]string{"GH_TOKEN": "gh-token", "GITHUB_TOKEN": ""}, wantError: true},
		{name: "GH_TOKEN empty", values: map[string]string{"GH_TOKEN": ""}, wantError: true},
		{name: "GITHUB_TOKEN empty", values: map[string]string{"GITHUB_TOKEN": ""}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			lookup := newCountingLookup(test.values)
			cfg := config.Config{Providers: map[string]config.Provider{"github": {Kind: config.ProviderGitHub}}}
			snapshot, err := credentials.Load(cfg, lookup.get)
			if test.wantError {
				assertSafeCredentialError(t, err, "TOKEN", "gh-token")
			} else if err != nil {
				t.Fatal(err)
			} else {
				if got, ok := snapshot.ProviderToken("github"); !ok || got != test.values[test.wantSource] {
					t.Fatalf("ProviderToken(github) = %q, %v", got, ok)
				}
				if got := snapshot.EnvironmentNames(); !reflect.DeepEqual(got, []string{test.wantSource}) {
					t.Fatalf("EnvironmentNames() = %v, want [%s]", got, test.wantSource)
				}
			}
			assertCallsOnce(t, lookup, "GH_TOKEN", "GITHUB_TOKEN")
		})
	}
}

func TestLoadReportsLegacyProvenanceWithoutReturningAnotherToken(t *testing.T) {
	lookup := newCountingLookup(map[string]string{"GH_TOKEN": "legacy-token"})
	cfg := config.Config{Providers: map[string]config.Provider{"github": {Kind: config.ProviderGitHub}}}

	snapshot, err := credentials.Load(cfg, lookup.get)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.UsesLegacyProviderToken("github") {
		t.Fatal("UsesLegacyProviderToken(github) = false, want true")
	}
	if snapshot.UsesLegacyProviderToken("missing") {
		t.Fatal("UsesLegacyProviderToken(missing) = true")
	}
	if got, ok := snapshot.ProviderToken("github"); !ok || got != "legacy-token" {
		t.Fatalf("ProviderToken(github) = %q, %v", got, ok)
	}
	if got := snapshot.EnvironmentNames(); !reflect.DeepEqual(got, []string{"GH_TOKEN"}) {
		t.Fatalf("EnvironmentNames() = %v, want [GH_TOKEN]", got)
	}
	assertCallsOnce(t, lookup, "GH_TOKEN", "GITHUB_TOKEN")
}

func newCountingLookup(values map[string]string) *countingLookup {
	return &countingLookup{values: values, calls: make(map[string]int)}
}

func assertCallsOnce(t *testing.T, lookup *countingLookup, names ...string) {
	t.Helper()
	for _, name := range names {
		if got := lookup.calls[name]; got != 1 {
			t.Errorf("lookup.calls[%q] = %d, want 1", name, got)
		}
	}
}

func assertSafeCredentialError(t *testing.T, err error, source, secret string) {
	t.Helper()
	if err == nil {
		t.Fatal("Load() returned nil error")
	}
	if !strings.Contains(err.Error(), source) {
		t.Fatalf("Load() error = %v, want source %q", err, source)
	}
	if secret != "" && strings.Contains(err.Error(), secret) {
		t.Fatal("Load() error disclosed a credential")
	}
	if secret != "" {
		digest := sha256.Sum256([]byte(secret))
		if strings.Contains(err.Error(), hex.EncodeToString(digest[:])) {
			t.Fatal("Load() error disclosed a credential digest")
		}
	}
}

func testToken(byteValue byte) string {
	return "rw1_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byteValue}, 32))
}
