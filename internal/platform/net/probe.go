package net

import (
	"context"
	stdnet "net"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// Probe opens a TCP connection to a host and reports how long it took.
//
// This is not ICMP, and the distinction is deliberate rather than a shortcut.
// Sending an ICMP echo needs a raw socket, which needs elevated privileges on
// every supported platform, and DevNest never asks for elevation: that is a
// stated principle, not an oversight. A tool that works only when run as
// administrator is a tool most people cannot use.
//
// A TCP probe also answers the question people are usually asking. "Is this
// host up" almost always means "is the service I want answering", and a great
// many hosts drop ICMP while happily accepting connections on 443. Every
// command and every field that reports a probe says it is TCP, so nobody has
// to guess what was measured.
func (s System) Probe(ctx context.Context, host string, port int) (time.Duration, error) {
	if port <= 0 {
		port = DefaultPort
	}
	address := Address(host, port)

	ctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	dialer := &stdnet.Dialer{}

	started := time.Now()
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return 0, classifyDialError(err, address)
	}
	elapsed := time.Since(started)

	// Nothing was written, so a close failure cannot lose anything.
	_ = connection.Close()

	return elapsed, nil
}

// ResolveHost returns the addresses a host resolves to, so a probe can report
// which one it reached.
func (s System) ResolveHost(ctx context.Context, host string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	addresses, err := (&stdnet.Resolver{}).LookupIPAddr(ctx, host)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeNotFound, "cannot resolve %s", host).
			WithHint("check the host name, and that this machine has working DNS")
	}

	values := make([]string, 0, len(addresses))
	for _, address := range addresses {
		values = append(values, address.IP.String())
	}
	return values, nil
}
