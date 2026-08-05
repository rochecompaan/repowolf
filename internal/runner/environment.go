package runner

import "strings"

// ProviderEnvironment removes service credentials and controls without
// inspecting or rewriting any retained value.
func ProviderEnvironment(base []string, excluded []string) []string {
	blocked := make(map[string]struct{}, len(excluded))
	for _, name := range excluded {
		blocked[name] = struct{}{}
	}

	result := make([]string, 0, len(base))
	for _, entry := range base {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			name = entry
		}
		if _, remove := blocked[name]; remove || strings.HasPrefix(name, "REPOWOLF_") {
			continue
		}
		result = append(result, strings.Clone(entry))
	}
	return result
}
