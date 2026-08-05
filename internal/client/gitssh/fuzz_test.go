package gitssh

import (
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/protobuf/proto"
)

const (
	maximumFuzzArgvBytes = 64 << 10
	maximumFuzzArguments = 64
)

func FuzzServerFrameSequence(f *testing.F) {
	completed := terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED, 0)
	f.Add(encodeServerFrameSequence(dataFrame([]byte("response")), completed))
	f.Add(encodeServerFrameSequence(openFrame(testRequest(UploadPack))))
	f.Add(encodeServerFrameSequence(completed, completed))
	f.Add(encodeServerFrameSequence(completed, dataFrame([]byte("late"))))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<20 {
			raw = raw[:1<<20]
		}
		var state serverFrameState
		for count := 0; len(raw) > 0 && count < 64; count++ {
			size := int(raw[0])
			raw = raw[1:]
			if size > len(raw) {
				size = len(raw)
			}
			var frame repowolfv1.GitFrame
			if err := proto.Unmarshal(raw[:size], &frame); err != nil {
				return
			}
			if _, err := state.Accept(&frame); err != nil {
				return
			}
			raw = raw[size:]
		}
	})
}

func encodeServerFrameSequence(frames ...*repowolfv1.GitFrame) []byte {
	var encoded []byte
	for _, frame := range frames {
		raw, err := proto.Marshal(frame)
		if err != nil || len(raw) > 255 {
			panic("invalid server frame fuzz seed")
		}
		encoded = append(encoded, byte(len(raw)))
		encoded = append(encoded, raw...)
	}
	return encoded
}

func FuzzParseSSHArgs(f *testing.F) {
	for _, args := range [][]string{
		{"git@github.example", "git-upload-pack 'owner/repo.git'"},
		{"-o", "SendEnv=GIT_PROTOCOL", "git@github.example", "git-receive-pack 'owner/repo.git'"},
		{"-p", "2222", "git@github.example", "git-upload-pack 'owner/repo.git'"},
		{"-o", "SendEnv=GIT_PROTOCOL", "-p", "65535", "git@github.example", "git-receive-pack 'owner/repo.git'"},
		{"-p", "0", "git@github.example", "git-upload-pack 'owner/repo.git'"},
		{"-p", "65536", "git@github.example", "git-upload-pack 'owner/repo.git'"},
		{"-o", "ProxyCommand=sh", "git@github.example", "git-upload-pack 'owner/repo.git'"},
		{"root@github.example", "git-upload-pack 'owner/repo.git'"},
		{"git@github.example", "sh"},
	} {
		f.Add(strings.Join(args, "\x00"))
	}
	f.Fuzz(func(t *testing.T, encoded string) {
		if len(encoded) > maximumFuzzArgvBytes {
			encoded = encoded[:maximumFuzzArgvBytes]
		}
		args := strings.Split(encoded, "\x00")
		if len(args) > maximumFuzzArguments {
			args = args[:maximumFuzzArguments]
		}
		_, _ = Parse(args)
	})
}
