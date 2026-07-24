package net

import (
	"context"
	stdnet "net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// listener opens a loopback port and returns its host and port.
func listener(t *testing.T) (string, int, func()) {
	t.Helper()

	socket, err := stdnet.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	host, portText, err := stdnet.SplitHostPort(socket.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}

	// Accept and immediately close, so a probe completes rather than queueing.
	go func() {
		for {
			connection, err := socket.Accept()
			if err != nil {
				return
			}
			_ = connection.Close()
		}
	}()

	return host, port, func() { _ = socket.Close() }
}

func TestProbeReachesAListeningPort(t *testing.T) {
	host, port, closeSocket := listener(t)
	defer closeSocket()

	elapsed, err := system().Probe(context.Background(), host, port)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if elapsed < 0 {
		t.Errorf("elapsed = %v", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Errorf("elapsed = %v, want a loopback connection to be quick", elapsed)
	}
}

func TestProbeFailsOnAClosedPort(t *testing.T) {
	host, port, closeSocket := listener(t)
	closeSocket()

	_, err := system().Probe(context.Background(), host, port)
	if errors.CodeOf(err) != errors.CodeNetwork {
		t.Errorf("code = %q, want %q (%v)", errors.CodeOf(err), errors.CodeNetwork, err)
	}
	if hint := errors.Classify(err).Hint; hint == "" {
		t.Error("a refused connection should suggest what to check")
	}
}

func TestProbeDefaultsToPort443(t *testing.T) {
	// Port zero means "pick the default", not "connect to port zero".
	client := system()
	client.Timeout = 200 * time.Millisecond

	_, err := client.Probe(context.Background(), "127.0.0.1", 0)
	if err == nil {
		t.Skip("something is listening on 127.0.0.1:443 on this machine")
	}
	if !strings.Contains(err.Error(), "443") {
		t.Errorf("error = %q, want it to name the default port", err.Error())
	}
}

func TestProbeRespectsCancellation(t *testing.T) {
	host, port, closeSocket := listener(t)
	defer closeSocket()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := system().Probe(ctx, host, port)
	if errors.CodeOf(err) != errors.CodeCancelled {
		t.Errorf("code = %q, want %q (%v)", errors.CodeOf(err), errors.CodeCancelled, err)
	}
}

func TestResolveHostOnLoopback(t *testing.T) {
	// localhost resolves from the hosts file, so this needs no network.
	addresses, err := system().ResolveHost(context.Background(), "localhost")
	if err != nil {
		t.Skipf("localhost does not resolve on this machine: %v", err)
	}
	if len(addresses) == 0 {
		t.Error("localhost resolved to nothing")
	}

	loopback := false
	for _, address := range addresses {
		if address == "127.0.0.1" || address == "::1" {
			loopback = true
		}
	}
	if !loopback {
		t.Errorf("addresses = %v, want a loopback address", addresses)
	}
}

func TestResolveHostFailsOnAReservedName(t *testing.T) {
	client := system()
	client.Timeout = 2 * time.Second

	// .invalid is reserved by RFC 2606 and never resolves.
	_, err := client.ResolveHost(context.Background(), "devnest-test.invalid")
	if err == nil {
		t.Fatal("a reserved invalid domain resolved")
	}
	if hint := errors.Classify(err).Hint; hint == "" {
		t.Error("a resolution failure should suggest what to check")
	}
}
