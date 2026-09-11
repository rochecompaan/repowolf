package gitea

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "code.gitea.io/sdk/gitea"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

type fakeRepositoryGetter struct {
	calls       int
	owner, name string
	repository  *sdk.Repository
	err         error
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
}
