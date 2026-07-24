package net

import (
	"context"
	"net"
	"sort"
	"strconv"
)

// Socket protocols reported by an enumeration.
const (
	ProtocolTCP = "tcp"
	ProtocolUDP = "udp"
)

// Socket is one listening endpoint on this machine.
//
// PID is zero when the operating system did not tell us which process owns the
// socket, which happens for another user's process without elevation. That is
// reported as unknown rather than dropped: a list that silently omits the
// sockets it could not attribute is worse than one that admits to a gap.
type Socket struct {
	Protocol string `json:"protocol"`
	// Address is the local address the socket is bound to, as the operating
	// system reports it. An all-interfaces bind is "0.0.0.0" or "::".
	Address string `json:"address"`
	Port    int    `json:"port"`
	// IPv6 distinguishes a socket bound on the IPv6 stack, because a port can
	// legitimately be held twice, once per family.
	IPv6 bool `json:"ipv6"`
	PID  int  `json:"pid"`
}

// AllInterfaces reports whether a socket is bound to every interface rather
// than to the loopback alone. The distinction is the difference between a
// development server only this machine can reach and one the network can.
func (s Socket) AllInterfaces() bool {
	address := net.ParseIP(s.Address)
	return address != nil && address.IsUnspecified()
}

// Loopback reports whether a socket is reachable only from this machine.
func (s Socket) Loopback() bool {
	address := net.ParseIP(s.Address)
	return address != nil && address.IsLoopback()
}

// String renders the endpoint the way every other network tool prints one.
func (s Socket) String() string {
	return net.JoinHostPort(s.Address, strconv.Itoa(s.Port))
}

// SocketOptions selects what an enumeration returns.
type SocketOptions struct {
	// TCP and UDP select the protocols. Both false means both, because a
	// caller that says nothing wants everything.
	TCP bool
	UDP bool
}

// wantsTCP and wantsUDP resolve the "neither means both" rule in one place.
func (o SocketOptions) wantsTCP() bool { return o.TCP || !o.UDP }
func (o SocketOptions) wantsUDP() bool { return o.UDP || !o.TCP }

// Sockets lists the listening sockets on this machine.
//
// This is the least uniform corner of the platform layer, and it is where the
// three operating systems have nothing in common. Windows asks the IP Helper
// API, Linux reads /proc and resolves socket inodes to processes, and macOS
// runs lsof because the alternative is cgo and libproc. Each lives in its own
// build-tagged file; nothing above this package contains an OS conditional.
//
// What they agree on is the contract: listening sockets only, sorted, with an
// owning PID where the system was willing to say. Failure to attribute a
// socket is not failure to list it.
func (s System) Sockets(ctx context.Context, options SocketOptions) ([]Socket, error) {
	sockets, err := listSockets(ctx, options)
	if err != nil {
		return nil, err
	}

	sort.Slice(sockets, func(first, second int) bool {
		left, right := sockets[first], sockets[second]
		switch {
		case left.Port != right.Port:
			return left.Port < right.Port
		case left.Protocol != right.Protocol:
			return left.Protocol < right.Protocol
		case left.Address != right.Address:
			return left.Address < right.Address
		default:
			return left.PID < right.PID
		}
	})

	return sockets, nil
}

// swapPort converts a port from network byte order.
//
// Two of the three implementations read the port straight out of a kernel
// structure, where it is big-endian regardless of the machine's own order.
func swapPort(value uint16) int {
	return int(value>>8 | value<<8)
}
