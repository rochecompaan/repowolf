package gitservice

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/runner"
)

func TestUploadPackInitialFrameTimeout(t *testing.T) {
	service := newTestService(t, config.GitRead)
	service.options.Limits.InitialStreamTimeout = 30 * time.Millisecond
	auditOutput := &bytes.Buffer{}
	service.options.Audit = audit.NewWriter(auditOutput)
	ctx, cancel := context.WithCancel(auth.WithPrincipal(context.Background(), "agent"))
	stream := &initiallyBlockedStream{memoryStream: memoryStream{ctx: ctx}}
	defer cancel()

	started := time.Now()
	if err := service.uploadPack(stream); err != nil {
		t.Fatalf("uploadPack: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("initial timeout took %v", elapsed)
	}
	assertTerminalCategory(t, &stream.memoryStream, repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_DEADLINE_EXCEEDED)
	events := decodeAuditEvents(t, auditOutput)
	if len(events) != 1 || events[0].Outcome != audit.OutcomeCancelled {
		t.Fatalf("audit events = %#v", events)
	}
}

func TestUploadPackOperationTimeoutReapsAndRemovesCWD(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	service, auditOutput, cwdFile, pidFile := uploadLifecycleService(t, "exec \"$SLEEP\" 10\n", []string{"SLEEP=" + sleep})
	service.options.Limits.OperationTimeout = 40 * time.Millisecond
	service.options.Limits.IdleStreamTimeout = time.Second
	stream := uploadFrames()

	if err := service.uploadPack(stream); err != nil {
		t.Fatalf("uploadPack: %v", err)
	}
	assertTerminalCategory(t, stream, repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_DEADLINE_EXCEEDED)
	assertProcessCleanup(t, cwdFile, pidFile)
	assertGitAuditPair(t, auditOutput, "git.upload-pack", 0, 0, nil)
}

func TestUploadPackDirectionLimitsUseInjectedLowBounds(t *testing.T) {
	t.Run("input", func(t *testing.T) {
		sleep, err := exec.LookPath("sleep")
		if err != nil {
			t.Fatal(err)
		}
		service, _, _, _ := uploadLifecycleService(t, "exec \"$SLEEP\" 10\n", []string{"SLEEP=" + sleep})
		service.options.Limits.MaxGitBytesPerDirection = 32
		stream := uploadFrames(dataFrame(bytes.Repeat([]byte{'x'}, 33)))
		if err := service.uploadPack(stream); err != nil {
			t.Fatalf("uploadPack: %v", err)
		}
		assertTerminalCategory(t, stream, repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_LIMIT_EXCEEDED)
	})

	t.Run("output", func(t *testing.T) {
		service, _, cwdFile, pidFile := uploadLifecycleService(t, "printf 012345678901234567890123456789012\n", nil)
		service.options.Limits.MaxGitBytesPerDirection = 32
		stream := uploadFrames()
		if err := service.uploadPack(stream); err != nil {
			t.Fatalf("uploadPack: %v", err)
		}
		assertTerminalCategory(t, stream, repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_LIMIT_EXCEEDED)
		assertProcessCleanup(t, cwdFile, pidFile)
	})
}

func TestUploadPackStderrOverflowIsLimitedAndSanitized(t *testing.T) {
	head, err := exec.LookPath("head")
	if err != nil {
		t.Fatal(err)
	}
	service, auditOutput, cwdFile, pidFile := uploadLifecycleService(t, "\"$HEAD\" -c 1048577 /dev/zero >&2\n", []string{"HEAD=" + head})
	stream := uploadFrames()

	if err := service.uploadPack(stream); err != nil {
		t.Fatalf("uploadPack: %v", err)
	}
	assertTerminalCategory(t, stream, repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_LIMIT_EXCEEDED)
	assertProcessCleanup(t, cwdFile, pidFile)
	if bytes.Contains(auditOutput.Bytes(), []byte{0}) {
		t.Fatal("audit contains raw stderr bytes")
	}
}

func TestUploadPackBlockedProviderWriteStopsAtOperationTimeout(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	service, _, cwdFile, pidFile := uploadLifecycleService(t, "exec \"$SLEEP\" 10\n", []string{"SLEEP=" + sleep})
	service.options.Limits.OperationTimeout = 50 * time.Millisecond
	service.options.Limits.IdleStreamTimeout = time.Second
	frames := make([]*repowolfv1.GitFrame, 0, 34)
	for range 32 {
		frames = append(frames, dataFrame(bytes.Repeat([]byte{'x'}, maxChunkBytes)))
	}
	stream := uploadFrames(frames...)

	started := time.Now()
	if err := service.uploadPack(stream); err != nil {
		t.Fatalf("uploadPack: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("blocked write cleanup took %v", elapsed)
	}
	assertTerminalCategory(t, stream, repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_DEADLINE_EXCEEDED)
	assertProcessCleanup(t, cwdFile, pidFile)
}

func uploadLifecycleService(t *testing.T, body string, environment []string) (*Service, *bytes.Buffer, string, string) {
	t.Helper()
	service := newTestService(t, config.GitRead)
	directory := t.TempDir()
	path := filepath.Join(directory, "ssh")
	cwdFile := filepath.Join(directory, "cwd")
	pidFile := filepath.Join(directory, "pid")
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!" + shell + "\npwd >\"$CWDFILE\"\nprintf '%s\\n' \"$$\" >\"$PIDFILE\"\n" + body
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	auditOutput := &bytes.Buffer{}
	service.options.SSHPath = path
	service.options.Environment = append(environment, "CWDFILE="+cwdFile, "PIDFILE="+pidFile)
	service.options.Runner = &runner.Runner{}
	service.options.Audit = audit.NewWriter(auditOutput)
	return service, auditOutput, cwdFile, pidFile
}

func uploadFrames(frames ...*repowolfv1.GitFrame) *memoryStream {
	received := []*repowolfv1.GitFrame{openFrame("git.example", "trusted-owner", "trusted-repo", 2222)}
	received = append(received, frames...)
	return &memoryStream{ctx: auth.WithPrincipal(context.Background(), "agent"), received: received}
}

type initiallyBlockedStream struct{ memoryStream }

func (stream *initiallyBlockedStream) Recv() (*repowolfv1.GitFrame, error) {
	<-stream.Context().Done()
	return nil, io.EOF
}
