package gitssh

import (
	"errors"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

var errInvalidServerFrame = errors.New("invalid Git server frame")

type serverFrameState struct {
	terminal bool
}

func (state *serverFrameState) Accept(frame *repowolfv1.GitFrame) (bool, error) {
	if state.terminal || frame == nil {
		return false, errInvalidServerFrame
	}
	switch payload := frame.Payload.(type) {
	case *repowolfv1.GitFrame_Data:
		if payload.Data == nil || len(payload.Data.Data) > maximumChunkBytes {
			return false, errInvalidServerFrame
		}
		return false, nil
	case *repowolfv1.GitFrame_Terminal:
		if !validTerminal(payload.Terminal) {
			return false, errInvalidServerFrame
		}
		state.terminal = true
		return true, nil
	default:
		return false, errInvalidServerFrame
	}
}

func validTerminal(terminal *repowolfv1.GitTerminal) bool {
	if terminal == nil {
		return false
	}
	switch terminal.Category {
	case repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED:
		return terminal.ExitCode == 0
	case repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_PERMISSION_DENIED,
		repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_INVALID_REQUEST,
		repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_PROVIDER_FAILURE,
		repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_DEADLINE_EXCEEDED,
		repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_LIMIT_EXCEEDED,
		repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_UNAVAILABLE:
		return terminal.ExitCode != 0
	default:
		return false
	}
}
