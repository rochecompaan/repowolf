package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	sdk "code.gitea.io/sdk/gitea"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/credentials"
	"github.com/rochecompaan/repowolf/internal/policy"
	providergithub "github.com/rochecompaan/repowolf/internal/provider/github"
	"github.com/rochecompaan/repowolf/internal/runner"
)

type recordingCaller struct {
	commands []runner.Command
	result   runner.Result
}

func (caller *recordingCaller) Call(_ context.Context, command runner.Command) (runner.Result, error) {
	caller.commands = append(caller.commands, command)
	return caller.result, nil
}

func TestBuildProviderInstancesCreatesIsolatedGitHubAdapters(t *testing.T) {
	cfg, snapshot := providerInstanceFixture(t)
	tokenFree := []string{"SAFE=value"}
	caller := &recordingCaller{result: repositoryViewResult()}

	var constructed []string
	constructor := func(provider config.Provider, token string) (*sdk.Client, error) {
		constructed = append(constructed, provider.APIHost+"="+token)
		return &sdk.Client{}, nil
	}
	instances, err := buildProviderInstances(cfg, snapshot, runner.Toolset{GH: "/pinned/gh"}, tokenFree, caller, constructor)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 4 {
		t.Fatalf("len(instances) = %d, want 4", len(instances))
	}
	if instances["github-a"].github == nil || instances["github-b"].github == nil {
		t.Fatalf("GitHub instances = %#v, want adapters", instances)
	}
	if instances["gitea-lab"].github != nil || instances["gitea-two"].github != nil {
		t.Fatal("Gitea instance unexpectedly has a GitHub adapter")
	}
	if instances["github-a"].gitea != nil || instances["github-b"].gitea != nil {
		t.Fatal("GitHub instance unexpectedly has a Gitea client")
	}
	if instances["gitea-lab"].gitea == nil || instances["gitea-two"].gitea == nil || instances["gitea-lab"].gitea == instances["gitea-two"].gitea {
		t.Fatal("Gitea instances did not retain distinct clients")
	}
	if want := []string{"gitea.invalid=gitea-secret", "gitea-two.invalid=gitea-two-secret"}; !reflect.DeepEqual(constructed, want) {
		t.Fatalf("Gitea construction = %q, want %q", constructed, want)
	}
	if instances["github-a"].legacy {
		t.Fatal("explicit GitHub instance is marked legacy")
	}
	if !instances["github-b"].legacy {
		t.Fatal("legacy GitHub instance is not marked legacy")
	}

	executor := buildGitHubExecutor(instances)
	if executor == nil || len(executor.adapters) != 2 {
		t.Fatalf("GitHub executor = %#v, want two adapters", executor)
	}
	request := &repowolfv1.GitHubRequest{
		Operation: &repowolfv1.GitHubRequest_RepositoryView{
			RepositoryView: &repowolfv1.GitHubRepositoryViewRequest{},
		},
	}
	for _, id := range []string{"github-a", "github-b"} {
		_, err := instances[id].github.Execute(context.Background(), policy.ResolvedRepository{
			Repository: config.Repository{Provider: id, Owner: "owner", Name: "repo"},
			Provider:   instances[id].provider,
		}, request)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(caller.commands) != 2 {
		t.Fatalf("provider commands = %d, want 2", len(caller.commands))
	}
	assertGitHubCommandEnvironment(t, caller.commands[0], "github-a-secret")
	assertGitHubCommandEnvironment(t, caller.commands[1], "github-b-secret")
}

func TestBuildGitHubExecutorReturnsNilWithoutGitHubAdapters(t *testing.T) {
	if executor := buildGitHubExecutor(map[string]providerInstance{
		"gitea": {provider: config.Provider{Kind: config.ProviderGitea}},
	}); executor != nil {
		t.Fatalf("buildGitHubExecutor() = %#v, want nil", executor)
	}
}

func TestBuildProviderInstancesRejectsUnsupportedKindWithoutPartialInstances(t *testing.T) {
	cfg, snapshot := providerInstanceFixture(t)
	provider := cfg.Providers["github-b"]
	provider.Kind = config.ProviderKind("unsupported")
	cfg.Providers["github-b"] = provider

	instances, err := buildProviderInstances(cfg, snapshot, runner.Toolset{GH: "/pinned/gh"}, nil, &recordingCaller{}, func(config.Provider, string) (*sdk.Client, error) { return &sdk.Client{}, nil })
	if err == nil {
		t.Fatal("buildProviderInstances() error = nil")
	}
	if instances != nil {
		t.Fatalf("buildProviderInstances() instances = %#v, want nil", instances)
	}
}

func TestBuildProviderInstancesRejectsInvalidGitHubTool(t *testing.T) {
	cfg, snapshot := providerInstanceFixture(t)
	tokenFree := []string{"SAFE=value"}
	caller := &recordingCaller{result: repositoryViewResult()}

	_, err := buildProviderInstances(cfg, snapshot, runner.Toolset{GH: "relative-gh"}, tokenFree, caller, func(config.Provider, string) (*sdk.Client, error) { return &sdk.Client{}, nil })
	if err == nil || !strings.Contains(err.Error(), "GitHub provider") {
		t.Fatalf("buildProviderInstances() error = %v", err)
	}
}

func TestBuildProviderInstancesFailsClosedOnGiteaConstructorError(t *testing.T) {
	cfg, snapshot := providerInstanceFixture(t)
	instances, err := buildProviderInstances(cfg, snapshot, runner.Toolset{GH: "/pinned/gh"}, nil, &recordingCaller{}, func(config.Provider, string) (*sdk.Client, error) {
		return nil, errors.New("invalid CA")
	})
	if err == nil || instances != nil || !strings.Contains(err.Error(), "gitea-lab") {
		t.Fatalf("buildProviderInstances() = %#v, %v", instances, err)
	}
}

func providerInstanceFixture(t *testing.T) (config.Config, *credentials.Snapshot) {
	t.Helper()
	cfg := config.Config{
		Providers: map[string]config.Provider{
			"github-a":  {Kind: config.ProviderGitHub, APIHost: "safe.invalid", GitHost: "safe.invalid", SSHUser: "git", SSHPort: 22, TokenEnv: "REPOWOLF_TOKEN_GITHUB_A"},
			"github-b":  {Kind: config.ProviderGitHub, APIHost: "safe.invalid", GitHost: "safe.invalid", SSHUser: "git", SSHPort: 22},
			"gitea-lab": {Kind: config.ProviderGitea, APIHost: "gitea.invalid", TokenEnv: "REPOWOLF_TOKEN_GITEA"},
			"gitea-two": {Kind: config.ProviderGitea, APIHost: "gitea-two.invalid", TokenEnv: "REPOWOLF_TOKEN_GITEA_TWO"},
		},
		Limits: config.Limits{OperationTimeout: time.Minute},
	}
	values := map[string]string{
		"REPOWOLF_TOKEN_GITHUB_A":  "github-a-secret",
		"GH_TOKEN":                 "github-b-secret",
		"REPOWOLF_TOKEN_GITEA":     "gitea-secret",
		"REPOWOLF_TOKEN_GITEA_TWO": "gitea-two-secret",
	}
	snapshot, err := credentials.Load(cfg, func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	return cfg, snapshot
}

func repositoryViewResult() runner.Result {
	return runner.Result{Stdout: []byte(`{"name":"repo","owner":{"login":"owner"},"full_name":"owner/repo","private":false,"html_url":"https://safe.invalid/owner/repo","ssh_url":"git@safe.invalid:owner/repo.git","default_branch":"main"}`)}
}

func assertGitHubCommandEnvironment(t *testing.T, command runner.Command, token string) {
	t.Helper()
	want := []string{
		"SAFE=value",
		"GH_TOKEN=" + token,
		"GH_PROMPT_DISABLED=1",
		"GH_NO_UPDATE_NOTIFIER=1",
		"NO_COLOR=1",
	}
	if !reflect.DeepEqual(command.Env, want) {
		t.Fatalf("command environment = %q, want %q", command.Env, want)
	}
}

var _ providergithub.Caller = (*recordingCaller)(nil)
