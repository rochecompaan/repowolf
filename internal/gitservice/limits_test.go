package gitservice

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/runner"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func TestClientFrameStateRequiresOneOpenThenData(t *testing.T) {
	open := openFrame("github.example", "owner", "repo", 22)
	data := dataFrame([]byte("request"))
	terminal := terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED, 0)

	tests := []struct {
		name   string
		frames []*repowolfv1.GitFrame
		close  bool
	}{
		{name: "missing open", close: true},
		{name: "data before open", frames: []*repowolfv1.GitFrame{data}},
		{name: "duplicate open", frames: []*repowolfv1.GitFrame{open, open}},
		{name: "client terminal", frames: []*repowolfv1.GitFrame{open, terminal}},
		{name: "nil frame", frames: []*repowolfv1.GitFrame{nil}},
		{name: "empty payload", frames: []*repowolfv1.GitFrame{{}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var state clientFrameState
			var err error
			for _, frame := range test.frames {
				if err = state.Accept(frame); err != nil {
					break
				}
			}
			if err == nil && test.close {
				err = state.Close()
			}
			if !errors.Is(err, errInvalidFrame) {
				t.Fatalf("error = %v, want invalid frame", err)
			}
		})
	}

	var valid clientFrameState
	if err := valid.Accept(open); err != nil {
		t.Fatalf("Accept(open): %v", err)
	}
	if err := valid.Accept(data); err != nil {
		t.Fatalf("Accept(data): %v", err)
	}
	if err := valid.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestClientFrameStateRejectsOversizedData(t *testing.T) {
	var state clientFrameState
	if err := state.Accept(openFrame("github.example", "owner", "repo", 22)); err != nil {
		t.Fatal(err)
	}
	if err := state.Accept(dataFrame(make([]byte, maxChunkBytes+1))); !errors.Is(err, errChunkLimit) {
		t.Fatalf("error = %v, want chunk limit", err)
	}
}

func TestServerFrameStateRequiresDataThenOneTerminal(t *testing.T) {
	var valid serverFrameState
	if terminal, err := valid.Accept(dataFrame([]byte("response"))); err != nil || terminal {
		t.Fatalf("data: terminal=%v error=%v", terminal, err)
	}
	if terminal, err := valid.Accept(terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED, 0)); err != nil || !terminal {
		t.Fatalf("terminal: terminal=%v error=%v", terminal, err)
	}
	if _, err := valid.Accept(dataFrame(nil)); !errors.Is(err, errInvalidFrame) {
		t.Fatalf("post-terminal error = %v", err)
	}

	for name, frame := range map[string]*repowolfv1.GitFrame{
		"open":              openFrame("github.example", "owner", "repo", 22),
		"empty":             {},
		"unspecified":       terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_UNSPECIFIED, 1),
		"completed nonzero": terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED, 1),
	} {
		t.Run(name, func(t *testing.T) {
			var state serverFrameState
			if _, err := state.Accept(frame); !errors.Is(err, errInvalidFrame) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCopyOutputToFramesUsesConfigured64KiBChunks(t *testing.T) {
	stream := &memoryStream{ctx: context.Background()}
	output := bytes.NewReader(make([]byte, 2*maxChunkBytes+1))
	count, err := copyOutputToFrames(output, stream, maxChunkBytes, nil)
	if err != nil {
		t.Fatalf("copyOutputToFrames: %v", err)
	}
	if count != 2*maxChunkBytes+1 || len(stream.sent) != 3 {
		t.Fatalf("count/frames = %d/%d", count, len(stream.sent))
	}
	for index, frame := range stream.sent {
		if size := len(frame.GetData().GetData()); size > maxChunkBytes || (index < 2 && size != maxChunkBytes) {
			t.Fatalf("frame %d size = %d", index, size)
		}
	}
}

func TestUploadIdleTimeoutDoesNotWaitForBlockedClientReceive(t *testing.T) {
	service := newTestService(t, config.GitRead)
	directory := t.TempDir()
	path := filepath.Join(directory, "ssh")
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!" + shell + "\nexec \"$SLEEP\" 10\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	service.options.SSHPath = path
	service.options.Environment = []string{"SLEEP=" + sleep}
	service.options.Runner = &runner.Runner{}
	service.options.Audit = audit.NewWriter(&bytes.Buffer{})
	service.options.Limits.IdleStreamTimeout = 50 * time.Millisecond
	ctx, cancel := context.WithTimeout(auth.WithPrincipal(context.Background(), "agent"), time.Second)
	defer cancel()
	stream := &blockingReceiveStream{ctx: ctx, first: openFrame("git.example", "trusted-owner", "trusted-repo", 2222)}

	started := time.Now()
	if err := service.uploadPack(stream); err != nil {
		t.Fatalf("uploadPack: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("idle cleanup took %v; blocked on client receive", elapsed)
	}
	assertTerminalCategory(t, &stream.memoryStream, repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_DEADLINE_EXCEEDED)
}

type blockingReceiveStream struct {
	memoryStream
	ctx   context.Context
	first *repowolfv1.GitFrame
	read  bool
}

func (stream *blockingReceiveStream) Context() context.Context { return stream.ctx }
func (stream *blockingReceiveStream) Recv() (*repowolfv1.GitFrame, error) {
	if !stream.read {
		stream.read = true
		return stream.first, nil
	}
	<-stream.ctx.Done()
	return nil, stream.ctx.Err()
}

func openFrame(host, owner, name string, port uint32) *repowolfv1.GitFrame {
	return &repowolfv1.GitFrame{Payload: &repowolfv1.GitFrame_Open{Open: &repowolfv1.GitOpen{Repository: &repowolfv1.RepositorySelector{Host: host, Owner: owner, Name: name, SshPort: port}}}}
}

func dataFrame(data []byte) *repowolfv1.GitFrame {
	return &repowolfv1.GitFrame{Payload: &repowolfv1.GitFrame_Data{Data: &repowolfv1.GitData{Data: data}}}
}

func terminalFrame(category repowolfv1.GitTerminalCategory, exitCode int32) *repowolfv1.GitFrame {
	return &repowolfv1.GitFrame{Payload: &repowolfv1.GitFrame_Terminal{Terminal: &repowolfv1.GitTerminal{Category: category, ExitCode: exitCode}}}
}
