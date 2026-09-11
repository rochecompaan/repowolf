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
	tests := []struct {
		name   string
		format outputFormat
		want   string
	}{
		{
			name:   "simple",
			format: outputSimple,
			want: "full_name: Owner/Repo\n" +
				"description: hello\n" +
				"default_branch: main\n" +
				"url: https://gitea.test/Owner/Repo\n" +
				"ssh_url: git@gitea.test:Owner/Repo.git\n" +
				"clone_url: https://gitea.test/Owner/Repo.git\n" +
				"private: true\n" +
				"archived: false\n" +
				"fork: true\n" +
				"mirror: false\n" +
				"empty: false\n" +
				"stars: 2\n" +
				"forks: 3\n" +
				"open_issues: 4\n" +
				"size: 5\n" +
				"topics: one, two\n" +
				"created: 2026-01-02T02:04:05Z\n" +
				"updated: 2026-02-03T04:05:06Z\n",
		},
		{
			name:   "table",
			format: outputTable,
			want: "full_name\tdescription\tdefault_branch\turl\tssh_url\tclone_url\tprivate\tarchived\tfork\tmirror\tempty\tstars\tforks\topen_issues\tsize\ttopics\tcreated\tupdated\n" +
				"Owner/Repo\thello\tmain\thttps://gitea.test/Owner/Repo\tgit@gitea.test:Owner/Repo.git\thttps://gitea.test/Owner/Repo.git\ttrue\tfalse\ttrue\tfalse\tfalse\t2\t3\t4\t5\tone, two\t2026-01-02T02:04:05Z\t2026-02-03T04:05:06Z\n",
		},
		{
			name:   "json",
			format: outputJSON,
			want:   "{\"full_name\":\"Owner/Repo\",\"description\":\"hello\",\"default_branch\":\"main\",\"url\":\"https://gitea.test/Owner/Repo\",\"ssh_url\":\"git@gitea.test:Owner/Repo.git\",\"clone_url\":\"https://gitea.test/Owner/Repo.git\",\"private\":true,\"archived\":false,\"fork\":true,\"mirror\":false,\"empty\":false,\"stars\":2,\"forks\":3,\"open_issues\":4,\"size\":5,\"topics\":[\"one\",\"two\"],\"created\":\"2026-01-02T02:04:05Z\",\"updated\":\"2026-02-03T04:05:06Z\"}\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output, err := render(command{format: test.format}, fixtureResponse())
			if err != nil {
				t.Fatal(err)
			}
			if string(output) != test.want {
				t.Fatalf("render() = %q, want %q", output, test.want)
			}
		})
	}

	response := fixtureResponse()
	response.GetRepositoryView().Repository.Topics = nil
	jsonOutput, _ := render(command{format: outputJSON}, response)
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

func TestRenderEnforcesExactOutputLimitWithoutPartialBytes(t *testing.T) {
	response := fixtureResponse()
	response.GetRepositoryView().Repository.Description = strings.Repeat("x", maxRenderedBytes-1024)
	for range 8 {
		output, err := render(command{format: outputJSON}, response)
		if err != nil {
			t.Fatal(err)
		}
		delta := maxRenderedBytes - len(output)
		if delta == 0 {
			break
		}
		response.GetRepositoryView().Repository.Description += strings.Repeat("x", delta)
	}
	output, err := render(command{format: outputJSON}, response)
	if err != nil || len(output) != maxRenderedBytes {
		t.Fatalf("exact limit render = %d bytes, %v", len(output), err)
	}
	response.GetRepositoryView().Repository.Description += "x"
	output, err = render(command{format: outputJSON}, response)
	if err == nil || output != nil {
		t.Fatalf("over-limit render returned %d partial bytes, %v", len(output), err)
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
