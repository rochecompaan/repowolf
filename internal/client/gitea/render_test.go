package gitea

import (
	"strings"
	"testing"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func fixtureResponse() *repowolfv1.GiteaResponse {
	r := &repowolfv1.GiteaRepositoryRecord{FullName: "Owner/Repo", Description: "hello", DefaultBranch: "main", Url: "https://gitea.test/Owner/Repo", SshUrl: "git@gitea.test:Owner/Repo.git", CloneUrl: "https://gitea.test/Owner/Repo.git", Private: true, Archived: false, Fork: true, Mirror: false, Empty: false, Stars: 2, Forks: 3, OpenIssues: 4, Size: 5, Topics: []string{"one", "two"}, Created: timestamppb.New(time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600))), Updated: timestamppb.New(time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC))}
	return &repowolfv1.GiteaResponse{Meta: &repowolfv1.ResponseMeta{RequestId: "request"}, Result: &repowolfv1.GiteaResponse_RepositoryView{RepositoryView: &repowolfv1.GiteaRepositoryViewResult{Repository: r}}}
}

func TestRenderFormats(t *testing.T) {
	response := fixtureResponse()
	for _, format := range []outputFormat{outputSimple, outputTable, outputJSON} {
		output, err := render(command{format: format}, response)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(string(output), "\n") || !strings.Contains(string(output), "Owner/Repo") {
			t.Fatalf("output = %q", output)
		}
	}
	jsonOutput, _ := render(command{format: outputJSON}, response)
	if strings.Contains(string(jsonOutput), "FullName") || !strings.Contains(string(jsonOutput), `"topics":["one","two"]`) || !strings.Contains(string(jsonOutput), `"created":"2026-01-02T02:04:05Z"`) {
		t.Fatalf("json = %s", jsonOutput)
	}
	response.GetRepositoryView().Repository.Topics = nil
	jsonOutput, _ = render(command{format: outputJSON}, response)
	if !strings.Contains(string(jsonOutput), `"topics":[]`) {
		t.Fatalf("empty topics are not an array: %s", jsonOutput)
	}
}

func TestRenderRejectsMalformedResponse(t *testing.T) {
	for _, response := range []*repowolfv1.GiteaResponse{nil, {}, {Meta: &repowolfv1.ResponseMeta{RequestId: "x"}}} {
		if _, err := render(command{}, response); err == nil {
			t.Error("malformed response accepted")
		}
	}
	response := fixtureResponse()
	response.GetRepositoryView().Repository.Created = timestamppb.New(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC))
	if _, err := render(command{}, response); err == nil {
		t.Error("invalid timestamp accepted")
	}
}

func TestRenderReplacesControlsInTextOnly(t *testing.T) {
	response := fixtureResponse()
	response.GetRepositoryView().Repository.Description = "a\tb\nc"
	text, _ := render(command{format: outputSimple}, response)
	if strings.Contains(string(text), "a\tb") {
		t.Fatal("control retained")
	}
	jsonOutput, _ := render(command{format: outputJSON}, response)
	if !strings.Contains(string(jsonOutput), `a\tb\nc`) {
		t.Fatalf("json controls changed: %s", jsonOutput)
	}
}
