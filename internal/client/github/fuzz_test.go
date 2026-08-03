package github

import (
	"bytes"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func FuzzParseArgs(f *testing.F) {
	for _, seed := range [][]string{
		{"issue", "list", "--repo", "owner/repo"},
		{"pr", "checks", "7", "--json", "name,state", "--repo", "owner/repo"},
		{"api", "/user"},
		{"workflow", "run"},
		{"issue", "view", "1", "--repo", "a/b", "--repo", "c/d"},
	} {
		f.Add(bytes.Join(stringsToBytes(seed), []byte{0}))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			data = data[:64<<10]
		}
		parts := bytes.Split(data, []byte{0})
		if len(parts) > 64 {
			parts = parts[:64]
		}
		args := make([]string, len(parts))
		for index := range parts {
			args[index] = string(parts[index])
		}
		request, err := Parse(args, "/fixed/nonexistent/repository")
		if err == nil && !typedOperation(request) {
			t.Fatalf("Parse() escaped typed operation allowlist: %T", request.GetOperation())
		}
	})
}

func stringsToBytes(values []string) [][]byte {
	result := make([][]byte, len(values))
	for index, value := range values {
		result[index] = []byte(value)
	}
	return result
}

func typedOperation(request *repowolfv1.GitHubRequest) bool {
	if request == nil || request.GetContext().GetRepository() == nil {
		return false
	}
	switch request.GetOperation().(type) {
	case *repowolfv1.GitHubRequest_RepositoryView,
		*repowolfv1.GitHubRequest_IssueList,
		*repowolfv1.GitHubRequest_IssueView,
		*repowolfv1.GitHubRequest_IssueCreate,
		*repowolfv1.GitHubRequest_IssueEdit,
		*repowolfv1.GitHubRequest_IssueComment,
		*repowolfv1.GitHubRequest_IssueClose,
		*repowolfv1.GitHubRequest_IssueReopen,
		*repowolfv1.GitHubRequest_PullList,
		*repowolfv1.GitHubRequest_PullView,
		*repowolfv1.GitHubRequest_PullCreate,
		*repowolfv1.GitHubRequest_PullEdit,
		*repowolfv1.GitHubRequest_PullComment,
		*repowolfv1.GitHubRequest_PullClose,
		*repowolfv1.GitHubRequest_PullReopen,
		*repowolfv1.GitHubRequest_PullReady,
		*repowolfv1.GitHubRequest_PullChecks,
		*repowolfv1.GitHubRequest_RunList,
		*repowolfv1.GitHubRequest_RunView,
		*repowolfv1.GitHubRequest_StatusView:
		return true
	default:
		return false
	}
}
