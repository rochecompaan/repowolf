package runner

import (
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

// processAuthority is the sole owner of negative-PGID signaling. It retires
// that authority before reaping makes the numeric process group reusable.
type processAuthority struct {
	pgid int
	kill func(int, syscall.Signal) error
	reap func() error

	mu         sync.Mutex
	terminated bool
	retired    bool
}

func newProcessAuthority(command *exec.Cmd) *processAuthority {
	return &processAuthority{
		pgid: command.Process.Pid,
		kill: syscall.Kill,
		reap: command.Wait,
	}
}

// terminate reports whether this call initiated termination.
func (authority *processAuthority) terminate() bool {
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.retired || authority.terminated {
		return false
	}
	authority.terminated = true
	_ = authority.kill(-authority.pgid, syscall.SIGKILL)
	return true
}

func (authority *processAuthority) retireAndReap() error {
	authority.mu.Lock()
	if !authority.terminated {
		authority.terminated = true
		_ = authority.kill(-authority.pgid, syscall.SIGKILL)
	}
	authority.retired = true
	authority.mu.Unlock()
	return authority.reap()
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
