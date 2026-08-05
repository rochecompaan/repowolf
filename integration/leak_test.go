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
		t.Fatalf("offline clone: %v; stdout=%q stderr=%q", clone.err, clone.stdout, clone.stderr)
	}
	git.gitOK(t, checkout, "config", "user.name", "Task 13")
	git.gitOK(t, checkout, "config", "user.email", "task13@invalid")
	git.gitOK(t, checkout, "switch", "-c", "leak-matrix")
	if err := os.WriteFile(filepath.Join(checkout, "allowed.txt"), []byte(allowedContentMarker+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git.gitOK(t, checkout, "add", "allowed.txt")
	git.gitOK(t, checkout, "commit", "-m", "allowed leak matrix content")
	allowedPush := git.git(t, checkout, "push", "origin", "HEAD:refs/heads/"+allowedUpdateMarker)
	if allowedPush.err != nil {
		t.Fatalf("allowed push: %v; stdout=%q stderr=%q", allowedPush.err, allowedPush.stdout, allowedPush.stderr)
	}
	allowedReceiveInput := string(mustRead(git.receiveInput))
	if allowedReceiveInput == "" {
		t.Fatal("allowed receive-pack forwarded no bytes")
	}
	allowedRemote := git.git(t, git.remote, "show", "refs/heads/"+allowedUpdateMarker+":allowed.txt")
	if allowedRemote.err != nil || allowedRemote.stdout != allowedContentMarker+"\n" {
		t.Fatalf("allowed remote content: %v; stdout=%q stderr=%q", allowedRemote.err, allowedRemote.stdout, allowedRemote.stderr)
	}
	if err := os.WriteFile(filepath.Join(checkout, "denied.txt"), []byte(deniedContentMarker+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git.gitOK(t, checkout, "add", "denied.txt")
	git.gitOK(t, checkout, "commit", "-m", "denied leak matrix content")
	deniedPush := git.git(t, checkout, "push", "origin", "HEAD:refs/heads/"+deniedUpdateMarker)
	if deniedPush.err == nil || !strings.Contains(deniedPush.stderr, "repowolf git transport failed") {
		t.Fatalf("denied push: %v; stdout=%q stderr=%q", deniedPush.err, deniedPush.stdout, deniedPush.stderr)
	}
	deniedReceiveInput := string(mustRead(git.receiveInput))
	if deniedReceiveInput != "" {
		t.Fatalf("denied receive-pack forwarded %d bytes", len(deniedReceiveInput))
	}
	deniedRemote := git.git(t, git.remote, "show", "refs/heads/"+deniedUpdateMarker+":denied.txt")
	if deniedRemote.err == nil || deniedRemote.stdout != "" {
		t.Fatalf("denied remote content exists: %v; stdout=%q stderr=%q", deniedRemote.err, deniedRemote.stdout, deniedRemote.stderr)
	}
	git.server.Stop(t)

	channels := map[string]string{
		"forge.client.stdout":   listOut + createOut + commentOut,
		"forge.client.stderr":   listErr + createErr + commentErr,
		"git.clone.stdout":      clone.stdout,
		"git.clone.stderr":      clone.stderr,
		"git.allowed.stdout":    allowedPush.stdout,
		"git.allowed.stderr":    allowedPush.stderr,
		"git.denied.stdout":     deniedPush.stdout,
		"git.denied.stderr":     deniedPush.stderr,
		"git.remote.stdout":     allowedRemote.stdout + deniedRemote.stdout,
		"git.remote.stderr":     allowedRemote.stderr + deniedRemote.stderr,
		"forge.audit":           forge.audit(),
		"git.audit":             string(mustRead(git.server.AuditPath)),
		"server.stderr":         string(mustRead(forge.server.StderrPath)) + string(mustRead(git.server.StderrPath)),
		"provider.argv":         string(mustRead(forge.providerArgvPath)),
		"provider.stdin":        string(mustRead(forge.providerInputPath)),
		"provider.stdout":       string(mustRead(forge.providerOutputPath)),
		"provider.environment":  string(mustRead(forge.providerEnvPath)),
		"provider.stderr":       string(mustRead(forge.providerStderrPath)),
		"ssh.argv":              string(mustRead(git.sshArgv)),
		"ssh.environment":       string(mustRead(git.sshEnvironment)),
		"ssh.upload.input":      string(mustRead(git.uploadInput)),
		"ssh.receive.allowed":   allowedReceiveInput,
		"ssh.receive.denied":    deniedReceiveInput,
		"git.checkout.contents": string(mustRead(filepath.Join(checkout, "pack.txt"))) + string(mustRead(filepath.Join(checkout, "allowed.txt"))) + string(mustRead(filepath.Join(checkout, "denied.txt"))),
	}
	assertAuditInvocations(t, channels["forge.audit"], forgeAuditExpectations(
		"github.issue_list", "github.issue_create", "github.issue_comment",
	), auditLeakMarkers())
	gitForbidden := append(auditLeakMarkers(), allowedContentMarker, deniedContentMarker)
	assertAuditInvocations(t, channels["git.audit"], gitAuditExpectations(
		"refs/heads/"+allowedUpdateMarker, "refs/heads/"+deniedUpdateMarker,
	), gitForbidden)
	allowed := map[string]map[string]bool{
		agentToken:           {},
		providerCredential:   {"provider.environment": true, "ssh.environment": true},
		environmentMarker:    {"provider.environment": true, "ssh.environment": true},
		issueBodyMarker:      {"forge.client.stdout": true, "provider.stdin": true, "provider.stdout": true},
		commentMarker:        {"provider.stdin": true, "provider.stdout": true},
		packMarker:           {"git.checkout.contents": true},
		allowedContentMarker: {"git.checkout.contents": true, "git.remote.stdout": true},
		deniedContentMarker:  {"git.checkout.contents": true},
		allowedUpdateMarker:  {"git.allowed.stderr": true, "git.audit": true, "ssh.receive.allowed": true},
		deniedUpdateMarker:   {"git.audit": true, "git.remote.stderr": true},
		providerStderr:       {"provider.stderr": true},
		argvMarker:           {"provider.argv": true},
		sshStderrMarker:      {},
	}
	for marker, intended := range allowed {
		locations := markerLocations(channels, marker)
		if !reflect.DeepEqual(locations, intended) {
			t.Errorf("marker %q locations = %v, want %v", marker, locations, intended)
		}
	}
	for _, channel := range []string{"provider.environment", "ssh.environment"} {
		environment := channels[channel]
		if !strings.Contains(environment, "REPOWOLF_TOKEN_AGENT=unset") || !strings.Contains(environment, "REPOWOLF_ENDPOINT=unset") {
			t.Errorf("%s retained RepoWolf controls: %q", channel, environment)
		}
	}
	for _, name := range []string{"FAKE_GIT_UPLOAD_PACK", "FAKE_GIT_RECEIVE_PACK", "FAKE_TEE"} {
		value, ok := recordedEnvironmentValue(channels["ssh.environment"], name)
		if !ok || !filepath.IsAbs(value) {
			t.Errorf("fake SSH tool %s is not pinned: %q", name, value)
		}
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

func recordedEnvironmentValue(contents, name string) (string, bool) {
	prefix := name + "="
	for _, line := range strings.Split(contents, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix), true
		}
	}
	return "", false
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
