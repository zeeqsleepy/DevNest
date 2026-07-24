// Package port is DevNest's port module: what is listening on this machine,
// whether a particular port is taken, and ending the process holding one.
//
// # Two of three commands cannot break anything
//
// Listing and checking take an Inspector and are read-only by signature.
// Freeing a port takes a Terminator, and it is the only function here that can
// end a process. Nothing about that is enforced by convention: a caller that
// hands List an Inspector cannot make it signal anything, because the type it
// was given has no method that does.
//
// # What the system will not say is reported, not hidden
//
// A socket owned by another user often comes back without an owning process,
// and on Windows and macOS naming that process may need rights this user does
// not have. Every such gap is reported as unknown. A listing that quietly
// omits the sockets it could not attribute answers the question "what is
// listening" with something that is not true, and the person reading it has no
// way to tell.
//
// # Ports below 1024
//
// Enumeration leaves them out unless asked. They are the system's own
// services, they are the same on every machine, and they are not what somebody
// running this command is looking for. The count of what was left out is part
// of the result, so the omission is visible rather than silent.
package port

import (
	"context"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
)

// Port bounds. Zero is not a port a socket can be listening on; 65535 is the
// largest a sixteen-bit field holds.
const (
	MinPort = 1
	MaxPort = 65535
)

// systemPortCeiling is the top of the range enumeration hides by default.
const systemPortCeiling = 1024

// Scope names how reachable a listener is, which is the fact people are
// usually looking for and the one an address alone makes them work out.
const (
	ScopeAllInterfaces = "all-interfaces"
	ScopeLoopback      = "loopback"
	ScopeAddress       = "address"
)

// Listener is one listening socket with whatever is known about its owner.
type Listener struct {
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	IPv6     bool   `json:"ipv6"`
	Scope    string `json:"scope"`

	// PID is zero and Process is empty when the system would not say who owns
	// the socket. Known is the field to branch on rather than comparing PID to
	// zero, because "unknown" is a real answer here and deserves a name.
	PID     int    `json:"pid,omitempty"`
	Process string `json:"process,omitempty"`
	Known   bool   `json:"ownerKnown"`
}

// ListRequest selects what to list.
type ListRequest struct {
	// TCP and UDP select protocols. Neither set means both.
	TCP bool
	UDP bool
	// IncludeSystem includes ports below 1024.
	IncludeSystem bool
	// Port, when non-zero, narrows the listing to one port. Check uses this;
	// it is not exposed as a flag on the listing command.
	Port int
}

// ListResult is everything listening, and what was left out.
type ListResult struct {
	Listeners []Listener `json:"listeners"`
	Count     int        `json:"count"`
	// SystemHidden counts the listeners below port 1024 that were omitted, so
	// a short list is never mistaken for a quiet machine.
	SystemHidden int `json:"systemHidden"`
	// UnknownOwners counts the listeners whose process could not be
	// identified, which is the honest measure of how complete this listing is.
	UnknownOwners int `json:"unknownOwners"`
}

// List reports the listening sockets on this machine.
func List(ctx context.Context, enumerator Enumerator, inspector Inspector, request ListRequest) (ListResult, error) {
	if request.Port != 0 {
		if err := ValidatePort(request.Port); err != nil {
			return ListResult{}, err
		}
	}

	sockets, err := enumerator.Sockets(ctx, net.SocketOptions{TCP: request.TCP, UDP: request.UDP})
	if err != nil {
		return ListResult{}, err
	}

	result := ListResult{Listeners: make([]Listener, 0, len(sockets))}
	names := make(map[int]string, 16)

	for _, socket := range sockets {
		if err := ctx.Err(); err != nil {
			return ListResult{}, errors.Wrap(err, errors.CodeCancelled, "cancelled")
		}
		if request.Port != 0 && socket.Port != request.Port {
			continue
		}
		if socket.Port < systemPortCeiling && !request.IncludeSystem && request.Port == 0 {
			result.SystemHidden++
			continue
		}

		listener := describe(socket, inspector, names)
		if !listener.Known {
			result.UnknownOwners++
		}
		result.Listeners = append(result.Listeners, listener)
	}

	result.Count = len(result.Listeners)
	return result, nil
}

// describe turns a socket into a listener, naming its process at most once per
// pid. A machine running twenty containers has twenty sockets on one process,
// and asking the operating system twenty times for the same name is work
// nobody needs done.
func describe(socket net.Socket, inspector Inspector, names map[int]string) Listener {
	listener := Listener{
		Protocol: socket.Protocol,
		Address:  socket.Address,
		Port:     socket.Port,
		IPv6:     socket.IPv6,
		Scope:    scopeOf(socket),
		PID:      socket.PID,
	}

	if socket.PID <= 0 {
		return listener
	}

	name, cached := names[socket.PID]
	if !cached {
		name = inspector.Describe(socket.PID).Name
		names[socket.PID] = name
	}

	listener.Process = name
	listener.Known = name != ""
	return listener
}

func scopeOf(socket net.Socket) string {
	switch {
	case socket.AllInterfaces():
		return ScopeAllInterfaces
	case socket.Loopback():
		return ScopeLoopback
	default:
		return ScopeAddress
	}
}

// ValidatePort rejects a number that is not a port.
//
// It is exported because the CLI validates the argument before anything else
// happens, and two implementations of "is this a port" would eventually
// disagree.
func ValidatePort(number int) error {
	if number < MinPort || number > MaxPort {
		return errors.New(errors.CodeInvalidInput,
			"%d is not a port number", number).
			WithHint("ports run from %d to %d", MinPort, MaxPort)
	}
	return nil
}
