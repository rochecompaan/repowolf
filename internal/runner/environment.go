package runner

import "strings"

var tokenEnvironmentPrefixes = []string{"REPOWOLF_", "GH_", "GITHUB_"}

// TokenFreeEnvironment removes credentials and controls without inspecting or
// rewriting any retained value.
func TokenFreeEnvironment(base []string, excluded []string) []string {
	blocked := make(map[string]struct{}, len(excluded))
	for _, name := range excluded {
		blocked[name] = struct{}{}
	}

	result := make([]string, 0, len(base))
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if _, remove := blocked[name]; remove || hasTokenEnvironmentPrefix(name) {
			continue
		}
		result = append(result, strings.Clone(entry))
	}
	return result
}

// GitHubEnvironment renders a GitHub CLI environment from token-free values.
func GitHubEnvironment(tokenFree []string, token string) []string {
	result := make([]string, 0, len(tokenFree)+4)
	for _, entry := range tokenFree {
		name, _, _ := strings.Cut(entry, "=")
		if name == "NO_COLOR" {
			continue
		}
		result = append(result, strings.Clone(entry))
	}
	return append(result,
		"GH_TOKEN="+token,
		"GH_PROMPT_DISABLED=1",
		"GH_NO_UPDATE_NOTIFIER=1",
		"NO_COLOR=1",
	)
}

func hasTokenEnvironmentPrefix(name string) bool {
	for _, prefix := range tokenEnvironmentPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// ProviderEnvironment removes service credentials and controls without
// inspecting or rewriting any retained value.
func ProviderEnvironment(base []string, excluded []string) []string {
	return TokenFreeEnvironment(base, excluded)
}
