package github

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/runner"
)

// Canonical API links are metadata only: later endpoints and CLI hosts remain local.
func TestLabelListCanonicalAPILinks(t *testing.T) {
	for _, test := range []struct{ host, target string }{
		{"github.com", "https://api.github.com/repositories/41881900/labels"},
		{"github.com", "https://api.github.com/repos/owner/repo/labels"},
		{"github.example", "https://github.example/api/v3/repositories/41881900/labels"},
		{"github.example", "https://github.example/api/v3/repos/owner/repo/labels"},
		{"github.example", "https://github.example/repos/owner/repo/labels"},
	} {
		t.Run(test.target, func(t *testing.T) {
			repo := repository()
			repo.Provider.APIHost = test.host
			first := []byte(includedResponse([]string{fmt.Sprintf(`Link: <%s?page=2&per_page=100>; rel="next", <%s?page=2&per_page=100>; rel="last"`, test.target, test.target)}, labelBody(t, 1, 100)))
			last := []byte(includedResponse([]string{fmt.Sprintf(`Link: <%s?page=1&per_page=100>; rel="prev", <%s?page=1&per_page=100>; rel="first"`, test.target, test.target)}, labelBody(t, 101, 1)))
			caller := &fakeCaller{results: []runner.Result{{Stdout: first}, {Stdout: last}}}
			response, err := testAdapter(t, caller).Execute(context.Background(), repo, labelListRequest(200))
			if err != nil {
				t.Fatal(err)
			}
			if len(response.GetLabelList().GetLabels()) != 101 || len(caller.commands) != 2 {
				t.Fatalf("response/calls = %v/%d", response, len(caller.commands))
			}
			for index, command := range caller.commands {
				if got := command.Args[len(command.Args)-1]; got != fmt.Sprintf("/repos/owner/repo/labels?page=%d&per_page=100", index+1) {
					t.Fatalf("endpoint = %q", got)
				}
				if command.Args[3] != test.host {
					t.Fatalf("hostname args = %v", command.Args)
				}
			}
		})
	}
}

func TestLabelListCanonicalLinkRejectsInvalidTargets(t *testing.T) {
	const canonical = "https://api.github.com/repositories/41881900/labels?page=2&per_page=100"
	for _, target := range []string{
		strings.Replace(canonical, "https:", "http:", 1),
		strings.Replace(canonical, "api.github.com", "github.com", 1),
		strings.Replace(canonical, "api.github.com", "attacker.example", 1),
		strings.Replace(canonical, "api.github.com", "api.github.com:443", 1),
		strings.Replace(canonical, "api.github.com", "user@api.github.com", 1),
		strings.Replace(canonical, "41881900", "0", 1),
		strings.Replace(canonical, "41881900", "-1", 1),
		strings.Replace(canonical, "41881900", "abc", 1),
		strings.Replace(canonical, "41881900", "%34", 1),
		strings.Replace(canonical, "/labels?", "/issues?", 1),
		strings.Replace(canonical, "/repositories/41881900", "/repos/other/repo", 1),
		strings.Replace(canonical, "page=2&", "page=3&", 1),
		strings.Replace(canonical, "per_page=100", "per_page=99", 1),
		canonical + "&page=2", canonical + "&per_page=100", canonical + "&extra=1", canonical + "#fragment",
	} {
		t.Run(target, func(t *testing.T) {
			raw := []byte(includedResponse([]string{`Link: <` + target + `>; rel="next"`}, `[]`))
			if _, err := decodeIncludedRepositoryPage(raw, 1, 100, "github.com", "/repos/owner/repo/labels"); err == nil {
				t.Fatal("accepted invalid target")
			}
		})
	}
	// A configured enterprise host cannot be replaced by the public API origin.
	raw := []byte(includedResponse([]string{`Link: <` + canonical + `>; rel="next"`}, `[]`))
	if _, err := decodeIncludedRepositoryPage(raw, 1, 100, "github.example", "/repos/owner/repo/labels"); err == nil {
		t.Fatal("accepted public origin for enterprise")
	}
}
