package port

import (
	"context"

	"github.com/devnest/devnest/internal/platform/net"
	"github.com/devnest/devnest/internal/platform/proc"
)

// Enumerator lists the machine's listening sockets. One method, read-only: a
// listing can never terminate anything, and the signature says so.
type Enumerator interface {
	Sockets(ctx context.Context, options net.SocketOptions) ([]net.Socket, error)
}

// Inspector names a process and reports whether it is still running.
//
// It is separate from Terminator on purpose. List and Check take an Inspector
// and are therefore incapable of signalling anything, which is a property of
// their signatures rather than of their implementations.
type Inspector interface {
	Describe(pid int) proc.Process
	Alive(pid int) bool
}

// Terminator is Inspector plus the ability to end a process. Only Free takes
// one.
type Terminator interface {
	Inspector
	Terminate(ctx context.Context, pid int, options proc.TerminateOptions) (proc.TerminateResult, error)
}
