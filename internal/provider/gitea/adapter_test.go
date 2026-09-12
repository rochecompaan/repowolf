package gitea

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "code.gitea.io/sdk/gitea"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"google.golang.org/grpc/metadata"
)

type fakeRepositoryGetter struct {
	fakeIssueAPI
	calls       int
	owner, name string
	repository  *sdk.Repository
	err         error
}

type recordingSDKRepositoryClient struct {
	contexts   []context.Context
	repository *sdk.Repository
	timeline   []*sdk.TimelineComment
	err        error
}

func (client *recordingSDKRepositoryClient) SetContext(ctx context.Context) {
	client.contexts = append(client.contexts, ctx)
}

func (client *recordingSDKRepositoryClient) GetRepo(string, string) (*sdk.Repository, *sdk.Response, error) {
	return client.repository, nil, client.err
}
func (client *recordingSDKRepositoryClient) ListRepoIssues(string, string, sdk.ListIssueOption) ([]*sdk.Issue, *sdk.Response, error) {
	return nil, nil, nil
}
func (client *recordingSDKRepositoryClient) GetIssue(string, string, int64) (*sdk.Issue, *sdk.Response, error) {
	return nil, nil, nil
}
func (client *recordingSDKRepositoryClient) ListIssueTimeline(string, string, int64, sdk.ListIssueCommentOptions) ([]*sdk.TimelineComment, *sdk.Response, error) {
	return client.timeline, nil, client.err
}

func (f *fakeRepositoryGetter) GetRepo(_ context.Context, owner, name string) (*sdk.Repository, error) {
	f.calls++
	f.owner, f.name = owner, name
	return f.repository, f.err
}
func request() *repowolfv1.GiteaRequest {
	return &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_RepositoryView{RepositoryView: &repowolfv1.GiteaRepositoryViewRequest{}}}
}
func resolved() policy.ResolvedRepository {
	return policy.ResolvedRepository{Repository: config.Repository{Owner: "Owner", Name: "Repo"}}
}
func sdkRepository() *sdk.Repository {
	now := time.Now().UTC()
	return &sdk.Repository{FullName: "Owner/Repo", Description: "d", DefaultBranch: "main", HTMLURL: "https://g/o/r", SSHURL: "git@g:o/r.git", CloneURL: "https://g/o/r.git", Stars: 1, Forks: 2, OpenIssues: 3, Size: 4, Topics: []string{"b", "a"}, Created: now, Updated: now.Add(time.Second)}
}

func TestGiteaOperation(t *testing.T) {
	if capability, err := Capability(request()); err != nil || capability != config.RepositoryRead {
		t.Fatalf("capability=%q err=%v", capability, err)
	}
	if operation, err := OperationName(request()); err != nil || operation != "gitea.repository_view" {
		t.Fatalf("operation=%q err=%v", operation, err)
	}
	if ValidateRequest(nil) == nil || ValidateRequest(&repowolfv1.GiteaRequest{}) == nil {
		t.Fatal("invalid request accepted")
	}
}
func TestRepositoryAdapterMapsOneCanonicalCall(t *testing.T) {
	fake := &fakeRepositoryGetter{repository: sdkRepository()}
	adapter, _ := newRepositoryAdapter(fake)
	response, err := adapter.Execute(context.Background(), resolved(), request())
	if err != nil {
		t.Fatal(err)
	}
	record := response.GetRepositoryView().GetRepository()
	if fake.calls != 1 || fake.owner != "Owner" || fake.name != "Repo" || record.FullName != "Owner/Repo" || record.Topics[0] != "b" || record.Stars != 1 {
		t.Fatalf("fake=%#v record=%#v", fake, record)
	}
}
func TestSerializedSDKAPIUsesPaginatedTimelineComments(t *testing.T) {
	now := time.Now().UTC()
	client := &recordingSDKRepositoryClient{timeline: []*sdk.TimelineComment{
		{ID: 1, Type: "label", Created: now, Updated: now},
		{ID: 2, Type: "comment", Poster: &sdk.User{ID: 3, UserName: "alice"}, HTMLURL: "https://g/o/r/issues/7#issuecomment-2", Body: "body", Created: now, Updated: now},
	}}
	api := newSerializedSDKAPI(client)
	page, err := api.ListIssueTimeline(context.Background(), "Owner", "Repo", 7, sdk.ListIssueCommentOptions{ListOptions: sdk.ListOptions{Page: 2, PageSize: 50}})
	if err != nil || page.entryCount != 2 || len(page.comments) != 1 || page.comments[0].ID != 2 || page.comments[0].Body != "body" {
		t.Fatalf("ListIssueTimeline() = %#v, %v", page, err)
	}
}

func TestSerializedSDKAPIObservesCancellationWhileQueued(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	client, err := sdk.NewClient(server.URL+"/", sdk.SetGiteaVersion(""))
	if err != nil {
		t.Fatal(err)
	}
	getter := newSerializedSDKAPI(client)
	firstDone := make(chan error, 1)
	go func() {
		_, err := getter.GetRepo(context.Background(), "Owner", "Repo")
		firstDone <- err
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() {
		_, err := getter.GetRepo(ctx, "Owner", "Repo")
		secondDone <- err
	}()
	cancel()
	select {
	case err := <-secondDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued GetRepo() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued GetRepo did not observe cancellation")
	}
	select {
	case err := <-firstDone:
		t.Fatalf("first GetRepo returned before release: %v", err)
	default:
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first GetRepo() error = %v", err)
	}
}

func TestSerializedSDKAPIDoesNotRetainRequestContext(t *testing.T) {
	for _, test := range []struct {
		name       string
		repository *sdk.Repository
		err        error
	}{
		{name: "success", repository: sdkRepository()},
		{name: "error", err: errors.New("provider failure")},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &recordingSDKRepositoryClient{repository: test.repository, err: test.err}
			getter := newSerializedSDKAPI(client)
			requestContext := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer secret"))

			_, _ = getter.GetRepo(requestContext, "Owner", "Repo")

			if len(client.contexts) != 2 {
				t.Fatalf("SetContext calls = %d, want request and reset", len(client.contexts))
			}
			if values := metadata.ValueFromIncomingContext(client.contexts[0], "authorization"); len(values) != 1 || values[0] != "Bearer secret" {
				t.Fatalf("request authorization metadata = %q", values)
			}
			if values := metadata.ValueFromIncomingContext(client.contexts[1], "authorization"); len(values) != 0 {
				t.Fatalf("reset context retained authorization metadata = %q", values)
			}
		})
	}
}

func TestRepositoryAdapterFailsClosed(t *testing.T) {
	for _, mutate := range []func(*sdk.Repository){func(r *sdk.Repository) { r.FullName = "wrong" }, func(r *sdk.Repository) { r.Size = -1 }, func(r *sdk.Repository) { r.HTMLURL = "" }, func(r *sdk.Repository) { r.Topics = []string{"bad\x00"} }, func(r *sdk.Repository) { r.Updated = r.Created.Add(-time.Second) }} {
		repository := sdkRepository()
		mutate(repository)
		adapter, _ := newRepositoryAdapter(&fakeRepositoryGetter{repository: repository})
		if _, err := adapter.Execute(context.Background(), resolved(), request()); !errors.Is(err, rpcstatus.ErrProviderFailure) {
			t.Errorf("err=%v", err)
		}
	}
	adapter, _ := newRepositoryAdapter(&fakeRepositoryGetter{err: errors.New("secret")})
	if _, err := adapter.Execute(context.Background(), resolved(), request()); !errors.Is(err, rpcstatus.ErrProviderFailure) {
		t.Fatalf("err=%v", err)
	}
	adapter, _ = newRepositoryAdapter(&fakeRepositoryGetter{err: context.Canceled})
	if _, err := adapter.Execute(context.Background(), resolved(), request()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation err=%v", err)
	}
}
