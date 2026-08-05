package gitservice

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/runner"
)

func TestReceivePackDisconnectDuringAdvertisementReapsAndWritesTerminalAudit(t *testing.T) {
	service, auditOutput, cwdFile, pidFile := blockingReceiveService(t, false)
	disconnected := errors.New("sensitive client disconnect")
	stream := &memoryStream{
		ctx:      auth.WithPrincipal(context.Background(), "agent"),
		received: []*repowolfv1.GitFrame{openFrame("git.example", "trusted-owner", "trusted-repo", 2222)},
		sendErr:  disconnected,
	}

	err := service.receivePack(stream)
	if !errors.Is(err, rpcstatus.ErrServiceUnavailable) {
		t.Fatalf("receivePack error = %v, want safe unavailable", err)
	}
	assertProcessCleanup(t, cwdFile, pidFile)
	assertGitAuditPair(t, auditOutput, "git.receive-pack", 0, 0, nil)
}

func TestReceivePackBlockedAdvertisementReturnsAtIdleAndReaps(t *testing.T) {
	service, auditOutput, cwdFile, pidFile := blockingReceiveService(t, false)
	ctx, cancel := context.WithCancel(auth.WithPrincipal(context.Background(), "agent"))
	stream := newBlockingSendStream(ctx, 1, []*repowolfv1.GitFrame{
		openFrame("git.example", "trusted-owner", "trusted-repo", 2222),
	})

	err := runWithRPCancellation(t, cancel, service.options.Limits.IdleStreamTimeout, func() error {
		return service.receivePack(stream)
	})
	if !errors.Is(err, rpcstatus.ErrServiceUnavailable) {
		t.Fatalf("receivePack error = %v, want unavailable", err)
	}
	stream.assertStopped(t)
	assertProcessCleanup(t, cwdFile, pidFile)
	assertGitAuditPair(t, auditOutput, "git.receive-pack", 0, 0, nil)
}

func TestReceivePackBlockedPostAdvertisementSendReturnsAtIdleAndReaps(t *testing.T) {
	service, auditOutput, cwdFile, pidFile := blockingReceiveService(t, true)
	prefix := receivePrefix("refs/heads/feature")
	frames := receiveStream(prefix).received
	ctx, cancel := context.WithCancel(auth.WithPrincipal(context.Background(), "agent"))
	stream := newBlockingSendStream(ctx, 2, frames)

	err := runWithRPCancellation(t, cancel, service.options.Limits.IdleStreamTimeout, func() error {
		return service.receivePack(stream)
	})
	if !errors.Is(err, rpcstatus.ErrServiceUnavailable) {
		t.Fatalf("receivePack error = %v, want unavailable", err)
	}
	stream.assertStopped(t)
	if stream.MaxConcurrentSends() != 1 {
		t.Fatalf("maximum concurrent sends = %d", stream.MaxConcurrentSends())
	}
	assertProcessCleanup(t, cwdFile, pidFile)
	assertGitAuditPair(t, auditOutput, "git.receive-pack", int64(len(prefix)), int64(len(advertisement())), []string{"refs/heads/feature"})
}

func runWithRPCancellation(t *testing.T, cancel context.CancelFunc, idle time.Duration, call func() error) error {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		err := call()
		cancel() // gRPC cancels the stream context when the handler returns.
		result <- err
	}()
	timer := time.NewTimer(6 * idle)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		cancel()
		<-result
		t.Fatalf("handler did not return within idle timeout bound")
		return nil
	}
}

func blockingReceiveService(t *testing.T, outputAfterInput bool) (*Service, *bytes.Buffer, string, string) {
	t.Helper()
	service := newTestService(t, config.GitRead, config.GitWrite)
	service.options.Limits.IdleStreamTimeout = 50 * time.Millisecond
	service.options.Limits.OperationTimeout = 2 * time.Second
	directory := t.TempDir()
	path := filepath.Join(directory, "ssh")
	cwdFile := filepath.Join(directory, "cwd")
	pidFile := filepath.Join(directory, "pid")
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!" + shell + "\n" +
		"pwd >\"$CWDFILE\"\nprintf '%s\\n' \"$$\" >\"$PIDFILE\"\n" +
		"printf '" + shellOctal(advertisement()) + "'\n"
	if outputAfterInput {
		script += "\"$CAT\" >/dev/null\nprintf post-advertisement-response\n"
	} else {
		script += "exec \"$CAT\" >/dev/null\n"
	}
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	auditOutput := &bytes.Buffer{}
	service.options.SSHPath = path
	service.options.Environment = []string{"CAT=" + cat, "CWDFILE=" + cwdFile, "PIDFILE=" + pidFile}
	service.options.Runner = &runner.Runner{}
	service.options.Audit = audit.NewWriter(auditOutput)
	return service, auditOutput, cwdFile, pidFile
}

func assertProcessCleanup(t *testing.T, cwdFile, pidFile string) {
	t.Helper()
	cwdRaw, err := os.ReadFile(cwdFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(string(bytes.TrimSpace(cwdRaw))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("provider cwd remains: %v", err)
	}
	pidRaw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(bytes.TrimSpace(pidRaw)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("provider process %d remains: %v", pid, err)
	}
}

func assertGitAuditPair(t *testing.T, output *bytes.Buffer, operation string, inputBytes, minimumOutput int64, refs []string) {
	t.Helper()
	events := decodeAuditEvents(t, output)
	if len(events) != 2 || events[0].Outcome != audit.OutcomeAccepted {
		t.Fatalf("audit events = %#v", events)
	}
	terminal := events[1]
	if terminal.Operation != operation || terminal.Repository != "project" || terminal.InputBytes != inputBytes || terminal.OutputBytes < minimumOutput || terminal.UpdateCount != len(refs) {
		t.Fatalf("terminal audit = %#v", terminal)
	}
	for index, ref := range refs {
		if len(terminal.Refs) != len(refs) || terminal.Refs[index] != ref {
			t.Fatalf("terminal refs = %#v", terminal.Refs)
		}
	}
}

type blockingSendStream struct {
	memoryStream
	blockAt int

	mu       sync.Mutex
	calls    int
	active   int
	maximum  int
	returned chan struct{}
}

func newBlockingSendStream(ctx context.Context, blockAt int, received []*repowolfv1.GitFrame) *blockingSendStream {
	return &blockingSendStream{
		memoryStream: memoryStream{ctx: ctx, received: received},
		blockAt:      blockAt,
		returned:     make(chan struct{}),
	}
}

func (stream *blockingSendStream) Send(frame *repowolfv1.GitFrame) error {
	stream.mu.Lock()
	stream.calls++
	call := stream.calls
	stream.active++
	if stream.active > stream.maximum {
		stream.maximum = stream.active
	}
	stream.mu.Unlock()
	defer func() {
		stream.mu.Lock()
		stream.active--
		if call == stream.blockAt {
			close(stream.returned)
		}
		stream.mu.Unlock()
	}()
	if call == stream.blockAt {
		<-stream.Context().Done()
		return stream.Context().Err()
	}
	stream.mu.Lock()
	stream.sent = append(stream.sent, frame)
	stream.mu.Unlock()
	return nil
}

func (stream *blockingSendStream) MaxConcurrentSends() int {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return stream.maximum
}

func (stream *blockingSendStream) assertStopped(t *testing.T) {
	t.Helper()
	select {
	case <-stream.returned:
	case <-time.After(time.Second):
		t.Fatal("blocked Send goroutine did not stop after handler return")
	}
	if stream.MaxConcurrentSends() != 1 {
		t.Fatalf("maximum concurrent sends = %d", stream.MaxConcurrentSends())
	}
}
