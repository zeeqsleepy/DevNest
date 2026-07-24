package port

import (
	"context"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
	"github.com/devnest/devnest/internal/platform/proc"
)

// fakeSystem stands in for the three kernels this module is written against.
//
// It records what it was asked to terminate, which is how the ordering rules
// are tested: that nothing is signalled when the target is ambiguous, and that
// the port is re-checked before anything is.
type fakeSystem struct {
	sockets []net.Socket
	names   map[int]string
	gone    map[int]bool

	terminated []int
	options    proc.TerminateOptions
	failWith   error
	// vanish removes the socket from the listing after the first enumeration,
	// which simulates the process letting go of the port between the listing
	// and the signal.
	vanish bool
	calls  int
}

func newFake(sockets ...net.Socket) *fakeSystem {
	return &fakeSystem{
		sockets: sockets,
		names:   map[int]string{},
		gone:    map[int]bool{},
	}
}

func (f *fakeSystem) named(pid int, name string) *fakeSystem {
	f.names[pid] = name
	return f
}

func (f *fakeSystem) Sockets(_ context.Context, options net.SocketOptions) ([]net.Socket, error) {
	f.calls++

	// The platform rule the fake has to reproduce: neither protocol selected
	// means both are wanted.
	wantsTCP := options.TCP || !options.UDP
	wantsUDP := options.UDP || !options.TCP

	matching := make([]net.Socket, 0, len(f.sockets))
	for _, socket := range f.sockets {
		if f.vanish && f.calls > 1 {
			continue
		}
		if socket.Protocol == net.ProtocolTCP && !wantsTCP {
			continue
		}
		if socket.Protocol == net.ProtocolUDP && !wantsUDP {
			continue
		}
		if f.gone[socket.PID] {
			continue
		}
		matching = append(matching, socket)
	}
	return matching, nil
}

func (f *fakeSystem) Describe(pid int) proc.Process {
	return proc.Process{PID: pid, Name: f.names[pid]}
}

func (f *fakeSystem) Alive(pid int) bool { return !f.gone[pid] }

func (f *fakeSystem) Terminate(_ context.Context, pid int, options proc.TerminateOptions) (proc.TerminateResult, error) {
	f.terminated = append(f.terminated, pid)
	f.options = options

	if f.failWith != nil {
		return proc.TerminateResult{}, f.failWith
	}

	f.gone[pid] = true
	return proc.TerminateResult{Graceful: !options.Force, WaitedMs: 12}, nil
}

func listening(protocol, address string, number, pid int) net.Socket {
	return net.Socket{Protocol: protocol, Address: address, Port: number, PID: pid}
}

func assertCode(t *testing.T, err error, want errors.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with code %q, got nil", want)
	}
	if got := errors.CodeOf(err); got != want {
		t.Fatalf("code = %q, want %q (%v)", got, want, err)
	}
}

func TestListNamesEachOwnerOnce(t *testing.T) {
	system := newFake(
		listening(net.ProtocolTCP, "0.0.0.0", 8080, 42),
		listening(net.ProtocolTCP, "::", 8080, 42),
		listening(net.ProtocolUDP, "127.0.0.1", 5353, 43),
	).named(42, "node").named(43, "mdns")

	result, err := List(context.Background(), system, system, ListRequest{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if result.Count != 3 {
		t.Fatalf("count = %d, want 3", result.Count)
	}
	if result.Listeners[0].Process != "node" || result.Listeners[2].Process != "mdns" {
		t.Errorf("processes = %+v, want each socket named", result.Listeners)
	}
	if result.UnknownOwners != 0 {
		t.Errorf("unknownOwners = %d, want 0", result.UnknownOwners)
	}
}

func TestListDescribesReachability(t *testing.T) {
	system := newFake(
		listening(net.ProtocolTCP, "0.0.0.0", 8080, 1),
		listening(net.ProtocolTCP, "127.0.0.1", 8081, 1),
		listening(net.ProtocolTCP, "192.168.1.5", 8082, 1),
	).named(1, "server")

	result, err := List(context.Background(), system, system, ListRequest{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	want := []string{ScopeAllInterfaces, ScopeLoopback, ScopeAddress}
	for index, scope := range want {
		if result.Listeners[index].Scope != scope {
			t.Errorf("listener %d scope = %q, want %q", index, result.Listeners[index].Scope, scope)
		}
	}
}

// A socket whose owner the system would not name is still listed, and the
// result counts how many such gaps there are. A listing that silently dropped
// them would answer "what is listening" with something untrue.
func TestListKeepsSocketsItCannotAttribute(t *testing.T) {
	system := newFake(
		listening(net.ProtocolTCP, "0.0.0.0", 9000, 0),
		listening(net.ProtocolTCP, "0.0.0.0", 9001, 77),
	)

	result, err := List(context.Background(), system, system, ListRequest{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if result.Count != 2 {
		t.Fatalf("count = %d, want both sockets", result.Count)
	}
	if result.UnknownOwners != 2 {
		t.Errorf("unknownOwners = %d, want 2: one has no pid and one has no name",
			result.UnknownOwners)
	}
	if result.Listeners[0].Known {
		t.Error("a socket with no owning pid is marked as known")
	}
}

func TestListHidesSystemPortsButCountsThem(t *testing.T) {
	system := newFake(
		listening(net.ProtocolTCP, "0.0.0.0", 80, 1),
		listening(net.ProtocolTCP, "0.0.0.0", 443, 1),
		listening(net.ProtocolTCP, "0.0.0.0", 8080, 2),
	).named(1, "system").named(2, "dev")

	hidden, err := List(context.Background(), system, system, ListRequest{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if hidden.Count != 1 || hidden.SystemHidden != 2 {
		t.Errorf("result = %+v, want one listener and two hidden", hidden)
	}

	all, err := List(context.Background(), system, system, ListRequest{IncludeSystem: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if all.Count != 3 || all.SystemHidden != 0 {
		t.Errorf("result = %+v, want everything listed", all)
	}
}

func TestCheckAnswersAboutOnePortIncludingASystemOne(t *testing.T) {
	system := newFake(listening(net.ProtocolTCP, "0.0.0.0", 80, 5)).named(5, "nginx")

	inUse, err := Check(context.Background(), system, system, CheckRequest{Port: 80})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !inUse.InUse || len(inUse.Listeners) != 1 {
		t.Errorf("result = %+v, want port 80 reported as in use", inUse)
	}

	free, err := Check(context.Background(), system, system, CheckRequest{Port: 8080})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if free.InUse || len(free.Listeners) != 0 {
		t.Errorf("result = %+v, want port 8080 reported as free", free)
	}
}

func TestPortsOutsideTheRangeAreRejected(t *testing.T) {
	system := newFake()

	for _, number := range []int{0, -1, 65536, 999999} {
		_, err := Check(context.Background(), system, system, CheckRequest{Port: number})
		assertCode(t, err, errors.CodeInvalidInput)
	}
}

func TestFreeEndsTheProcessHoldingThePort(t *testing.T) {
	system := newFake(listening(net.ProtocolTCP, "0.0.0.0", 3000, 91)).named(91, "node")

	result, err := Free(context.Background(), system, system, FreeRequest{
		Port:  3000,
		Grace: time.Second,
	})
	if err != nil {
		t.Fatalf("Free: %v", err)
	}

	if len(system.terminated) != 1 || system.terminated[0] != 91 {
		t.Fatalf("terminated = %v, want exactly pid 91", system.terminated)
	}
	if result.Target.Process != "node" {
		t.Errorf("target = %+v, want the process named before it was signalled", result.Target)
	}
	if !result.Graceful || !result.Freed {
		t.Errorf("result = %+v, want a graceful termination that freed the port", result)
	}
}

func TestFreePassesForceThroughRatherThanDecidingItself(t *testing.T) {
	system := newFake(listening(net.ProtocolTCP, "0.0.0.0", 3000, 91)).named(91, "node")

	if _, err := Free(context.Background(), system, system, FreeRequest{Port: 3000, Force: true}); err != nil {
		t.Fatalf("Free: %v", err)
	}
	if !system.options.Force {
		t.Error("Force was not passed down to the platform layer")
	}
}

// Two processes on one port is a forked server or two address families, and
// choosing between them is guessing with somebody's process. Nothing is
// signalled.
func TestFreeRefusesWhenThePortIsHeldByMoreThanOneProcess(t *testing.T) {
	system := newFake(
		listening(net.ProtocolTCP, "0.0.0.0", 3000, 91),
		listening(net.ProtocolTCP, "::", 3000, 92),
	).named(91, "node").named(92, "node")

	_, err := Free(context.Background(), system, system, FreeRequest{Port: 3000})
	assertCode(t, err, errors.CodeConflict)

	if len(system.terminated) != 0 {
		t.Fatalf("terminated = %v, want nothing signalled", system.terminated)
	}
}

func TestFreeReportsAPortNobodyIsHolding(t *testing.T) {
	system := newFake()

	_, err := Free(context.Background(), system, system, FreeRequest{Port: 3000})
	assertCode(t, err, errors.CodeNotFound)

	if len(system.terminated) != 0 {
		t.Fatalf("terminated = %v, want nothing signalled", system.terminated)
	}
}

func TestFreeRefusesWhenTheOwnerIsUnknown(t *testing.T) {
	system := newFake(listening(net.ProtocolTCP, "0.0.0.0", 3000, 0))

	_, err := Free(context.Background(), system, system, FreeRequest{Port: 3000})
	assertCode(t, err, errors.CodePermissionDenied)

	if len(system.terminated) != 0 {
		t.Fatalf("terminated = %v, want nothing signalled", system.terminated)
	}
}

// The pid is re-verified against the port immediately before signalling. Pids
// are reused, and this window is exactly where the wrong process gets killed.
func TestFreeChecksThePortAgainBeforeSignalling(t *testing.T) {
	system := newFake(listening(net.ProtocolTCP, "0.0.0.0", 3000, 91)).named(91, "node")
	system.vanish = true

	_, err := Free(context.Background(), system, system, FreeRequest{Port: 3000})
	assertCode(t, err, errors.CodeConflict)

	if len(system.terminated) != 0 {
		t.Fatalf("terminated = %v, want nothing signalled once the holder changed",
			system.terminated)
	}
}

func TestFreeReportsATerminationThatFailed(t *testing.T) {
	system := newFake(listening(net.ProtocolTCP, "0.0.0.0", 3000, 91)).named(91, "node")
	system.failWith = errors.New(errors.CodePermissionDenied, "not permitted")

	result, err := Free(context.Background(), system, system, FreeRequest{Port: 3000})
	assertCode(t, err, errors.CodePermissionDenied)

	if result.Target.PID != 91 {
		t.Errorf("target = %+v, want the process named even though the attempt failed",
			result.Target)
	}
}
