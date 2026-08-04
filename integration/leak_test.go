package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/clientconfig"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestMarkersRemainInTheirIntendedChannels(t *testing.T) {
	forge := newFixture(t)
	listOut, listErr := forge.runGH(t, "issue", "list", "--repo", "alpha/repo")
	createOut, createErr := forge.runGH(t, "issue", "create", "--repo", "alpha/repo", "--title", "typed write", "--body", issueBodyMarker)
	commentOut, commentErr := forge.runGH(t, "issue", "comment", "31", "--repo", "alpha/repo", "--body", commentMarker)
	forge.stop(t)

	git := newGitFixture(t)
	checkout := filepath.Join(git.root, "leak-checkout")
	clone := git.git(t, git.root, "clone", "ssh://git@github.com:22/alpha/repo.git", checkout)
	if clone.err != nil {
		t.Fatalf("offline clone: %v; stderr=%q", clone.err, clone.stderr)
	}
	git.server.Stop(t)

	channels := map[string]string{
		"client.stdout":        listOut + createOut + commentOut + clone.stdout,
		"client.diagnostics":   listErr + createErr + commentErr + clone.stderr,
		"audit":                forge.audit() + string(mustRead(git.server.AuditPath)),
		"server.stderr":        string(mustRead(forge.server.StderrPath)) + string(mustRead(git.server.StderrPath)),
		"provider.argv":        string(mustRead(forge.providerArgvPath)),
		"provider.stdin":       string(mustRead(forge.providerInputPath)),
		"provider.stdout":      string(mustRead(forge.providerOutputPath)),
		"provider.environment": string(mustRead(forge.providerEnvPath)),
		"provider.stderr":      string(mustRead(forge.providerStderrPath)),
		"checkout":             string(mustRead(filepath.Join(checkout, "pack.txt"))),
	}
	allowed := map[string]map[string]bool{
		agentToken:         {},
		providerCredential: {"provider.environment": true},
		environmentMarker:  {"provider.environment": true},
		issueBodyMarker:    {"client.stdout": true, "provider.stdin": true, "provider.stdout": true},
		commentMarker:      {"provider.stdin": true, "provider.stdout": true},
		packMarker:         {"checkout": true},
		providerStderr:     {"provider.stderr": true},
		argvMarker:         {"provider.argv": true},
		sshStderrMarker:    {},
	}
	for marker, intended := range allowed {
		locations := markerLocations(channels, marker)
		if !reflect.DeepEqual(locations, intended) {
			t.Errorf("marker %q locations = %v, want %v", marker, locations, intended)
		}
	}
	if environment := channels["provider.environment"]; !strings.Contains(environment, "REPOWOLF_TOKEN_AGENT=unset") || !strings.Contains(environment, "REPOWOLF_ENDPOINT=unset") {
		t.Errorf("provider environment retained RepoWolf controls: %q", environment)
	}
	assertNoFixtureProcess(t, forge.root)
	assertNoFixtureProcess(t, git.root)
	assertRepositoryUnchanged(t, forge.sourceStatus)
	assertRepositoryUnchanged(t, git.sourceStatus)
}

func TestUnknownAndUnauthorizedRepositoriesHaveIdenticalGRPCStatus(t *testing.T) {
	fixture := newFixture(t)
	connection, err := clientconfig.Dial(context.Background(), clientconfig.Config{
		Endpoint: fixture.server.Endpoint, Token: agentToken, CAFile: fixture.server.Certificate.CAFile,
		ServerName: fixture.server.Certificate.ServerName,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := repowolfv1.NewGitHubServiceClient(connection)

	call := func(owner, name string) ([]byte, *status.Status) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, callErr := client.Execute(ctx, &repowolfv1.GitHubRequest{
			Context:   &repowolfv1.RequestContext{Repository: &repowolfv1.RepositorySelector{Host: "github.com", Owner: owner, Name: name}},
			Operation: &repowolfv1.GitHubRequest_RepositoryView{RepositoryView: &repowolfv1.GitHubRepositoryViewRequest{}},
		})
		if callErr == nil {
			t.Fatalf("%s/%s unexpectedly authorized", owner, name)
		}
		encoded, marshalErr := proto.MarshalOptions{Deterministic: true}.Marshal(status.Convert(callErr).Proto())
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return encoded, status.Convert(callErr)
	}
	unknown, unknownStatus := call("alpha", "missing")
	unauthorized, _ := call("beta", "repo")
	if string(unknown) != string(unauthorized) {
		t.Fatalf("status bytes differ: unknown=%x unauthorized=%x", unknown, unauthorized)
	}
	if unknownStatus.Code().String() != "PermissionDenied" || unknownStatus.Message() != "permission denied" {
		t.Fatalf("canonical denial = %s / %q", unknownStatus.Code(), unknownStatus.Message())
	}
	fixture.stop(t)
	assertNoFixtureProcess(t, fixture.root)
	assertRepositoryUnchanged(t, fixture.sourceStatus)
}

func markerLocations(channels map[string]string, marker string) map[string]bool {
	locations := make(map[string]bool)
	for channel, contents := range channels {
		if strings.Contains(contents, marker) {
			locations[channel] = true
		}
	}
	return locations
}

func TestInvalidBearerTokenIsRejectedWithoutProviderExecution(t *testing.T) {
	fixture := newFixture(t)
	wrongToken := "rw1_AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI"
	// Authentication failure is covered through the same real TLS transport
	// without ever invoking host gh or the fake provider.
	connection, err := clientconfig.Dial(context.Background(), clientconfig.Config{
		Endpoint: fixture.server.Endpoint, Token: wrongToken, CAFile: fixture.server.Certificate.CAFile,
		ServerName: fixture.server.Certificate.ServerName,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, callErr := repowolfv1.NewGitHubServiceClient(connection).Execute(context.Background(), &repowolfv1.GitHubRequest{
		Context:   &repowolfv1.RequestContext{Repository: &repowolfv1.RepositorySelector{Host: "github.com", Owner: "alpha", Name: "repo"}},
		Operation: &repowolfv1.GitHubRequest_RepositoryView{RepositoryView: &repowolfv1.GitHubRepositoryViewRequest{}},
	})
	connection.Close()
	if converted := status.Convert(callErr); converted.Code().String() != "Unauthenticated" || converted.Message() != "authentication required" {
		t.Fatalf("invalid bearer status = %s / %q", converted.Code(), converted.Message())
	}
	fixture.stop(t)
	assertNoFixtureProcess(t, fixture.root)
	assertRepositoryUnchanged(t, fixture.sourceStatus)
	if _, err := os.Stat(fixture.providerArgvPath); !os.IsNotExist(err) {
		t.Fatalf("provider executed for invalid bearer: %v", err)
	}
}
