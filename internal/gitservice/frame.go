package gitservice

import (
	"errors"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

var errInvalidFrame = errors.New("invalid git frame sequence")

type clientFrameState struct {
	opened bool
	failed bool
}

func (state *clientFrameState) Accept(frame *repowolfv1.GitFrame) error {
	if state.failed || frame == nil {
		state.failed = true
		return errInvalidFrame
	}
	switch payload := frame.Payload.(type) {
	case *repowolfv1.GitFrame_Open:
		if state.opened || payload.Open == nil || payload.Open.Repository == nil {
			state.failed = true
			return errInvalidFrame
		}
		state.opened = true
		return nil
	case *repowolfv1.GitFrame_Data:
		if !state.opened || payload.Data == nil {
			state.failed = true
			return errInvalidFrame
		}
		if err := validChunk(payload.Data.Data); err != nil {
			state.failed = true
			return err
		}
		return nil
	default:
		state.failed = true
		return errInvalidFrame
	}
}

func (state *clientFrameState) Close() error {
	if state.failed || !state.opened {
		return errInvalidFrame
	}
	return nil
}

type serverFrameState struct {
	terminal bool
}

func (state *serverFrameState) Accept(frame *repowolfv1.GitFrame) (bool, error) {
	if state.terminal || frame == nil {
		return false, errInvalidFrame
	}
	switch payload := frame.Payload.(type) {
	case *repowolfv1.GitFrame_Data:
		if payload.Data == nil {
			return false, errInvalidFrame
		}
		return false, validChunk(payload.Data.Data)
	case *repowolfv1.GitFrame_Terminal:
		if !validTerminal(payload.Terminal) {
			return false, errInvalidFrame
		}
		state.terminal = true
		return true, nil
	default:
		return false, errInvalidFrame
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
