//go:build !windows

package proc

import (
	"syscall"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// gracefulSupported: Unix has SIGTERM, so a process can always be asked before
// it is killed.
const gracefulSupported = true

// refusedWithoutForce explains a process that ignored the request and was left
// running because the caller did not ask for a kill.
func refusedWithoutForce(pid int, grace time.Duration) error {
	return errors.New(errors.CodeConflict,
		"process %d did not exit within %s", pid, grace).
		WithHint("pass --force to terminate it, which gives it no chance to save state")
}

// requestExit sends SIGTERM: the signal every well-behaved program handles by
// closing its listeners, flushing, and exiting.
func requestExit(pid int) error {
	return signal(pid, syscall.SIGTERM, "ask process %d to exit")
}

// forceExit sends SIGKILL, which the process cannot catch, block, or clean up
// after. It is the last resort and the caller has to have asked for it.
func forceExit(pid int) error {
	return signal(pid, syscall.SIGKILL, "terminate process %d")
}

func signal(pid int, which syscall.Signal, format string) error {
	err := syscall.Kill(pid, which)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, syscall.EPERM):
		return errors.Wrap(err, errors.CodePermissionDenied,
			"not permitted to "+format, pid).
			WithHint("it belongs to another user; DevNest does not ask for elevation, " +
				"so run this as that user or with sudo if you are sure")
	case errors.Is(err, syscall.ESRCH):
		return errors.Wrap(err, errors.CodeNotFound, "process %d is not running", pid)
	default:
		return errors.Wrap(err, errors.CodeInternal, "cannot "+format, pid)
	}
}

// processAlive asks the kernel whether a pid exists by signalling it with
// signal zero, which performs the permission and existence checks and delivers
// nothing. A process owned by another user still answers "exists".
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
