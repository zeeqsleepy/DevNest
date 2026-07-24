//go:build windows

package proc

import (
	"syscall"
	"time"
	"unsafe"

	"github.com/devnest/devnest/internal/errors"
)

// Windows has no SIGTERM.
//
// A console program can be sent CTRL_BREAK_EVENT, but only by a process
// attached to the same console, and a GUI program needs a window message. From
// an unrelated process the only universally available mechanism is
// TerminateProcess, which is the equivalent of SIGKILL: the target gets no
// chance to flush or clean up.
//
// So the polite step is reported as unavailable rather than faked. A caller
// that has not asked for force gets an error saying Windows offers no graceful
// option, instead of a kill dressed up as a request. That difference matters
// when the process is a database.
const gracefulSupported = false

// requestExit is never called on Windows, because gracefulSupported is false.
// It exists so that the shared flow compiles against one set of names.
func requestExit(pid int) error {
	return errors.New(errors.CodeInternal,
		"asking process %d to exit is not possible on Windows", pid)
}

// refusedWithoutForce explains why nothing happened.
func refusedWithoutForce(pid int, _ time.Duration) error {
	return errors.New(errors.CodeUnsupported,
		"Windows has no way for one process to ask another to exit politely (pid %d)", pid).
		WithHint("closing the program's own window is the graceful path; " +
			"--force terminates it immediately, losing anything unsaved")
}

// forceExit calls TerminateProcess, having opened the process with exactly the
// one right that needs. Waiting on the handle afterwards would need a second
// right; this package polls instead, so it does not ask for one.
func forceExit(pid int) error {
	handle, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return openError(err, pid)
	}
	defer func() { _ = syscall.CloseHandle(handle) }()

	if err := syscall.TerminateProcess(handle, 1); err != nil {
		return errors.Wrap(err, errors.CodeInternal, "cannot terminate process %d", pid)
	}
	return nil
}

func openError(err error, pid int) error {
	if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return errors.Wrap(err, errors.CodePermissionDenied,
			"not permitted to terminate process %d", pid).
			WithHint("it belongs to another user or runs at a higher integrity level; " +
				"DevNest does not ask for elevation, so run this from an elevated " +
				"prompt if you are sure")
	}
	return errors.Wrap(err, errors.CodeInternal, "cannot open process %d", pid)
}

// stillActive is the exit code Windows reports for a process that has not
// exited. A process that genuinely exits with 259 is indistinguishable from a
// running one through this API; that is a documented quirk of Windows and not
// something this code can fix.
const stillActive = 259

// queryLimitedInformation is PROCESS_QUERY_LIMITED_INFORMATION, the narrowest
// right that allows asking whether a process has exited. The standard library
// does not name it, so it is spelled out here rather than reached for through
// the wider PROCESS_QUERY_INFORMATION, which a caller does not need.
const queryLimitedInformation = 0x1000

// processAlive opens the process for the smallest query right there is and
// asks for its exit code.
func processAlive(pid int) bool {
	handle, err := syscall.OpenProcess(queryLimitedInformation, false, uint32(pid))
	if err != nil {
		// Access denied means the process exists and this user may not look at
		// it, which is still alive for the purpose of the question.
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}
	defer func() { _ = syscall.CloseHandle(handle) }()

	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}

// processName walks the process snapshot the toolhelp API provides.
//
// The snapshot is a copy of the process table and needs no rights over the
// target, so a process owned by another user still gets a name. Only the
// executable name is read; the command line is not, because it can carry a
// credential passed as an argument into output that gets exported.
func processName(pid int) string {
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ""
	}
	defer func() { _ = syscall.CloseHandle(snapshot) }()

	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	if err := syscall.Process32First(snapshot, &entry); err != nil {
		return ""
	}
	for {
		if int(entry.ProcessID) == pid {
			return syscall.UTF16ToString(entry.ExeFile[:])
		}
		if err := syscall.Process32Next(snapshot, &entry); err != nil {
			return ""
		}
	}
}
