package network

import (
	"context"
	"time"

	"github.com/devnest/devnest/internal/platform/net"
)

// The module declares one interface per kind of network operation rather than
// one interface for the network, so each function's signature says exactly
// what it reaches for. Faking a DNS lookup in a test means implementing one
// method, not six.

// Requester performs HTTP exchanges. Used by monitor, http, and latency.
type Requester interface {
	Request(ctx context.Context, request net.Request) (net.Response, error)
}

// Resolver looks up DNS records. Used by dns.
type Resolver interface {
	Resolve(ctx context.Context, domain string, kinds []net.Kind) ([]net.Answer, error)
}

// Prober opens a TCP connection and times it, and resolves a host to the
// addresses it answers on. Used by ping.
type Prober interface {
	Probe(ctx context.Context, host string, port int) (time.Duration, error)
	ResolveHost(ctx context.Context, host string) ([]string, error)
}

// Inspector performs a TLS handshake and reports the served chain. Used by
// ssl.
type Inspector interface {
	Certificates(ctx context.Context, host string, port int) (net.Chain, error)
}
