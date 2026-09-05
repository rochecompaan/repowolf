package runner

import (
	"reflect"
	"testing"
)

func TestTokenFreeEnvironmentRemovesTokensAndControls(t *testing.T) {
	base := []string{
		"PATH=/bin",
		"SSH_AUTH_SOCK=/run/agent.sock",
		"GIT_PROTOCOL=version=2",
		"REPOWOLF_TOKEN_AGENT=principal-secret",
		"REPOWOLF_TOKEN_GITHUB=provider-secret",
		"REPOWOLF_INTERNAL=control",
		"GH_TOKEN=ambient-gh",
		"GH_HOST=ambient-host",
		"GITHUB_TOKEN=ambient-github",
		"NO_COLOR=0",
		"SAFE=value=with=equals",
		"MALFORMED",
	}

	got := TokenFreeEnvironment(base, []string{"REPOWOLF_TOKEN_AGENT", "REPOWOLF_TOKEN_GITHUB"})
	want := []string{
		"PATH=/bin",
		"SSH_AUTH_SOCK=/run/agent.sock",
		"GIT_PROTOCOL=version=2",
		"NO_COLOR=0",
		"SAFE=value=with=equals",
		"MALFORMED",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
}

func TestTokenFreeEnvironmentCopiesInput(t *testing.T) {
	base := []string{"PATH=/bin"}
	got := TokenFreeEnvironment(base, nil)

	base[0] = "PATH=/changed"
	if got[0] != "PATH=/bin" {
		t.Fatal("result aliases input")
	}
}

func TestGitHubEnvironmentAppendsGitHubControls(t *testing.T) {
	tokenFree := TokenFreeEnvironment([]string{
		"PATH=/bin",
		"NO_COLOR=0",
		"SAFE=value=with=equals",
		"MALFORMED",
	}, nil)

	got := GitHubEnvironment(tokenFree, "provider-secret")
	want := []string{
		"PATH=/bin",
		"SAFE=value=with=equals",
		"MALFORMED",
		"GH_TOKEN=provider-secret",
		"GH_PROMPT_DISABLED=1",
		"GH_NO_UPDATE_NOTIFIER=1",
		"NO_COLOR=1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
}

func TestGitHubEnvironmentCopiesInputAndOutput(t *testing.T) {
	tokenFree := []string{"PATH=/bin"}
	first := GitHubEnvironment(tokenFree, "first-secret")
	second := GitHubEnvironment(tokenFree, "second-secret")

	tokenFree[0] = "PATH=/changed"
	first[0] = "PATH=/first-changed"
	if second[0] != "PATH=/bin" {
		t.Fatal("GitHub environments alias an input or output")
	}
	if contains(first, "GH_TOKEN=second-secret") || contains(second, "GH_TOKEN=first-secret") {
		t.Fatal("GitHub environments contain another environment's token")
	}
}

func contains(environment []string, entry string) bool {
	for _, candidate := range environment {
		if candidate == entry {
			return true
		}
	}
	return false
}
