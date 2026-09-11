package gitea

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	sdk "code.gitea.io/sdk/gitea"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type repositoryGetter interface {
	GetRepo(context.Context, string, string) (*sdk.Repository, error)
}
type sdkRepositoryGetter struct {
	mu     sync.Mutex
	client *sdk.Client
}

func (getter *sdkRepositoryGetter) GetRepo(ctx context.Context, owner, name string) (*sdk.Repository, error) {
	getter.mu.Lock()
	defer getter.mu.Unlock()
	getter.client.SetContext(ctx)
	repository, _, err := getter.client.GetRepo(owner, name)
	return repository, err
}

// RepositoryAdapter executes the one supported Gitea repository operation.
type RepositoryAdapter struct{ getter repositoryGetter }

func NewRepositoryAdapter(client *sdk.Client) (*RepositoryAdapter, error) {
	if client == nil {
		return nil, fmt.Errorf("construct Gitea repository adapter: nil client")
	}
	return &RepositoryAdapter{getter: &sdkRepositoryGetter{client: client}}, nil
}
func newRepositoryAdapter(getter repositoryGetter) (*RepositoryAdapter, error) {
	if getter == nil {
		return nil, fmt.Errorf("construct Gitea repository adapter: nil getter")
	}
	return &RepositoryAdapter{getter: getter}, nil
}

func (adapter *RepositoryAdapter) Execute(ctx context.Context, repository policy.ResolvedRepository, request *repowolfv1.GiteaRequest) (*repowolfv1.GiteaResponse, error) {
	if adapter == nil || adapter.getter == nil {
		return nil, rpcstatus.ErrServiceUnavailable
	}
	if err := ValidateRequest(request); err != nil {
		return nil, ErrInvalidRequest
	}
	result, err := adapter.getter.GetRepo(ctx, repository.Repository.Owner, repository.Repository.Name)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, rpcstatus.ErrProviderFailure
	}
	if err := validateRepository(result, repository.Repository.Owner, repository.Repository.Name); err != nil {
		return nil, rpcstatus.ErrProviderFailure
	}
	record := &repowolfv1.GiteaRepositoryRecord{
		FullName: result.FullName, Description: result.Description, DefaultBranch: result.DefaultBranch,
		Url: result.HTMLURL, SshUrl: result.SSHURL, CloneUrl: result.CloneURL,
		Private: result.Private, Archived: result.Archived, Fork: result.Fork, Mirror: result.Mirror, Empty: result.Empty,
		Stars: uint64(result.Stars), Forks: uint64(result.Forks), OpenIssues: uint64(result.OpenIssues), Size: uint64(result.Size), Topics: append([]string(nil), result.Topics...),
		Created: timestamppb.New(result.Created), Updated: timestamppb.New(result.Updated),
	}
	return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_RepositoryView{RepositoryView: &repowolfv1.GiteaRepositoryViewResult{Repository: record}}}, nil
}

func validateRepository(repository *sdk.Repository, owner, name string) error {
	if repository == nil || repository.FullName != owner+"/"+name || repository.HTMLURL == "" || repository.SSHURL == "" || repository.CloneURL == "" || repository.Created.IsZero() || repository.Updated.IsZero() || repository.Updated.Before(repository.Created) || repository.Stars < 0 || repository.Forks < 0 || repository.OpenIssues < 0 || repository.Size < 0 {
		return fmt.Errorf("invalid repository")
	}
	values := []string{repository.FullName, repository.Description, repository.DefaultBranch, repository.HTMLURL, repository.SSHURL, repository.CloneURL}
	values = append(values, repository.Topics...)
	for _, value := range values {
		if !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
			return fmt.Errorf("invalid repository string")
		}
	}
	return nil
}
