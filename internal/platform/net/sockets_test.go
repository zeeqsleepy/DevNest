package net

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"
	"time"
)

// These tests bind a real listener and then ask the operating system to
// describe it. That is the only way to test this package honestly: the whole
// point of it is that three kernels are asked the same question, and a fake
// would test the fake.
func TestSocketsFindsAListenerThisTestOpened(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("open a listener: %v", err)
	}
	defer func() { _ = listener.Close() }()

	_, rawPort, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("read the listening address: %v", err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatalf("read the listening port: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sockets, err := System{}.Sockets(ctx, SocketOptions{TCP: true})
	if err != nil {
		t.Fatalf("Sockets: %v", err)
	}

	found, ok := socketOn(sockets, port)
	if !ok {
		t.Fatalf("port %d is listening but was not reported among %d sockets", port, len(sockets))
	}
	if found.Protocol != ProtocolTCP {
		t.Errorf("protocol = %q, want %q", found.Protocol, ProtocolTCP)
	}
	if !found.Loopback() {
		t.Errorf("address = %q, want the loopback address the listener bound to", found.Address)
	}

	// Attribution is allowed to fail (another user's process, a hardened
	// container), but when the owner is reported for a socket this process
	// opened, it has to be this process.
	if found.PID != 0 && found.PID != os.Getpid() {
		t.Errorf("pid = %d, want %d or 0 for unknown", found.PID, os.Getpid())
	}
}

func TestSocketsAreSortedByPort(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sockets, err := System{}.Sockets(ctx, SocketOptions{})
	if err != nil {
		t.Fatalf("Sockets: %v", err)
	}

	for index := 1; index < len(sockets); index++ {
		if sockets[index-1].Port > sockets[index].Port {
			t.Fatalf("socket %d (port %d) comes after port %d",
				index, sockets[index].Port, sockets[index-1].Port)
		}
	}
}

// A cancelled context must not produce a listing. The check is at the top of
// the platform call rather than left to whatever the platform does, so it
// behaves the same on all three.
func TestSocketsRespectsACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := (System{}).Sockets(ctx, SocketOptions{}); err == nil {
		t.Error("a cancelled context produced a listing")
	}
}

func TestSocketClassification(t *testing.T) {
	cases := []struct {
		address           string
		wantAll, wantBack bool
	}{
		{"0.0.0.0", true, false},
		{"::", true, false},
		{"127.0.0.1", false, true},
		{"::1", false, true},
		{"192.168.1.10", false, false},
		{"not an address", false, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.address, func(t *testing.T) {
			socket := Socket{Address: testCase.address, Port: 80}
			if got := socket.AllInterfaces(); got != testCase.wantAll {
				t.Errorf("AllInterfaces() = %v, want %v", got, testCase.wantAll)
			}
			if got := socket.Loopback(); got != testCase.wantBack {
				t.Errorf("Loopback() = %v, want %v", got, testCase.wantBack)
			}
		})
	}
}

func TestSocketStringIsAnEndpoint(t *testing.T) {
	cases := map[Socket]string{
		{Address: "127.0.0.1", Port: 8080}: "127.0.0.1:8080",
		{Address: "::1", Port: 443}:        "[::1]:443",
	}

	for socket, want := range cases {
		if got := socket.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	}
}

func TestSocketOptionsTreatSilenceAsBoth(t *testing.T) {
	both := SocketOptions{}
	if !both.wantsTCP() || !both.wantsUDP() {
		t.Error("an empty selection should mean every protocol")
	}

	only := SocketOptions{TCP: true}
	if !only.wantsTCP() || only.wantsUDP() {
		t.Error("selecting tcp should exclude udp")
	}
}

func TestSwapPortReadsNetworkByteOrder(t *testing.T) {
	// 0x1F90 is 8080 in network order; read the other way it is 0x901F.
	if got := swapPort(0x901F); got != 8080 {
		t.Errorf("swapPort(0x901F) = %d, want 8080", got)
	}
}

func socketOn(sockets []Socket, port int) (Socket, bool) {
	for _, socket := range sockets {
		if socket.Port == port {
			return socket, true
		}
	}
	return Socket{}, false
}
