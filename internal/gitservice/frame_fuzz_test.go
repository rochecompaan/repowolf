package gitservice

import (
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/protobuf/proto"
)

func FuzzClientFrameSequence(f *testing.F) {
	f.Add(encodeFrameSequence(
		openFrame("git.example", "owner", "repo", 22),
		dataFrame([]byte("request")),
	))
	f.Add(encodeFrameSequence(dataFrame([]byte("before-open"))))
	f.Add(encodeFrameSequence(
		openFrame("git.example", "owner", "repo", 22),
		openFrame("git.example", "owner", "repo", 22),
	))
	f.Add(encodeFrameSequence(
		openFrame("git.example", "owner", "repo", 22),
		terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED, 0),
	))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<20 {
			raw = raw[:1<<20]
		}
		var state clientFrameState
		valid := true
		for count := 0; len(raw) > 0 && count < 64; count++ {
			size := int(raw[0])
			raw = raw[1:]
			if size > len(raw) {
				size = len(raw)
			}
			var frame repowolfv1.GitFrame
			if err := proto.Unmarshal(raw[:size], &frame); err != nil || state.Accept(&frame) != nil {
				valid = false
				break
			}
			raw = raw[size:]
		}
		if valid {
			_ = state.Close()
		}
	})
}

func FuzzServerFrameSequence(f *testing.F) {
	f.Add(encodeFrameSequence(
		dataFrame([]byte("response")),
		terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED, 0),
	))
	f.Add(encodeFrameSequence(openFrame("git.example", "owner", "repo", 22)))
	f.Add(encodeFrameSequence(terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED, 0)))
	f.Add(encodeFrameSequence(terminalFrame(repowolfv1.GitTerminalCategory(99), 1)))
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

func encodeFrameSequence(frames ...*repowolfv1.GitFrame) []byte {
	var encoded []byte
	for _, frame := range frames {
		raw, err := proto.Marshal(frame)
		if err != nil || len(raw) > 255 {
			panic("invalid fuzz seed")
		}
		encoded = append(encoded, byte(len(raw)))
		encoded = append(encoded, raw...)
	}
	return encoded
}
