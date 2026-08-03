package policy

import (
	"errors"
	"testing"

	"github.com/rochecompaan/repowolf/internal/config"
)

func TestResolveExactGrantedRepository(t *testing.T) {
	snapshot := testSnapshot(t)

	resolved, err := snapshot.Resolve("infra-agent", Selector{
		Kind: config.ProviderGitHub, Host: "github.com", SSHPort: 22, Owner: "alpha", Name: "clubhouse",
	}, config.RepositoryRead)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != "clubhouse" {
		t.Fatalf("ID = %q, want clubhouse", resolved.ID)
	}
	if resolved.Repository.Owner != "alpha" || resolved.Repository.Name != "clubhouse" {
		t.Fatalf("Repository = %#v", resolved.Repository)
	}
	if resolved.Provider.GitHost != "github.com" || resolved.Provider.SSHPort != 22 {
		t.Fatalf("Provider = %#v", resolved.Provider)
	}
}

func TestResolveMultiRepositoryPrincipal(t *testing.T) {
	snapshot := testSnapshot(t)

	for _, selector := range []Selector{
		{Host: "github.com", Owner: "alpha", Name: "clubhouse"},
		{Host: "ssh.example.test", Owner: "ops", Name: "tools"},
	} {
		if _, err := snapshot.Resolve("infra-agent", selector, config.RepositoryRead); err != nil {
			t.Fatalf("Resolve(%#v) error = %v", selector, err)
		}
	}
}

func TestResolveNonDefaultPortAllowsUnspecifiedOrExact(t *testing.T) {
	snapshot := testSnapshot(t)

	for _, port := range []uint16{0, 2222} {
		resolved, err := snapshot.Resolve("infra-agent", Selector{
			Host: "ssh.example.test", SSHPort: port, Owner: "ops", Name: "tools",
		}, config.RepositoryRead)
		if err != nil {
			t.Fatalf("Resolve(port=%d) error = %v", port, err)
		}
		if resolved.Provider.SSHPort != 2222 {
			t.Fatalf("resolved port = %d, want configured port 2222", resolved.Provider.SSHPort)
		}
	}
}

func TestResolveRejectsEveryMismatchedSelectorField(t *testing.T) {
	snapshot := testSnapshot(t)

	selectors := []Selector{
		{Kind: "other", Host: "github.com", Owner: "alpha", Name: "clubhouse"},
		{Host: "wrong.example.test", Owner: "alpha", Name: "clubhouse"},
		{Host: "github.com", SSHPort: 2222, Owner: "alpha", Name: "clubhouse"},
		{Host: "github.com", Owner: "other", Name: "clubhouse"},
		{Host: "github.com", Owner: "alpha", Name: "other"},
	}
	for _, selector := range selectors {
		_, err := snapshot.Resolve("infra-agent", selector, config.RepositoryRead)
		if !errors.Is(err, ErrDenied) {
			t.Fatalf("Resolve(%#v) error = %v, want ErrDenied", selector, err)
		}
	}
}

func TestResolveDeniesMissingCapabilitiesAndUnknownPrincipal(t *testing.T) {
	snapshot := testSnapshot(t)
	selector := Selector{Host: "ssh.example.test", Owner: "ops", Name: "tools"}

	for _, principal := range []string{"infra-agent", "unknown"} {
		_, err := snapshot.Resolve(principal, selector, config.GitWrite)
		if !errors.Is(err, ErrDenied) {
			t.Fatalf("Resolve(%q) error = %v, want ErrDenied", principal, err)
		}
	}
	if _, err := snapshot.Resolve("infra-agent", selector, config.GitRead); err != nil {
		t.Fatalf("Resolve(git:read) error = %v", err)
	}
	if _, err := snapshot.Resolve("infra-agent", Selector{Host: "github.com", Owner: "alpha", Name: "clubhouse"}, config.GitWrite); err != nil {
		t.Fatalf("Resolve(git:write) error = %v", err)
	}
}

func TestUnauthorizedAndUnknownRepositoryAreIndistinguishable(t *testing.T) {
	snapshot := testSnapshot(t)
	unknownSelector := Selector{Kind: config.ProviderGitHub, Host: "github.com", Owner: "none", Name: "missing"}
	unauthorizedSelector := Selector{Kind: config.ProviderGitHub, Host: "github.com", Owner: "other", Name: "private"}

	_, unknown := snapshot.Resolve("infra-agent", unknownSelector, config.RepositoryRead)
	_, unauthorized := snapshot.Resolve("infra-agent", unauthorizedSelector, config.RepositoryRead)
	if !errors.Is(unknown, ErrDenied) || !errors.Is(unauthorized, ErrDenied) {
		t.Fatalf("unknown=%v unauthorized=%v", unknown, unauthorized)
	}
	if unknown.Error() != unauthorized.Error() {
		t.Fatalf("errors enumerate policy: %q != %q", unknown, unauthorized)
	}
}

func TestResolveDeniesAmbiguousSelector(t *testing.T) {
	snapshot := testSnapshot(t)

	_, err := snapshot.Resolve("infra-agent", Selector{}, config.RepositoryRead)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("Resolve() error = %v, want ErrDenied", err)
	}
}

func TestSnapshotCopiesConfigurationAndResolution(t *testing.T) {
	cfg := testConfig()
	snapshot, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	repository := cfg.Repositories["clubhouse"]
	repository.Git.DenyRefs[0] = "refs/heads/replaced"
	cfg.Repositories["clubhouse"] = repository

	selector := Selector{Host: "github.com", Owner: "alpha", Name: "clubhouse"}
	first, err := snapshot.Resolve("infra-agent", selector, config.RepositoryRead)
	if err != nil {
		t.Fatal(err)
	}
	first.Repository.Git.DenyRefs[0] = "refs/heads/changed"
	second, err := snapshot.Resolve("infra-agent", selector, config.RepositoryRead)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := second.Repository.Git.DenyRefs[0], "refs/heads/main"; got != want {
		t.Fatalf("DenyRefs[0] = %q, want %q", got, want)
	}
}

func TestNewRejectsUnknownRepositoryReferences(t *testing.T) {
	cfg := testConfig()
	cfg.Repositories["clubhouse"] = config.Repository{Provider: "missing", Owner: "alpha", Name: "clubhouse"}

	_, err := New(cfg)
	if !errors.Is(err, ErrRepository) {
		t.Fatalf("New() error = %v, want ErrRepository", err)
	}
}

func testSnapshot(t *testing.T) *Snapshot {
	t.Helper()
	snapshot, err := New(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func testConfig() config.Config {
	return config.Config{
		Providers: map[string]config.Provider{
			"github": {Kind: config.ProviderGitHub, APIHost: "api.github.com", GitHost: "github.com", SSHUser: "git", SSHPort: 22},
			"forge":  {Kind: config.ProviderGitHub, APIHost: "api.example.test", GitHost: "ssh.example.test", SSHUser: "git", SSHPort: 2222},
		},
		Repositories: map[string]config.Repository{
			"clubhouse": {Provider: "github", Owner: "alpha", Name: "clubhouse", Git: config.PushPolicy{DenyRefs: []string{"refs/heads/main"}, DenyDeletes: true, MaxRefUpdates: 16}},
			"tools":     {Provider: "forge", Owner: "ops", Name: "tools", Git: config.PushPolicy{DenyRefs: []string{"refs/heads/main"}, MaxRefUpdates: 16}},
			"private":   {Provider: "github", Owner: "other", Name: "private", Git: config.PushPolicy{DenyRefs: []string{"refs/heads/main"}, MaxRefUpdates: 16}},
		},
		Principals: map[string]config.Principal{
			"infra-agent": {Grants: []config.Grant{
				{Repository: "clubhouse", Capabilities: []config.Capability{config.RepositoryRead, config.GitRead, config.GitWrite}},
				{Repository: "tools", Capabilities: []config.Capability{config.RepositoryRead, config.GitRead}},
			}},
			"other-agent": {Grants: []config.Grant{{Repository: "private", Capabilities: []config.Capability{config.RepositoryRead}}}},
		},
	}
}
