package github

import (
	"context"
	"encoding/json"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/protobuf/proto"
)

const maximumMutationBytes = 4 * miB

func requiresPreflight(request *repowolfv1.GitHubRequest) bool {
	switch request.Operation.(type) {
	case *repowolfv1.GitHubRequest_IssueEdit, *repowolfv1.GitHubRequest_IssueComment,
		*repowolfv1.GitHubRequest_IssueClose, *repowolfv1.GitHubRequest_IssueReopen,
		*repowolfv1.GitHubRequest_PullComment:
		return true
	default:
		return false
	}
}

func (adapter *Adapter) preflight(ctx context.Context, repository policy.ResolvedRepository, request *repowolfv1.GitHubRequest) (int, error) {
	base := "/repos/" + repository.Repository.Owner + "/" + repository.Repository.Name
	var number uint64
	pull := false
	switch operation := request.Operation.(type) {
	case *repowolfv1.GitHubRequest_IssueEdit:
		number = operation.IssueEdit.Number
	case *repowolfv1.GitHubRequest_IssueComment:
		number = operation.IssueComment.Number
	case *repowolfv1.GitHubRequest_IssueClose:
		number = operation.IssueClose.Number
	case *repowolfv1.GitHubRequest_IssueReopen:
		number = operation.IssueReopen.Number
	case *repowolfv1.GitHubRequest_PullComment:
		number, pull = operation.PullComment.Number, true
	default:
		return 0, ErrInvalidRequest
	}
	resource := "/issues/"
	if pull {
		resource = "/pulls/"
	}
	command := adapter.apiCommand(repository.Provider.APIHost, "GET", base+resource+decimal(number), nil, maximumMutationBytes)
	result, err := adapter.call(ctx, command)
	if err != nil {
		return 0, err
	}
	remaining := maximumMutationBytes - len(result.Stdout)
	if remaining <= 0 {
		return 0, runner.ErrOutputLimit
	}
	if pull {
		var value struct {
			Head *apiRef `json:"head"`
		}
		if err := decode(result.Stdout, &value); err != nil {
			return 0, err
		}
		if value.Head == nil {
			return 0, providerResponse(nil, "pull request")
		}
		return remaining, nil
	}
	if err := rejectPullIssue(result.Stdout); err != nil {
		return 0, err
	}
	return remaining, nil
}

func rejectPullIssue(raw []byte) error {
	var value struct {
		PullRequest json.RawMessage `json:"pull_request"`
	}
	if err := decode(raw, &value); err != nil {
		return err
	}
	if value.PullRequest != nil {
		return providerResponse(nil, "issue")
	}
	return nil
}

func (adapter *Adapter) executeReady(ctx context.Context, repository policy.ResolvedRepository, request *repowolfv1.GitHubRequest) (*repowolfv1.GitHubResponse, error) {
	operation := request.GetPullReady()
	ready := adapter.command([]string{"pr", "ready", decimal(operation.Number), "--repo", repository.Provider.APIHost + "/" + repository.Repository.Owner + "/" + repository.Repository.Name}, nil, miB)
	if _, err := adapter.call(ctx, ready); err != nil {
		return nil, err
	}
	base := "/repos/" + repository.Repository.Owner + "/" + repository.Repository.Name
	view := adapter.apiCommand(repository.Provider.APIHost, "GET", base+"/pulls/"+decimal(operation.Number), nil, 2*miB)
	result, err := adapter.call(ctx, view)
	if err != nil {
		return nil, err
	}
	response, err := normalize(request, "pull", result.Stdout)
	if err != nil {
		return nil, err
	}
	if proto.Size(response) > maximumResponseBytes {
		return nil, runner.ErrOutputLimit
	}
	return response, nil
}
