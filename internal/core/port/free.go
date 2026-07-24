package port

import (
	"context"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/proc"
)

// FreeRequest asks for the process holding a port to be ended.
type FreeRequest struct {
	Port int
	TCP  bool
	UDP  bool
	// Force permits killing a process that does not exit on request.
	Force bool
	// Grace is how long the process is given to exit on its own. Zero means
	// the platform default.
	Grace time.Duration
}

// FreeResult reports what happened.
type FreeResult struct {
	Port int `json:"port"`
	// Target is the process that was holding the port, as it was identified
	// before anything was signalled. It is in the result whether or not the
	// termination succeeded, because "which process was this" is half of what
	// the user wanted to know.
	Target Listener `json:"target"`
	// Graceful is true when the process exited after being asked, false when
	// it had to be killed.
	Graceful bool  `json:"graceful"`
	WaitedMs int64 `json:"waitedMs"`
	// Freed reports whether the port is actually free afterwards. A process
	// can exit while a socket lingers, and claiming success without looking
	// would be the kind of lie that costs somebody an afternoon.
	Freed bool `json:"freed"`
}

// Free ends the process holding a port.
//
// This is the sharpest edge in DevNest and the sequence matters:
//
//  1. The port is enumerated and the holder identified by pid and name.
//  2. A port held by more than one process is refused outright rather than
//     acted on. Two listeners on one port means a forked server or two address
//     families, and picking one of them is guessing with somebody's process.
//  3. The pid is re-verified against the port immediately before signalling.
//     Pids are reused, and the window between "we listed the sockets" and "we
//     signalled something" is exactly where the wrong process gets killed.
//  4. The platform layer refuses pid 0 and pid 1, asks politely first, and
//     escalates only when Force was passed.
//  5. The port is enumerated again afterwards to see whether it is free.
//
// Interactive confirmation is not here. It belongs to the interface layer,
// which owns the terminal; this function performs what it was told to.
func Free(ctx context.Context, enumerator Enumerator, terminator Terminator, request FreeRequest) (FreeResult, error) {
	if err := ValidatePort(request.Port); err != nil {
		return FreeResult{}, err
	}

	holders, err := holdersOf(ctx, enumerator, terminator, request)
	if err != nil {
		return FreeResult{}, err
	}

	target, err := soleTarget(holders, request.Port)
	if err != nil {
		return FreeResult{}, err
	}

	// Re-verification. Between the listing above and the signal below, the
	// process could have exited and its pid been handed to something else.
	// Asking again narrows that window to the smallest this design allows.
	if err := stillHolding(ctx, enumerator, terminator, request, target); err != nil {
		return FreeResult{}, err
	}

	outcome, err := terminator.Terminate(ctx, target.PID, proc.TerminateOptions{
		Force: request.Force,
		Grace: request.Grace,
	})
	if err != nil {
		return FreeResult{Port: request.Port, Target: target}, err
	}

	freed, err := holdersOf(ctx, enumerator, terminator, request)
	if err != nil {
		return FreeResult{}, err
	}

	return FreeResult{
		Port:     request.Port,
		Target:   target,
		Graceful: outcome.Graceful,
		WaitedMs: outcome.WaitedMs,
		Freed:    len(freed) == 0,
	}, nil
}

func holdersOf(ctx context.Context, enumerator Enumerator, inspector Inspector, request FreeRequest) ([]Listener, error) {
	listing, err := List(ctx, enumerator, inspector, ListRequest{
		TCP:           request.TCP,
		UDP:           request.UDP,
		IncludeSystem: true,
		Port:          request.Port,
	})
	if err != nil {
		return nil, err
	}
	return listing.Listeners, nil
}

// soleTarget picks the one process to act on, or explains why there is not one.
func soleTarget(holders []Listener, number int) (Listener, error) {
	owned := make([]Listener, 0, len(holders))
	for _, holder := range holders {
		if holder.PID > 0 {
			owned = append(owned, holder)
		}
	}

	switch {
	case len(holders) == 0:
		return Listener{}, errors.New(errors.CodeNotFound,
			"nothing is listening on port %d", number)

	case len(owned) == 0:
		return Listener{}, errors.New(errors.CodePermissionDenied,
			"port %d is in use, but this machine will not say which process holds it", number).
			WithHint("it belongs to another user; DevNest does not ask for elevation")

	case distinctPIDs(owned) > 1:
		return Listener{}, errors.New(errors.CodeConflict,
			"port %d is held by %d different processes", number, distinctPIDs(owned)).
			WithHint("run \"devnest port check %d\" to see them, then end the right one "+
				"yourself; choosing for you would be guessing with somebody's process", number)

	default:
		return owned[0], nil
	}
}

func distinctPIDs(listeners []Listener) int {
	seen := make(map[int]bool, len(listeners))
	for _, listener := range listeners {
		seen[listener.PID] = true
	}
	return len(seen)
}

// stillHolding re-checks that the target process is the one on the port.
func stillHolding(ctx context.Context, enumerator Enumerator, inspector Inspector, request FreeRequest, target Listener) error {
	holders, err := holdersOf(ctx, enumerator, inspector, request)
	if err != nil {
		return err
	}

	for _, holder := range holders {
		if holder.PID == target.PID {
			return nil
		}
	}

	return errors.New(errors.CodeConflict,
		"process %d no longer holds port %d", target.PID, request.Port).
		WithHint("something changed while this command was running; " +
			"nothing was signalled, so run it again")
}
