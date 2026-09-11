package gitea

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdk "code.gitea.io/sdk/gitea"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type repositoryGetter interface {
	GetRepo(context.Context, string, string) (*sdk.Repository, error)
}
type sdkRepositoryClient interface {
	SetContext(context.Context)
	GetRepo(string, string) (*sdk.Repository, *sdk.Response, error)
}
type sdkRepositoryGetter struct {
	client sdkRepositoryClient
	slot   chan struct{}
}

func newSDKRepositoryGetter(client sdkRepositoryClient) *sdkRepositoryGetter {
	s := &sdkRepositoryGetter{client: client, slot: make(chan struct{}, 1)}
	s.slot <- struct{}{}
	return s
}
func (a *sdkRepositoryGetter) GetRepo(ctx context.Context, o, r string) (*sdk.Repository, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-a.slot:
	}
	defer func() { a.client.SetContext(context.Background()); a.slot <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.client.SetContext(ctx)
	v, _, err := a.client.GetRepo(o, r)
	return v, err
}

type issueAPI interface {
	ListRepoIssues(context.Context, string, string, sdk.ListIssueOption) ([]*sdk.Issue, error)
	GetIssue(context.Context, string, string, int64) (*sdk.Issue, int, error)
	ListIssueComments(context.Context, string, string, int64, sdk.ListIssueCommentOptions) ([]*sdk.Comment, error)
}
type sdkClient interface {
	SetContext(context.Context)
	GetRepo(string, string) (*sdk.Repository, *sdk.Response, error)
	ListRepoIssues(string, string, sdk.ListIssueOption) ([]*sdk.Issue, *sdk.Response, error)
	GetIssue(string, string, int64) (*sdk.Issue, *sdk.Response, error)
	ListIssueComments(string, string, int64, sdk.ListIssueCommentOptions) ([]*sdk.Comment, *sdk.Response, error)
}
type serializedSDKAPI struct {
	client sdkClient
	slot   chan struct{}
}

func newSerializedSDKAPI(client sdkClient) *serializedSDKAPI {
	s := &serializedSDKAPI{client: client, slot: make(chan struct{}, 1)}
	s.slot <- struct{}{}
	return s
}
func (a *serializedSDKAPI) with(ctx context.Context, call func() error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-a.slot:
	}
	defer func() { a.client.SetContext(context.Background()); a.slot <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return err
	}
	a.client.SetContext(ctx)
	return call()
}
func (a *serializedSDKAPI) GetRepo(ctx context.Context, o, r string) (v *sdk.Repository, err error) {
	err = a.with(ctx, func() error { var e error; v, _, e = a.client.GetRepo(o, r); return e })
	return
}
func (a *serializedSDKAPI) ListRepoIssues(ctx context.Context, o, r string, opt sdk.ListIssueOption) (v []*sdk.Issue, err error) {
	err = a.with(ctx, func() error { var e error; v, _, e = a.client.ListRepoIssues(o, r, opt); return e })
	return
}
func (a *serializedSDKAPI) GetIssue(ctx context.Context, o, r string, i int64) (v *sdk.Issue, status int, err error) {
	err = a.with(ctx, func() error {
		var response *sdk.Response
		var e error
		v, response, e = a.client.GetIssue(o, r, i)
		if response != nil {
			status = response.StatusCode
		}
		return e
	})
	return
}
func (a *serializedSDKAPI) ListIssueComments(ctx context.Context, o, r string, i int64, opt sdk.ListIssueCommentOptions) (v []*sdk.Comment, err error) {
	err = a.with(ctx, func() error { var e error; v, _, e = a.client.ListIssueComments(o, r, i, opt); return e })
	return
}

type RepositoryAdapter struct {
	getter repositoryGetter
	api    issueAPI
}

func NewRepositoryAdapter(client *sdk.Client) (*RepositoryAdapter, error) {
	if client == nil {
		return nil, fmt.Errorf("construct Gitea repository adapter: nil client")
	}
	api := newSerializedSDKAPI(client)
	return &RepositoryAdapter{getter: api, api: api}, nil
}
func newRepositoryAdapter(getter repositoryGetter) (*RepositoryAdapter, error) {
	if getter == nil {
		return nil, fmt.Errorf("construct Gitea repository adapter: nil getter")
	}
	a := &RepositoryAdapter{getter: getter}
	if api, ok := getter.(issueAPI); ok {
		a.api = api
	}
	return a, nil
}
func newIssueAdapter(api interface {
	repositoryGetter
	issueAPI
}) (*RepositoryAdapter, error) {
	if api == nil {
		return nil, fmt.Errorf("nil api")
	}
	return &RepositoryAdapter{getter: api, api: api}, nil
}

func (a *RepositoryAdapter) Execute(ctx context.Context, repo policy.ResolvedRepository, request *repowolfv1.GiteaRequest) (*repowolfv1.GiteaResponse, error) {
	if a == nil || a.getter == nil {
		return nil, rpcstatus.ErrServiceUnavailable
	}
	if ValidateRequest(request) != nil {
		return nil, ErrInvalidRequest
	}
	switch {
	case request.GetRepositoryView() != nil:
		return a.repository(ctx, repo)
	case request.GetIssueList() != nil:
		if a.api == nil {
			return nil, rpcstatus.ErrServiceUnavailable
		}
		return a.issueList(ctx, repo, request.GetIssueList())
	case request.GetIssueView() != nil:
		if a.api == nil {
			return nil, rpcstatus.ErrServiceUnavailable
		}
		return a.issueView(ctx, repo, request.GetIssueView())
	}
	return nil, ErrInvalidRequest
}
func (a *RepositoryAdapter) repository(ctx context.Context, repository policy.ResolvedRepository) (*repowolfv1.GiteaResponse, error) {
	result, err := a.getter.GetRepo(ctx, repository.Repository.Owner, repository.Repository.Name)
	if err != nil {
		return nil, classifyProviderError(ctx, err)
	}
	if validateRepository(result, repository.Repository.Owner, repository.Repository.Name) != nil {
		return nil, rpcstatus.ErrProviderFailure
	}
	record := &repowolfv1.GiteaRepositoryRecord{FullName: result.FullName, Description: result.Description, DefaultBranch: result.DefaultBranch, Url: result.HTMLURL, SshUrl: result.SSHURL, CloneUrl: result.CloneURL, Private: result.Private, Archived: result.Archived, Fork: result.Fork, Mirror: result.Mirror, Empty: result.Empty, Stars: uint64(result.Stars), Forks: uint64(result.Forks), OpenIssues: uint64(result.OpenIssues), Size: uint64(result.Size), Topics: append([]string{}, result.Topics...), Created: timestamppb.New(result.Created), Updated: timestamppb.New(result.Updated)}
	return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_RepositoryView{RepositoryView: &repowolfv1.GiteaRepositoryViewResult{Repository: record}}}, nil
}
func (a *RepositoryAdapter) issueList(ctx context.Context, repository policy.ResolvedRepository, r *repowolfv1.GiteaIssueListRequest) (*repowolfv1.GiteaResponse, error) {
	owner, name := repository.Repository.Owner, repository.Repository.Name
	if r.Owner != nil && !strings.EqualFold(r.GetOwner(), owner) {
		return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueList{IssueList: &repowolfv1.GiteaIssueListResult{Issues: []*repowolfv1.GiteaIssueRecord{}}}}, nil
	}
	states := map[repowolfv1.GiteaIssueState]sdk.StateType{1: sdk.StateOpen, 2: sdk.StateClosed, 3: sdk.StateAll}
	opt := sdk.ListIssueOption{ListOptions: sdk.ListOptions{Page: int(r.Page), PageSize: int(r.Limit)}, State: states[r.State], Type: sdk.IssueTypeIssue, KeyWord: r.GetKeyword(), CreatedBy: r.GetAuthor(), AssignedBy: r.GetAssignee(), MentionedBy: r.GetMentions(), Owner: canonicalOwnerFilter(r, owner)}
	if r.From != nil {
		opt.Since = r.From.AsTime()
	}
	if r.Until != nil {
		opt.Before = r.Until.AsTime()
	}
	issues, err := a.api.ListRepoIssues(ctx, owner, name, opt)
	if err != nil {
		return nil, classifyProviderError(ctx, err)
	}
	if len(issues) > int(r.Limit) {
		return nil, rpcstatus.ErrProviderFailure
	}
	records := make([]*repowolfv1.GiteaIssueRecord, len(issues))
	for i, v := range issues {
		n, e := normalizeIssue(v, owner, name)
		if e != nil {
			return nil, rpcstatus.ErrProviderFailure
		}
		records[i] = projectIssue(n, r.Fields)
	}
	return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueList{IssueList: &repowolfv1.GiteaIssueListResult{Issues: records}}}, nil
}
func (a *RepositoryAdapter) issueView(ctx context.Context, repository policy.ResolvedRepository, r *repowolfv1.GiteaIssueViewRequest) (*repowolfv1.GiteaResponse, error) {
	owner, name := repository.Repository.Owner, repository.Repository.Name
	issue, statusCode, err := a.api.GetIssue(ctx, owner, name, r.Index)
	if err != nil {
		if statusCode == 404 {
			return nil, rpcstatus.ErrNotFound
		}
		return nil, classifyProviderError(ctx, err)
	}
	if issue == nil {
		return nil, rpcstatus.ErrProviderFailure
	}
	if issue.PullRequest != nil {
		return nil, rpcstatus.ErrIssueKind
	}
	n, e := normalizeIssue(issue, owner, name)
	if e != nil || n.index != r.Index {
		return nil, rpcstatus.ErrProviderFailure
	}
	record := projectIssue(n, allIssueFields)
	if r.IncludeComments {
		comments, e := loadIssueComments(ctx, a.api, owner, name, r.Index, record)
		if e != nil {
			return nil, e
		}
		record.Comments = comments
	}
	return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueView{IssueView: &repowolfv1.GiteaIssueViewResult{Issue: record}}}, nil
}
func canonicalOwnerFilter(request *repowolfv1.GiteaIssueListRequest, owner string) string {
	if request.Owner != nil {
		return owner
	}
	return ""
}

func classifyProviderError(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	return rpcstatus.ErrProviderFailure
}
func validateRepository(repository *sdk.Repository, owner, name string) error {
	if repository == nil || repository.FullName != owner+"/"+name || repository.HTMLURL == "" || repository.SSHURL == "" || repository.CloneURL == "" || repository.Created.IsZero() || repository.Updated.IsZero() || repository.Updated.Before(repository.Created) || repository.Stars < 0 || repository.Forks < 0 || repository.OpenIssues < 0 || repository.Size < 0 {
		return fmt.Errorf("invalid repository")
	}
	values := []string{repository.FullName, repository.Description, repository.DefaultBranch, repository.HTMLURL, repository.SSHURL, repository.CloneURL}
	values = append(values, repository.Topics...)
	for _, value := range values {
		if !validProviderString(value) {
			return fmt.Errorf("invalid repository string")
		}
	}
	return nil
}
