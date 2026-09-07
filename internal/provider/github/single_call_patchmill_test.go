package github

import (
	"context"
	"reflect"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/protobuf/proto"
)

// Regression: Patchmill's fixed identity, repository, and label calls must not
// permit request data to alter the pinned command surface.
func TestPatchmillSingleCallCommandContract(t *testing.T) {
	const (
		userJSON       = `{"login":"octocat"}`
		repositoryJSON = `{"name":"name","owner":{"login":"owner"},"full_name":"owner/name","private":false,"html_url":"https://github.example/owner/name","ssh_url":"git@github.example:owner/name.git","default_branch":"main"}`
		labelJSON      = `{"name":"patchmill:ready"}`
	)
	resolvedRepository := repository()
	resolvedRepository.Repository.Name = "name"
	base := "/repos/owner/name"
	cases := []struct {
		name     string
		request  *repowolfv1.GitHubRequest
		response string
		command  runner.Command
	}{
		{
			name:     "current user",
			request:  &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_CurrentUser{CurrentUser: &repowolfv1.GitHubCurrentUserRequest{}}},
			response: userJSON,
			command:  expectedAPI("GET", "/user", nil, miB),
		},
		{
			name:     "repository view",
			request:  &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_RepositoryView{RepositoryView: &repowolfv1.GitHubRepositoryViewRequest{}}},
			response: repositoryJSON,
			command:  expectedAPI("GET", base, nil, miB),
		},
		{
			name:     "label create",
			request:  &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateRequest{Name: "patchmill:ready", Color: "1a2B3c", Description: "Ready for work"}}},
			response: labelJSON,
			command:  expectedAPI("POST", base+"/labels", []byte(`{"color":"1a2B3c","description":"Ready for work","name":"patchmill:ready"}`), 4*miB),
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			caller := &fakeCaller{results: []runner.Result{{Stdout: []byte(test.response)}}}
			response, err := testAdapter(t, caller).Execute(context.Background(), resolvedRepository, test.request)
			if err != nil || response == nil {
				t.Fatalf("Execute() = %#v, %v", response, err)
			}
			if !reflect.DeepEqual(caller.commands, []runner.Command{test.command}) {
				t.Fatalf("commands = %#v\nwant %#v", caller.commands, []runner.Command{test.command})
			}
		})
	}
}

// Regression: fixed single-call responses must return only required typed data
// and reject malformed or oversized provider output.
func TestPatchmillSingleCallProviderResponsesRejectInvalidOrMismatchedData(t *testing.T) {
	resolvedRepository := repository()
	resolvedRepository.Provider.GitHost = "github.example"
	resolvedRepository.Provider.SSHUser = "git"
	resolvedRepository.Provider.SSHPort = 22

	currentUser := &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_CurrentUser{CurrentUser: &repowolfv1.GitHubCurrentUserRequest{}}}
	labelCreate := &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateRequest{Name: "patchmill:ready", Color: "1a2B3c", Description: "Ready for work"}}}
	repositoryView := &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_RepositoryView{RepositoryView: &repowolfv1.GitHubRepositoryViewRequest{}}}
	repositoryJSON := func(name, owner, fullName, htmlURL, sshURL string) []byte {
		return []byte(`{"name":"` + name + `","owner":{"login":"` + owner + `"},"full_name":"` + fullName + `","private":false,"html_url":"` + htmlURL + `","ssh_url":"` + sshURL + `","default_branch":"main"}`)
	}

	tests := []struct {
		name    string
		request *repowolfv1.GitHubRequest
		stdout  []byte
	}{
		{"empty login", currentUser, []byte(`{"login":""}`)},
		{"invalid UTF-8 login", currentUser, []byte{'{', '"', 'l', 'o', 'g', 'i', 'n', '"', ':', '"', 0xff, '"', '}'}},
		{"oversized login", currentUser, []byte(`{"login":"` + strings.Repeat("x", maximumNameBytes+1) + `"}`)},
		{"empty created label name", labelCreate, []byte(`{"name":""}`)},
		{"oversized created label name", labelCreate, []byte(`{"name":"` + strings.Repeat("x", 51) + `"}`)},
		{"mismatched created label name", labelCreate, []byte(`{"name":"other"}`)},
		{"mismatched repository identity", repositoryView, repositoryJSON("other", "owner", "owner/other", "https://github.example/owner/other", "git@github.example:owner/other.git")},
		{"empty repository URL", repositoryView, repositoryJSON("repo", "owner", "owner/repo", "", "git@github.example:owner/repo.git")},
		{"malformed repository URL", repositoryView, repositoryJSON("repo", "owner", "owner/repo", "not a URL", "git@github.example:owner/repo.git")},
		{"untrusted repository URL", repositoryView, repositoryJSON("repo", "owner", "owner/repo", "https://evil.example/owner/repo", "git@github.example:owner/repo.git")},
		{"empty SSH URL", repositoryView, repositoryJSON("repo", "owner", "owner/repo", "https://github.example/owner/repo", "")},
		{"malformed SSH URL", repositoryView, repositoryJSON("repo", "owner", "owner/repo", "https://github.example/owner/repo", "not an SSH URL")},
		{"untrusted SSH target", repositoryView, repositoryJSON("repo", "owner", "owner/repo", "https://github.example/owner/repo", "git@evil.example:owner/repo.git")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			caller := &fakeCaller{results: []runner.Result{{Stdout: test.stdout}}}
			response, err := testAdapter(t, caller).Execute(context.Background(), resolvedRepository, test.request)
			if response != nil || err == nil {
				t.Fatalf("Execute() = %#v, %v; want provider response rejection", response, err)
			}
		})
	}
}

func TestPatchmillSingleCallNormalization(t *testing.T) {
	currentUser := &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_CurrentUser{CurrentUser: &repowolfv1.GitHubCurrentUserRequest{}}}
	labelCreate := &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateRequest{Name: "patchmill:ready", Color: "1a2B3c", Description: "Ready for work"}}}
	repositoryView := &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_RepositoryView{RepositoryView: &repowolfv1.GitHubRepositoryViewRequest{}}}

	valid := []struct {
		name string
		req  *repowolfv1.GitHubRequest
		kind string
		raw  string
		want *repowolfv1.GitHubResponse
	}{
		{
			name: "current user",
			req:  currentUser,
			kind: "current_user",
			raw:  `{"login":"octocat"}`,
			want: &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_CurrentUser{CurrentUser: &repowolfv1.GitHubCurrentUserResult{User: &repowolfv1.GitHubUserRecord{Login: "octocat"}}}},
		},
		{
			name: "label create",
			req:  labelCreate,
			kind: "label_create",
			raw:  `{"name":"patchmill:ready"}`,
			want: &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateResult{Label: &repowolfv1.GitHubLabelRecord{Name: "patchmill:ready"}}}},
		},
		{
			name: "repository view",
			req:  repositoryView,
			kind: "repository",
			raw:  `{"name":"repowolf","owner":{"login":"owner"},"full_name":"owner/repowolf","private":false,"html_url":"https://github.com/owner/repowolf","ssh_url":"git@github.com:owner/repowolf.git","default_branch":"main"}`,
			want: &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_RepositoryView{RepositoryView: &repowolfv1.GitHubRepositoryViewResult{Repository: &repowolfv1.GitHubRepositoryRecord{Repository: "repowolf", Owner: "owner", NameWithOwner: "owner/repowolf", Private: false, Url: "https://github.com/owner/repowolf", SshUrl: "git@github.com:owner/repowolf.git", DefaultBranch: "main"}}}},
		},
	}
	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalize(test.req, test.kind, []byte(test.raw))
			if err != nil || !proto.Equal(got, test.want) {
				t.Fatalf("normalize() = %#v, %v; want %#v, nil", got, err, test.want)
			}
		})
	}

	malformed := []struct {
		name string
		req  *repowolfv1.GitHubRequest
		kind string
		raw  string
	}{
		{"missing login", currentUser, "current_user", `{}`},
		{"missing label name", labelCreate, "label_create", `{}`},
		{"missing SSH URL", repositoryView, "repository", `{"name":"repowolf","owner":{"login":"owner"},"full_name":"owner/repowolf","private":false,"html_url":"https://github.com/owner/repowolf","default_branch":"main"}`},
		{"wrong current user type", currentUser, "current_user", `{"login":1}`},
		{"wrong label type", labelCreate, "label_create", `{"name":1}`},
		{"wrong repository type", repositoryView, "repository", `{"ssh_url":1}`},
		{"trailing JSON", currentUser, "current_user", `{"login":"octocat"} {}`},
	}
	for _, test := range malformed {
		t.Run(test.name, func(t *testing.T) {
			response, err := normalize(test.req, test.kind, []byte(test.raw))
			if err == nil || response != nil {
				t.Fatalf("normalize() = %#v, %v; want malformed response rejection", response, err)
			}
		})
	}

	t.Run("oversized provider label", func(t *testing.T) {
		name := strings.Repeat("x", 51)
		caller := &fakeCaller{results: []runner.Result{{Stdout: []byte(`{"name":"` + name + `"}`)}}}
		response, err := testAdapter(t, caller).Execute(context.Background(), repository(), labelCreate)
		if response != nil || err == nil {
			t.Fatalf("Execute() = %#v, %v; want oversized provider label rejection", response, err)
		}
	})
}
