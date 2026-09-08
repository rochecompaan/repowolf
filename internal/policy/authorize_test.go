package policy

import (
	"errors"
	"testing"

	"github.com/rochecompaan/repowolf/internal/config"
)

func TestResolveExactGrantedRepository(t *testing.T) {
	snapshot := testSnapshot(t)

	resolved, err := snapshot.Resolve("infra-agent", Selector{
		Kind: config.ProviderGitHub, Host: "github.com", SSHPort: 22, Owner: "alpha", Name: "sample-project",
	}, config.RepositoryRead)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != "sample-project" {
		t.Fatalf("ID = %q, want sample-project", resolved.ID)
	}
	if resolved.Repository.Owner != "alpha" || resolved.Repository.Name != "sample-project" {
		t.Fatalf("Repository = %#v", resolved.Repository)
	}
	if resolved.Provider.GitHost != "github.com" || resolved.Provider.SSHPort != 22 {
		t.Fatalf("Provider = %#v", resolved.Provider)
	}
}

func TestResolveGitProviderAwareAuthority(t *testing.T) {
	snapshot := testSnapshot(t)

	resolved, err := snapshot.ResolveGit("infra-agent", GitSelector{
		SSHUser: "forge_user", Host: "gitea.example", SSHPort: 2222,
		Owner: "team_name", Name: "repo.one",
	}, config.GitRead)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != "gitea-read" || resolved.Provider.Kind != config.ProviderGitea || resolved.Repository.Owner != "Team_Name" || resolved.Repository.Name != "Repo.One" {
		t.Fatalf("resolved = %#v", resolved)
	}

	for _, selector := range []GitSelector{
		{SSHUser: "forge_user", Host: "GITEA.EXAMPLE", Owner: "TEAM_NAME", Name: "REPO.ONE"},
		{SSHUser: "git", Host: "GITHUB.COM", Owner: "alpha", Name: "sample-project"},
		{Host: "github.com", Owner: "alpha", Name: "sample-project"},
	} {
		if _, err := snapshot.ResolveGit("infra-agent", selector, config.GitRead); err != nil {
			t.Fatalf("ResolveGit(%#v): %v", selector, err)
		}
	}
}

func TestResolveGitDenialsAndLegacyUserCompatibility(t *testing.T) {
	snapshot := testSnapshot(t)
	denied := []GitSelector{
		{SSHUser: "git", Host: "gitea.example", Owner: "Team_Name", Name: "Repo.One"},
		{SSHUser: "forge_user", Host: "gitea.example", SSHPort: 22, Owner: "Team_Name", Name: "Repo.One"},
		{Host: "gitea.example", Owner: "Team_Name", Name: "Repo.One"},
		{SSHUser: "git", Host: "github.com", Owner: "ALPHA", Name: "sample-project"},
	}
	for _, selector := range denied {
		if _, err := snapshot.ResolveGit("infra-agent", selector, config.GitRead); !errors.Is(err, ErrDenied) {
			t.Fatalf("ResolveGit(%#v) = %v, want denied", selector, err)
		}
	}
	if _, err := snapshot.ResolveGit("infra-agent", GitSelector{SSHUser: "forge_user", Host: "gitea.example", Owner: "Team_Name", Name: "Repo.One"}, config.GitWrite); !errors.Is(err, ErrDenied) {
		t.Fatalf("missing capability = %v, want denied", err)
	}
}

func TestResolveGitDeniesAmbiguousCoordinates(t *testing.T) {
	cfg := testConfig()
	cfg.Repositories["gitea-copy"] = config.Repository{Provider: "gitea", Owner: "TEAM_NAME", Name: "REPO.ONE"}
	principal := cfg.Principals["infra-agent"]
	principal.Grants = append(principal.Grants, config.Grant{Repository: "gitea-copy", Capabilities: []config.Capability{config.GitRead}})
	cfg.Principals["infra-agent"] = principal
	snapshot, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = snapshot.ResolveGit("infra-agent", GitSelector{SSHUser: "forge_user", Host: "gitea.example", Owner: "team_name", Name: "repo.one"}, config.GitRead)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("ambiguous ResolveGit = %v, want denied", err)
	}
}

func TestResolveGitRejectsUnicodeCaseFoldConfusables(t *testing.T) {
	cfg := testConfig()
	cfg.Repositories["kelvin"] = config.Repository{Provider: "gitea", Owner: "Kelvin", Name: "Repo"}
	principal := cfg.Principals["infra-agent"]
	principal.Grants = append(principal.Grants, config.Grant{Repository: "kelvin", Capabilities: []config.Capability{config.GitRead}})
	cfg.Principals["infra-agent"] = principal
	snapshot, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	_, err = snapshot.ResolveGit("infra-agent", GitSelector{
		SSHUser: "forge_user", Host: "gitea.example", Owner: "Kelvin", Name: "Repo",
	}, config.GitRead)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("Unicode-confusable ResolveGit = %v, want denied", err)
	}
}

func TestResolveMultiRepositoryPrincipal(t *testing.T) {
	snapshot := testSnapshot(t)

	for _, selector := range []Selector{
		{Host: "github.com", Owner: "alpha", Name: "sample-project"},
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
		{Kind: "other", Host: "github.com", Owner: "alpha", Name: "sample-project"},
		{Host: "wrong.example.test", Owner: "alpha", Name: "sample-project"},
		{Host: "github.com", SSHPort: 2222, Owner: "alpha", Name: "sample-project"},
		{Host: "github.com", Owner: "other", Name: "sample-project"},
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
	if _, err := snapshot.Resolve("infra-agent", Selector{Host: "github.com", Owner: "alpha", Name: "sample-project"}, config.GitWrite); err != nil {
		t.Fatalf("Resolve(git:write) error = %v", err)
	}
}

func TestUnauthorizedUnknownAndWrongPortAreIndistinguishable(t *testing.T) {
	snapshot := testSnapshot(t)
	unknownSelector := Selector{Kind: config.ProviderGitHub, Host: "github.com", Owner: "none", Name: "missing"}
	unauthorizedSelector := Selector{Kind: config.ProviderGitHub, Host: "github.com", Owner: "other", Name: "private"}
	wrongPortSelector := Selector{Kind: config.ProviderGitHub, Host: "ssh.example.test", SSHPort: 22, Owner: "ops", Name: "tools"}

	_, unknown := snapshot.Resolve("infra-agent", unknownSelector, config.RepositoryRead)
	_, unauthorized := snapshot.Resolve("infra-agent", unauthorizedSelector, config.RepositoryRead)
	_, wrongPort := snapshot.Resolve("infra-agent", wrongPortSelector, config.RepositoryRead)
	if !errors.Is(unknown, ErrDenied) || !errors.Is(unauthorized, ErrDenied) || !errors.Is(wrongPort, ErrDenied) {
		t.Fatalf("unknown=%v unauthorized=%v wrongPort=%v", unknown, unauthorized, wrongPort)
	}
	if unknown.Error() != unauthorized.Error() || unknown.Error() != wrongPort.Error() {
		t.Fatalf("errors enumerate policy: unknown=%q unauthorized=%q wrongPort=%q", unknown.Error(), unauthorized.Error(), wrongPort.Error())
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

	repository := cfg.Repositories["sample-project"]
	repository.Git.DenyRefs[0] = "refs/heads/replaced"
	cfg.Repositories["sample-project"] = repository

	selector := Selector{Host: "github.com", Owner: "alpha", Name: "sample-project"}
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
	cfg.Repositories["sample-project"] = config.Repository{Provider: "missing", Owner: "alpha", Name: "sample-project"}

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
			"gitea":  {Kind: config.ProviderGitea, APIHost: "gitea.example", GitHost: "Gitea.Example", SSHUser: "forge_user", SSHPort: 2222},
		},
		Repositories: map[string]config.Repository{
			"sample-project": {Provider: "github", Owner: "alpha", Name: "sample-project", Git: config.PushPolicy{DenyRefs: []string{"refs/heads/main"}, DenyDeletes: true, MaxRefUpdates: 16}},
			"tools":          {Provider: "forge", Owner: "ops", Name: "tools", Git: config.PushPolicy{DenyRefs: []string{"refs/heads/main"}, MaxRefUpdates: 16}},
			"private":        {Provider: "github", Owner: "other", Name: "private", Git: config.PushPolicy{DenyRefs: []string{"refs/heads/main"}, MaxRefUpdates: 16}},
			"gitea-read":     {Provider: "gitea", Owner: "Team_Name", Name: "Repo.One", Git: config.PushPolicy{MaxRefUpdates: 16}},
		},
		Principals: map[string]config.Principal{
			"infra-agent": {Grants: []config.Grant{
				{Repository: "sample-project", Capabilities: []config.Capability{config.RepositoryRead, config.GitRead, config.GitWrite}},
				{Repository: "tools", Capabilities: []config.Capability{config.RepositoryRead, config.GitRead}},
				{Repository: "gitea-read", Capabilities: []config.Capability{config.GitRead}},
			}},
			"other-agent": {Grants: []config.Grant{{Repository: "private", Capabilities: []config.Capability{config.RepositoryRead}}}},
		},
	}
}
