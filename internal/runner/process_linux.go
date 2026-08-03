package runner

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const processGroupReapTimeout = 2 * time.Second

var (
	// processTableMu closes the PID-reuse window between leader reap and group
	// retirement. Starts are brief; retirement remains serialized until every
	// process in the old group is gone.
	processTableMu sync.Mutex
	errGroupAlive  = errors.New("provider process group remains after termination")
)

// processAuthority is the sole owner of negative-PGID signaling. The winning
// reason is published under the same lock before SIGKILL can release an exit
// observer. Authority remains live until the leader and adopted group children
// have been reaped.
type processAuthority struct {
	pgid      int
	kill      func(int, syscall.Signal) error
	reap      func() error
	reapGroup func(int) error

	mu         sync.Mutex
	terminated bool
	retired    bool
	reason     error
}

func newProcessAuthority(command *exec.Cmd) *processAuthority {
	return &processAuthority{
		pgid:      command.Process.Pid,
		kill:      syscall.Kill,
		reap:      command.Wait,
		reapGroup: reapOwnedProcessGroup,
	}
}

// terminate reports whether this call initiated termination.
func (authority *processAuthority) terminate(reason error) bool {
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.retired || authority.terminated {
		// Output can be drained after the leader has already exited. Preserve
		// that independently observed bound violation without replacing a
		// context or input reason that won while the process was live.
		if authority.reason == nil && errors.Is(reason, ErrOutputLimit) {
			authority.reason = ErrOutputLimit
		}
		return false
	}
	authority.terminated = true
	authority.reason = reason
	_ = authority.kill(-authority.pgid, syscall.SIGKILL)
	return true
}

func (authority *processAuthority) terminationReason() error {
	authority.mu.Lock()
	defer authority.mu.Unlock()
	return authority.reason
}

func (authority *processAuthority) retireAndReap() (leaderErr, groupErr error) {
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if !authority.terminated {
		authority.terminated = true
		_ = authority.kill(-authority.pgid, syscall.SIGKILL)
	}

	processTableMu.Lock()
	defer processTableMu.Unlock()
	leaderErr = authority.reap()
	groupErr = authority.reapGroup(authority.pgid)
	authority.retired = true
	return leaderErr, groupErr
}

func reapOwnedProcessGroup(pgid int) error {
	deadline := time.Now().Add(processGroupReapTimeout)
	for {
		for {
			pid, err := syscall.Wait4(-pgid, nil, syscall.WNOHANG, nil)
			if pid > 0 {
				continue
			}
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			if err != nil && !errors.Is(err, syscall.ECHILD) {
				return err
			}
			break
		}

		err := syscall.Kill(-pgid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil && !errors.Is(err, syscall.EPERM) {
			return err
		}
		if time.Now().After(deadline) {
			return errGroupAlive
		}
		time.Sleep(time.Millisecond)
	}
}

func waitWithoutReap(pid int) error {
	const processID = 1 // Linux P_PID
	var info [128]byte
	for {
		_, _, errno := syscall.Syscall6(
			syscall.SYS_WAITID,
			processID,
			uintptr(pid),
			uintptr(unsafe.Pointer(&info[0])),
			uintptr(syscall.WEXITED|syscall.WNOWAIT),
			0,
			0,
		)
		if errno == 0 {
			return nil
		}
		if errno != syscall.EINTR {
			return errno
		}
	}
}
