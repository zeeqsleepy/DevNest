package proc

import (
	"context"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// gracePoll is how often a terminated process is checked for having gone.
const gracePoll = 50 * time.Millisecond

// DefaultGrace is how long a process is given to exit on its own before
// forceful termination is considered.
//
// Two seconds is enough for a development server to close its listeners and
// flush, and short enough that nobody wonders whether the command hung.
const DefaultGrace = 2 * time.Second

// Process is what this layer can say about a running process.
//
// Name is empty when the operating system would not say, which happens for
// another user's process without elevation. Reporting it as unknown is the
// honest answer; omitting the process entirely would hide a real listener.
type Process struct {
	PID  int    `json:"pid"`
	Name string `json:"name,omitempty"`
}

// Describe names a running process.
//
// A process that has exited, or one this user may not inspect, comes back with
// an empty name and no error. The caller is asking "what is this", and "the
// system would not say" is an answer to that question rather than a failure of
// the command asking it.
func (s System) Describe(pid int) Process {
	if pid <= 0 {
		return Process{}
	}
	return Process{PID: pid, Name: processName(pid)}
}

// Alive reports whether a process exists.
func (s System) Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return processAlive(pid)
}

// TerminateOptions configures how hard a termination tries.
type TerminateOptions struct {
	// Force permits killing a process that has not exited within Grace. A
	// termination without it stops at the polite request.
	Force bool
	// Grace is how long the process is given to exit on its own. Zero means
	// DefaultGrace.
	Grace time.Duration
}

// TerminateResult reports what it took.
type TerminateResult struct {
	// Graceful is true when the process exited after the polite signal, false
	// when it had to be killed.
	Graceful bool `json:"graceful"`
	// WaitedMs is how long the process took to go.
	WaitedMs int64 `json:"waitedMs"`
}

// Terminate asks a process to exit, and kills it only if allowed to.
//
// The sequence is fixed and is the whole safety design of this function: ask
// politely, wait, and escalate only when the caller passed Force. A caller
// that wants a process gone immediately still goes through the request first,
// because a process given a chance to flush its state is the difference
// between a restarted server and a corrupted database file.
//
// PID 0 and PID 1 are refused here as well as in the module above. This is the
// last place the refusal can be enforced and the cheapest place to be sure of
// it: on Unix, signalling PID 0 addresses the entire process group, and PID 1
// is init.
//
// Permission is left to the operating system. DevNest does not check process
// ownership itself and does not attempt to acquire elevation: the kernel is
// the authority on who may signal what, and a second opinion computed here
// would either duplicate that check or contradict it.
func (s System) Terminate(ctx context.Context, pid int, options TerminateOptions) (TerminateResult, error) {
	if pid <= 1 {
		return TerminateResult{}, errors.New(errors.CodeInvalidInput,
			"refusing to signal process %d", pid).
			WithHint("process 0 addresses a whole process group and process 1 is the " +
				"init process; neither is ever the thing you meant")
	}
	if !processAlive(pid) {
		return TerminateResult{}, errors.New(errors.CodeNotFound,
			"process %d is not running", pid)
	}

	grace := options.Grace
	if grace <= 0 {
		grace = DefaultGrace
	}

	started := time.Now()

	// Windows has no mechanism for one process to ask another to exit, so the
	// polite step is skipped there rather than faked. gracefulSupported is a
	// platform constant, which is what keeps that difference out of the flow
	// below and out of every caller.
	if gracefulSupported {
		if err := requestExit(pid); err != nil {
			return TerminateResult{}, err
		}

		gone, err := waitForExit(ctx, pid, grace)
		if err != nil {
			return TerminateResult{}, err
		}
		if gone {
			return TerminateResult{
				Graceful: true,
				WaitedMs: time.Since(started).Milliseconds(),
			}, nil
		}
	}

	if !options.Force {
		return TerminateResult{}, refusedWithoutForce(pid, grace)
	}

	if err := forceExit(pid); err != nil {
		return TerminateResult{}, err
	}
	if _, err := waitForExit(ctx, pid, grace); err != nil {
		return TerminateResult{}, err
	}

	return TerminateResult{Graceful: false, WaitedMs: time.Since(started).Milliseconds()}, nil
}

// waitForExit polls until the process is gone or the deadline passes.
//
// Polling rather than waiting on a handle, because the process is not a child
// of this one and none of the three platforms offers a portable way to wait on
// a stranger.
func waitForExit(ctx context.Context, pid int, within time.Duration) (bool, error) {
	deadline := time.Now().Add(within)

	for {
		if !processAlive(pid) {
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, nil
		}

		select {
		case <-ctx.Done():
			return false, errors.Wrap(ctx.Err(), errors.CodeCancelled,
				"stopped waiting for process %d", pid)
		case <-time.After(gracePoll):
		}
	}
}
