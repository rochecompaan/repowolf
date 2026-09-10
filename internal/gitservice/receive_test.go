package gitservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/runner"
)

const (
	testOID1 = "1111111111111111111111111111111111111111"
	testOID2 = "2222222222222222222222222222222222222222"
)

func TestReceivePackAcceptsOmittedPortBeforeForwardingExactBytes(t *testing.T) {
	service, capture, _ := receiveExecutableService(t)
	prefix := receivePrefix("refs/heads/feature")
	pack := []byte("PACK\x00payload")
	client := append(append([]byte(nil), prefix...), pack...)
	stream := receiveStreamAtPort(client, 0)

	if err := service.receivePack(stream); err != nil {
		t.Fatalf("receivePack: %v", err)
	}
	got, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, client) {
		t.Fatalf("provider input = %q, want exact %q", got, client)
	}
	assertTerminalCategory(t, stream, repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED)
	if len(stream.sent) < 2 || !bytes.Equal(stream.sent[0].GetData().GetData(), advertisement()) {
		t.Fatalf("advertisement not relayed exactly: %#v", stream.sent)
	}
}

func TestReceivePackDeniedRefForwardsZeroClientBytes(t *testing.T) {
	service, capture, auditOutput := receiveExecutableService(t)
	stream := receiveStream(receivePrefix("refs/heads/main"))

	if err := service.receivePack(stream); err != nil {
		t.Fatalf("receivePack: %v", err)
	}
	got, err := os.ReadFile(capture)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("provider received %d denied update bytes", len(got))
	}
	assertTerminalCategory(t, stream, repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_INVALID_REQUEST)
	events := decodeAuditEvents(t, auditOutput)
	if len(events) != 2 || events[0].Outcome != audit.OutcomeAccepted || events[1].Outcome != audit.OutcomeDenied {
		t.Fatalf("audit events = %#v", events)
	}
	terminal := events[1]
	if terminal.Repository != "project" || terminal.InputBytes != 0 || terminal.UpdateCount != 1 || len(terminal.Refs) != 1 || terminal.Refs[0] != "refs/heads/main" {
		t.Fatalf("terminal audit = %#v", terminal)
	}
}

func TestReceivePackMalformedPrefixForwardsZeroClientBytes(t *testing.T) {
	service, capture, _ := receiveExecutableService(t)
	stream := receiveStream([]byte("zzzzmalformed"))

	if err := service.receivePack(stream); err != nil {
		t.Fatalf("receivePack: %v", err)
	}
	got, err := os.ReadFile(capture)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("provider received %d malformed update bytes", len(got))
	}
	assertTerminalCategory(t, stream, repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_INVALID_REQUEST)
}

func TestReceiveCommandUsesTrustedGiteaConfiguration(t *testing.T) {
	service := newGiteaTestService(t, config.GitRead, config.GitWrite)
	ctx := auth.WithPrincipal(context.Background(), "agent")
	open := &repowolfv1.GitOpen{Repository: &repowolfv1.RepositorySelector{
		SshUser: "forge_user", Host: "gitea.example", Owner: "team_name", Name: "repo.one", SshPort: 2222,
	}}

	repository, command, err := service.receiveCommand(ctx, open)
	if err != nil {
		t.Fatalf("receiveCommand: %v", err)
	}
	want := []string{"-T", "-p", "2222", "--", "forge_user@Gitea.Example", "git-receive-pack 'Team_Name/Repo.One.git'"}
	if !reflect.DeepEqual(command.Args, want) || repository.ID != "gitea-read" {
		t.Fatalf("repository=%#v argv=%#v, want repository gitea-read argv %#v", repository, command.Args, want)
	}
}

func TestReceivePackGiteaAppliesSharedPushPolicy(t *testing.T) {
	for _, test := range []struct {
		name        string
		push        config.PushPolicy
		prefix      []byte
		want        repowolfv1.GitTerminalCategory
		wantOutcome audit.Outcome
		wantRefs    []string
	}{
		{name: "allowed", push: config.PushPolicy{DenyRefs: []string{"refs/heads/main"}, DenyDeletes: true, MaxRefUpdates: 4}, prefix: receivePrefix("refs/heads/feature"), want: repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED, wantOutcome: audit.OutcomeCompleted, wantRefs: []string{"refs/heads/feature"}},
		{name: "denied ref", push: config.PushPolicy{DenyRefs: []string{"refs/heads/main"}, MaxRefUpdates: 4}, prefix: receivePrefix("refs/heads/main"), want: repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_INVALID_REQUEST, wantOutcome: audit.OutcomeDenied, wantRefs: []string{"refs/heads/main"}},
		{name: "denied delete", push: config.PushPolicy{DenyDeletes: true, MaxRefUpdates: 4}, prefix: receiveDeletePrefix("refs/heads/feature"), want: repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_INVALID_REQUEST, wantOutcome: audit.OutcomeDenied, wantRefs: []string{"refs/heads/feature"}},
		{name: "too many updates", push: config.PushPolicy{MaxRefUpdates: 1}, prefix: receiveTwoUpdatePrefix(), want: repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_INVALID_REQUEST, wantOutcome: audit.OutcomeDenied, wantRefs: []string{"refs/heads/feature", "refs/heads/other"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, capture, auditOutput := receiveExecutableGiteaService(t, test.push)
			stream := giteaReceiveStream(test.prefix)
			if err := service.receivePack(stream); err != nil {
				t.Fatal(err)
			}
			input, err := os.ReadFile(capture)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			wantInput := test.want == repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED
			if wantInput && !bytes.Equal(input, test.prefix) {
				t.Fatalf("provider input = %q, want exact %q", input, test.prefix)
			}
			if !wantInput && len(input) != 0 {
				t.Fatalf("provider received %d denied bytes", len(input))
			}
			assertTerminalCategory(t, stream, test.want)
			events := decodeAuditEvents(t, auditOutput)
			if len(events) != 2 || events[0].Outcome != audit.OutcomeAccepted || events[0].Provider != "gitea" {
				t.Fatalf("audit events = %#v", events)
			}
			terminal := events[1]
			if terminal.Outcome != test.wantOutcome || terminal.Repository != "gitea-read" || terminal.Provider != "gitea" || terminal.InputBytes != int64(len(input)) || terminal.UpdateCount != len(test.wantRefs) || !reflect.DeepEqual(terminal.Refs, test.wantRefs) {
				t.Fatalf("terminal audit = %#v", terminal)
			}
		})
	}
}

func TestReceivePackGiteaCancellationReapsProviderAndWritesCancelledAudit(t *testing.T) {
	const sensitive = "sensitive-gitea-cancellation-marker"
	service, auditOutput, cwdFile, pidFile, readyFile := cancellableGiteaReceiveService(t, sensitive)
	ctx, cancel := context.WithCancel(auth.WithPrincipal(context.Background(), "agent"))
	stream := giteaReceiveStream(receivePrefix("refs/heads/feature"))
	stream.ctx = ctx
	result := make(chan error, 1)
	go func() { result <- service.receivePack(stream) }()

	deadline := time.Now().Add(time.Second)
	for fileSizeForTest(readyFile) < 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if fileSizeForTest(readyFile) < 0 {
		cancel()
		t.Fatal("Gitea receive-pack provider did not start")
	}
	started := time.Now()
	cancel()
	var receiveErr error
	select {
	case receiveErr = <-result:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancelled Gitea receive-pack did not return promptly")
	}
	if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
		t.Fatalf("cancelled Gitea receive-pack took %v", elapsed)
	}
	assertProcessCleanup(t, cwdFile, pidFile)
	events := decodeAuditEvents(t, auditOutput)
	if len(events) != 2 || events[0].Outcome != audit.OutcomeAccepted {
		t.Fatalf("audit events = %#v", events)
	}
	terminal := events[1]
	if terminal.Outcome != audit.OutcomeCancelled || terminal.Provider != "gitea" || terminal.Repository != "gitea-read" || terminal.Operation != "git.receive-pack" {
		t.Fatalf("terminal audit = %#v", terminal)
	}
	if strings.Contains(fmt.Sprint(receiveErr), sensitive) || bytes.Contains(auditOutput.Bytes(), []byte(sensitive)) {
		t.Fatal("sensitive cancellation marker escaped into error or audit")
	}
}

func TestReceivePackGiteaAuthorizationDenialsBeforeInput(t *testing.T) {
	valid := &repowolfv1.RepositorySelector{SshUser: "forge_user", Host: "gitea.example", Owner: "team_name", Name: "repo.one", SshPort: 2222}
	mismatch := config.Config{
		Providers: map[string]config.Provider{"gitea": {Kind: config.ProviderGitea, GitHost: "Gitea.Example", SSHUser: "forge_user", SSHPort: 2222}},
		Repositories: map[string]config.Repository{
			"gitea-read":  {Provider: "gitea", Owner: "Team_Name", Name: "Repo.One", Git: config.PushPolicy{MaxRefUpdates: 4}},
			"gitea-write": {Provider: "gitea", Owner: "Team_Name", Name: "Repo.One", Git: config.PushPolicy{MaxRefUpdates: 4}},
		},
		Principals: map[string]config.Principal{"agent": {Grants: []config.Grant{
			{Repository: "gitea-read", Capabilities: []config.Capability{config.GitRead}},
			{Repository: "gitea-write", Capabilities: []config.Capability{config.GitWrite}},
		}}},
	}
	unsupported := config.Config{
		Providers:    map[string]config.Provider{"other": {Kind: config.ProviderKind("gitlab"), GitHost: "gitea.example", SSHUser: "forge_user", SSHPort: 2222}},
		Repositories: map[string]config.Repository{"other": {Provider: "other", Owner: "team_name", Name: "repo.one", Git: config.PushPolicy{MaxRefUpdates: 4}}},
		Principals:   map[string]config.Principal{"agent": {Grants: []config.Grant{{Repository: "other", Capabilities: []config.Capability{config.GitRead, config.GitWrite}}}}},
	}
	for _, test := range []struct {
		name         string
		capabilities []config.Capability
		selector     *repowolfv1.RepositorySelector
		policy       *config.Config
	}{
		{name: "missing read", capabilities: []config.Capability{config.GitWrite}, selector: valid},
		{name: "missing write", capabilities: []config.Capability{config.GitRead}, selector: valid},
		{name: "wrong SSH user", capabilities: []config.Capability{config.GitRead, config.GitWrite}, selector: &repowolfv1.RepositorySelector{SshUser: "git", Host: valid.Host, Owner: valid.Owner, Name: valid.Name, SshPort: valid.SshPort}},
		{name: "wrong host", capabilities: []config.Capability{config.GitRead, config.GitWrite}, selector: &repowolfv1.RepositorySelector{SshUser: valid.SshUser, Host: "evil.example", Owner: valid.Owner, Name: valid.Name, SshPort: valid.SshPort}},
		{name: "wrong port", capabilities: []config.Capability{config.GitRead, config.GitWrite}, selector: &repowolfv1.RepositorySelector{SshUser: valid.SshUser, Host: valid.Host, Owner: valid.Owner, Name: valid.Name, SshPort: 22}},
		{name: "ungranted repository", capabilities: []config.Capability{config.GitRead, config.GitWrite}, selector: &repowolfv1.RepositorySelector{SshUser: valid.SshUser, Host: valid.Host, Owner: valid.Owner, Name: "missing", SshPort: valid.SshPort}},
		{name: "read write repository mismatch", selector: valid, policy: &mismatch},
		{name: "unsupported provider kind", selector: valid, policy: &unsupported},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := newGiteaTestService(t, test.capabilities...)
			if test.policy != nil {
				snapshot, err := policy.New(*test.policy)
				if err != nil {
					t.Fatal(err)
				}
				service.options.Policy = snapshot
			}
			counter := &countingProcessRunner{}
			service.options.Runner = counter
			service.options.Audit = audit.NewWriter(io.Discard)
			stream := &memoryStream{ctx: auth.WithPrincipal(context.Background(), "agent"), received: []*repowolfv1.GitFrame{
				{Payload: &repowolfv1.GitFrame_Open{Open: &repowolfv1.GitOpen{Repository: test.selector}}},
				dataFrame([]byte("must remain unread")),
			}}

			if err := service.receivePack(stream); err != nil {
				t.Fatal(err)
			}
			if counter.starts != 0 || stream.recvAt != 1 || stream.sent[0].GetTerminal().GetCategory() != repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_PERMISSION_DENIED {
				t.Fatalf("starts=%d recvAt=%d sent=%#v", counter.starts, stream.recvAt, stream.sent)
			}
		})
	}
}

func cancellableGiteaReceiveService(t *testing.T, sensitive string) (*Service, *bytes.Buffer, string, string, string) {
	t.Helper()
	service, _, auditOutput := receiveExecutableGiteaService(t, config.PushPolicy{MaxRefUpdates: 4})
	service.options.Limits.IdleStreamTimeout = time.Second
	directory := t.TempDir()
	path := filepath.Join(directory, "ssh")
	cwdFile := filepath.Join(directory, "cwd")
	pidFile := filepath.Join(directory, "pid")
	readyFile := filepath.Join(directory, "ready")
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Fatal(err)
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!" + shell + "\n" +
		"pwd >\"$CWDFILE\"\nprintf '%s\\n' \"$$\" >\"$PIDFILE\"\n" +
		"printf '" + shellOctal(advertisement()) + "'\n" +
		"\"$CAT\" >/dev/null\n: >\"$READYFILE\"\nexec \"$SLEEP\" 30\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	service.options.SSHPath = path
	service.options.Environment = []string{"CAT=" + cat, "SLEEP=" + sleep, "CWDFILE=" + cwdFile, "PIDFILE=" + pidFile, "READYFILE=" + readyFile, "SENSITIVE=" + sensitive}
	return service, auditOutput, cwdFile, pidFile, readyFile
}

func fileSizeForTest(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.Size()
}

func receiveExecutableGiteaService(t *testing.T, push config.PushPolicy) (*Service, string, *bytes.Buffer) {
	t.Helper()
	service := newGiteaTestService(t, config.GitRead, config.GitWrite)
	snapshot, err := policy.New(config.Config{
		Providers: map[string]config.Provider{"gitea": {
			Kind: config.ProviderGitea, GitHost: "Gitea.Example", SSHUser: "forge_user", SSHPort: 2222,
		}},
		Repositories: map[string]config.Repository{"gitea-read": {
			Provider: "gitea", Owner: "Team_Name", Name: "Repo.One", Git: push,
		}},
		Principals: map[string]config.Principal{"agent": {
			Grants: []config.Grant{{Repository: "gitea-read", Capabilities: []config.Capability{config.GitRead, config.GitWrite}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.options.Policy = snapshot
	directory := t.TempDir()
	path := filepath.Join(directory, "ssh")
	capture := filepath.Join(directory, "provider-input")
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!" + shell + "\n" +
		"[ \"$5\" = forge_user@Gitea.Example ] && [ \"$6\" = \"git-receive-pack 'Team_Name/Repo.One.git'\" ] || exit 92\n" +
		"printf '" + shellOctal(advertisement()) + "'\n" +
		"exec \"$CAT\" >\"$CAPTURE\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	service.options.SSHPath = path
	service.options.Environment = []string{"CAT=" + cat, "CAPTURE=" + capture}
	service.options.Runner = &runner.Runner{}
	auditOutput := &bytes.Buffer{}
	service.options.Audit = audit.NewWriter(auditOutput)
	return service, capture, auditOutput
}

func giteaReceiveStream(data []byte) *memoryStream {
	frames := []*repowolfv1.GitFrame{{Payload: &repowolfv1.GitFrame_Open{Open: &repowolfv1.GitOpen{Repository: &repowolfv1.RepositorySelector{
		SshUser: "forge_user", Host: "gitea.example", Owner: "team_name", Name: "repo.one", SshPort: 2222,
	}}}}}
	for len(data) > 0 {
		size := len(data)
		if size > 17 {
			size = 17
		}
		frames = append(frames, dataFrame(append([]byte(nil), data[:size]...)))
		data = data[size:]
	}
	return &memoryStream{ctx: auth.WithPrincipal(context.Background(), "agent"), received: frames}
}

type countingProcessRunner struct{ starts int }

func (counter *countingProcessRunner) Start(context.Context, runner.Command) (*runner.Process, error) {
	counter.starts++
	return nil, errors.New("unexpected process start")
}

func receiveExecutableService(t *testing.T) (*Service, string, *bytes.Buffer) {
	t.Helper()
	service := newTestService(t, config.GitRead, config.GitWrite)
	directory := t.TempDir()
	path := filepath.Join(directory, "ssh")
	capture := filepath.Join(directory, "provider-input")
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!" + shell + "\n" +
		"[ \"$6\" = \"git-receive-pack 'trusted-owner/trusted-repo.git'\" ] || exit 92\n" +
		"printf '" + shellOctal(advertisement()) + "'\n" +
		"exec \"$CAT\" >\"$CAPTURE\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	service.options.SSHPath = path
	service.options.Environment = []string{"CAT=" + cat, "CAPTURE=" + capture}
	service.options.Runner = &runner.Runner{}
	auditOutput := &bytes.Buffer{}
	service.options.Audit = audit.NewWriter(auditOutput)
	return service, capture, auditOutput
}

func receiveStream(data []byte) *memoryStream {
	return receiveStreamAtPort(data, 2222)
}

func receiveStreamAtPort(data []byte, port uint32) *memoryStream {
	frames := []*repowolfv1.GitFrame{openFrame("git.example", "trusted-owner", "trusted-repo", port)}
	for len(data) > 0 {
		size := len(data)
		if size > 17 {
			size = 17
		}
		frames = append(frames, dataFrame(append([]byte(nil), data[:size]...)))
		data = data[size:]
	}
	return &memoryStream{ctx: auth.WithPrincipal(context.Background(), "agent"), received: frames}
}

func advertisement() []byte {
	return append(pkt(testOID1+" refs/heads/feature\x00report-status delete-refs\n"), []byte("0000")...)
}

func receivePrefix(ref string) []byte {
	return append(pkt(testOID1+" "+testOID2+" "+ref+"\x00report-status"), []byte("0000")...)
}

func receiveDeletePrefix(ref string) []byte {
	return append(pkt(testOID1+" "+strings.Repeat("0", 40)+" "+ref+"\x00report-status"), []byte("0000")...)
}

func receiveTwoUpdatePrefix() []byte {
	prefix := pkt(testOID1 + " " + testOID2 + " refs/heads/feature\x00report-status")
	prefix = append(prefix, pkt(testOID1+" "+testOID2+" refs/heads/other")...)
	return append(prefix, []byte("0000")...)
}

func pkt(payload string) []byte { return []byte(fmt.Sprintf("%04x%s", len(payload)+4, payload)) }

func shellOctal(data []byte) string {
	var result strings.Builder
	for _, value := range data {
		fmt.Fprintf(&result, "\\%03o", value)
	}
	return result.String()
}

func decodeAuditEvents(t *testing.T, output *bytes.Buffer) []audit.Event {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	var events []audit.Event
	for decoder.More() {
		var event audit.Event
		if err := decoder.Decode(&event); err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		events = append(events, event)
	}
	return events
}

func assertTerminalCategory(t *testing.T, stream *memoryStream, want repowolfv1.GitTerminalCategory) {
	t.Helper()
	if len(stream.sent) == 0 {
		t.Fatal("no server frames")
	}
	terminal := stream.sent[len(stream.sent)-1].GetTerminal()
	if terminal == nil || terminal.Category != want {
		t.Fatalf("terminal = %#v, want %s", terminal, want)
	}
}
