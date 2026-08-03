package gitservice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/runner"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

func TestUploadCommandUsesOnlyPinnedRepositoryConfiguration(t *testing.T) {
	service := newTestService(t, config.GitRead)
	ctx := auth.WithPrincipal(context.Background(), "agent")
	open := &repowolfv1.GitOpen{Repository: &repowolfv1.RepositorySelector{
		Host: "git.example", Owner: "trusted-owner", Name: "trusted-repo", SshPort: 2222,
	}}

	command, repository, err := service.command(ctx, open, config.GitRead, "git-upload-pack")
	if err != nil {
		t.Fatalf("command: %v", err)
	}
	if repository.ID != "project" {
		t.Fatalf("repository = %q", repository.ID)
	}
	want := []string{"-T", "-p", "2222", "--", "git@git.example", "git-upload-pack 'trusted-owner/trusted-repo.git'"}
	if !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("argv = %#v, want %#v", command.Args, want)
	}
	if command.Path != "/pinned/ssh" {
		t.Fatalf("path = %q", command.Path)
	}
	if command.StdinLimit != 8<<30 || command.StdoutLimit != 8<<30 {
		t.Fatalf("stream limits = %d/%d", command.StdinLimit, command.StdoutLimit)
	}
}

func TestUploadCommandRejectsInexactOrUnauthorizedRepository(t *testing.T) {
	service := newTestService(t, config.GitRead)
	authorized := auth.WithPrincipal(context.Background(), "agent")
	unauthorized := auth.WithPrincipal(context.Background(), "other")

	for name, test := range map[string]struct {
		ctx      context.Context
		selector *repowolfv1.RepositorySelector
	}{
		"missing host": {authorized, &repowolfv1.RepositorySelector{Owner: "trusted-owner", Name: "trusted-repo", SshPort: 2222}},
		"wrong host":   {authorized, &repowolfv1.RepositorySelector{Host: "evil.example", Owner: "trusted-owner", Name: "trusted-repo", SshPort: 2222}},
		"wrong owner":  {authorized, &repowolfv1.RepositorySelector{Host: "git.example", Owner: "other", Name: "trusted-repo", SshPort: 2222}},
		"wrong port":   {authorized, &repowolfv1.RepositorySelector{Host: "git.example", Owner: "trusted-owner", Name: "trusted-repo", SshPort: 22}},
		"no grant":     {unauthorized, &repowolfv1.RepositorySelector{Host: "git.example", Owner: "trusted-owner", Name: "trusted-repo", SshPort: 2222}},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := service.command(test.ctx, &repowolfv1.GitOpen{Repository: test.selector}, config.GitRead, "git-upload-pack")
			if err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestUploadPackStreamsDataAndOneTerminal(t *testing.T) {
	service, auditOutput := executableTestService(t, config.GitRead)
	stream := &memoryStream{
		ctx: auth.WithPrincipal(context.Background(), "agent"),
		received: []*repowolfv1.GitFrame{
			openFrame("git.example", "trusted-owner", "trusted-repo", 2222),
			dataFrame([]byte("client-request")),
		},
	}

	if err := service.uploadPack(stream); err != nil {
		t.Fatalf("uploadPack: %v", err)
	}
	if len(stream.sent) != 2 {
		t.Fatalf("sent %d frames, want data and terminal", len(stream.sent))
	}
	if got := string(stream.sent[0].GetData().GetData()); got != "server-response" {
		t.Fatalf("response = %q", got)
	}
	if terminal := stream.sent[1].GetTerminal(); terminal == nil || terminal.Category != repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED || terminal.ExitCode != 0 {
		t.Fatalf("terminal = %#v", terminal)
	}
	if auditOutput.Len() == 0 {
		t.Fatal("missing Git audit events")
	}
}

func TestUploadPackDisconnectReapsProviderAndWritesTerminalAudit(t *testing.T) {
	service, auditOutput := executableTestService(t, config.GitRead)
	disconnected := errors.New("sensitive client disconnect")
	stream := &memoryStream{
		ctx: auth.WithPrincipal(context.Background(), "agent"),
		received: []*repowolfv1.GitFrame{
			openFrame("git.example", "trusted-owner", "trusted-repo", 2222), dataFrame([]byte("request")),
		},
		sendErr: disconnected,
	}
	started := time.Now()
	err := service.uploadPack(stream)
	if !errors.Is(err, rpcstatus.ErrServiceUnavailable) || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("uploadPack error = %v, want sanitized unavailable", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("disconnect cleanup took %v", elapsed)
	}
	events := decodeAuditEvents(t, auditOutput)
	if len(events) != 2 || events[0].Outcome != audit.OutcomeAccepted || events[1].Outcome != audit.OutcomeFailed {
		t.Fatalf("audit events = %#v", events)
	}
	terminal := events[1]
	if terminal.Operation != "git.upload-pack" || terminal.Repository != "project" || terminal.UpdateCount != 0 || len(terminal.Refs) != 0 {
		t.Fatalf("terminal audit = %#v", terminal)
	}
}

func TestTerminalDeliveryAndAuditFailuresJoinAsSafeUnavailable(t *testing.T) {
	service, _ := executableTestService(t, config.GitRead)
	sink := &failSecondAuditSink{}
	service.options.Audit = sink
	stream := &memoryStream{
		ctx:      auth.WithPrincipal(context.Background(), "agent"),
		received: []*repowolfv1.GitFrame{openFrame("git.example", "trusted-owner", "trusted-repo", 2222)},
		sendErr:  errors.New("sensitive transport detail"),
	}

	err := service.uploadPack(stream)
	if !errors.Is(err, rpcstatus.ErrServiceUnavailable) || !errors.Is(err, errTerminalDelivery) || !errors.Is(err, errTerminalAudit) || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("uploadPack error = %v, want joined sanitized unavailable", err)
	}
	if sink.Count() != 2 {
		t.Fatalf("audit attempts = %d, want accepted and terminal", sink.Count())
	}
}

func TestUploadPackDenialStartsNoProviderAndSendsPermissionTerminal(t *testing.T) {
	service := newTestService(t, config.GitRead)
	service.options.Runner = &runner.Runner{}
	service.options.Audit = audit.NewWriter(io.Discard)
	stream := &memoryStream{
		ctx:      auth.WithPrincipal(context.Background(), "other"),
		received: []*repowolfv1.GitFrame{openFrame("git.example", "trusted-owner", "trusted-repo", 2222)},
	}
	if err := service.uploadPack(stream); err != nil {
		t.Fatalf("uploadPack: %v", err)
	}
	if len(stream.sent) != 1 || stream.sent[0].GetTerminal().GetCategory() != repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_PERMISSION_DENIED {
		t.Fatalf("sent = %#v", stream.sent)
	}
}

func executableTestService(t *testing.T, capabilities ...config.Capability) (*Service, *bytes.Buffer) {
	t.Helper()
	service := newTestService(t, capabilities...)
	directory := t.TempDir()
	path := filepath.Join(directory, "ssh")
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!" + shell + "\n" +
		"[ \"$1\" = -T ] && [ \"$2\" = -p ] && [ \"$3\" = 2222 ] && [ \"$4\" = -- ] || exit 91\n" +
		"[ \"$5\" = git@git.example ] && [ \"$6\" = \"git-upload-pack 'trusted-owner/trusted-repo.git'\" ] || exit 92\n" +
		"printf server-response\nwhile IFS= read -r line; do :; done\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	auditOutput := &bytes.Buffer{}
	service.options.SSHPath = path
	service.options.Runner = &runner.Runner{}
	service.options.Audit = audit.NewWriter(auditOutput)
	return service, auditOutput
}

type failSecondAuditSink struct {
	mu    sync.Mutex
	calls int
}

func (sink *failSecondAuditSink) Write(audit.Event) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.calls++
	if sink.calls == 2 {
		return errors.New("sensitive audit detail")
	}
	return nil
}

func (sink *failSecondAuditSink) Count() int {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return sink.calls
}

type memoryStream struct {
	ctx      context.Context
	received []*repowolfv1.GitFrame
	sent     []*repowolfv1.GitFrame
	recvAt   int
	sendErr  error
}

func (stream *memoryStream) Context() context.Context { return stream.ctx }
func (stream *memoryStream) Recv() (*repowolfv1.GitFrame, error) {
	if stream.recvAt == len(stream.received) {
		return nil, io.EOF
	}
	frame := stream.received[stream.recvAt]
	stream.recvAt++
	return frame, nil
}
func (stream *memoryStream) Send(frame *repowolfv1.GitFrame) error {
	if stream.sendErr != nil {
		return stream.sendErr
	}
	stream.sent = append(stream.sent, frame)
	return nil
}

func newTestService(t *testing.T, capabilities ...config.Capability) *Service {
	t.Helper()
	cfg := config.Config{
		Providers: map[string]config.Provider{"forge": {
			Kind: config.ProviderGitHub, GitHost: "git.example", SSHUser: "git", SSHPort: 2222,
		}},
		Repositories: map[string]config.Repository{"project": {
			Provider: "forge", Owner: "trusted-owner", Name: "trusted-repo",
			Git: config.PushPolicy{DenyRefs: []string{"refs/heads/main"}, MaxRefUpdates: 4},
		}},
		Principals: map[string]config.Principal{"agent": {
			Grants: []config.Grant{{Repository: "project", Capabilities: capabilities}},
		}},
	}
	snapshot, err := policy.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &Service{options: Options{
		Policy: snapshot, SSHPath: "/pinned/ssh", Limits: config.Limits{
			MaxStreamChunkBytes: 64 << 10, MaxPushPrefixBytes: 1 << 20,
			MaxGitBytesPerDirection: 8 << 30, InitialStreamTimeout: time.Second,
			OperationTimeout: time.Minute, IdleStreamTimeout: time.Second,
		},
	}}
}
