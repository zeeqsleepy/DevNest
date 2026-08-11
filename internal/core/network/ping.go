package network

import (
	"context"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// PingRequest describes one reachability check.
type PingRequest struct {
	Host     string
	Port     int
	Attempts int
	Interval time.Duration
	// OnProbe is called after every probe with its result, so a caller can
	// report progress live while a run is still in flight. It may be nil. It
	// is called from the probe loop, so it must not block for long.
	OnProbe func(Probe)
}

// Probe is one connection attempt.
type Probe struct {
	Number     int    `json:"number"`
	ResponseMs int64  `json:"responseMs"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
}

// PingResult summarises a run.
//
// Method is always "tcp" and is reported rather than assumed, because the
// difference matters: a host that drops ICMP but answers on 443 is reachable
// by this measure and unreachable by the other, and a user reading a report
// needs to know which question was asked.
type PingResult struct {
	Host        string     `json:"host"`
	Port        int        `json:"port"`
	Method      string     `json:"method"`
	Addresses   []string   `json:"addresses"`
	Sent        int        `json:"sent"`
	Received    int        `json:"received"`
	LossPercent float64    `json:"lossPercent"`
	Reachable   bool       `json:"reachable"`
	Statistics  Statistics `json:"statistics"`
	Probes      []Probe    `json:"probes"`
}

// Ping checks whether a host is reachable by opening a TCP connection to it.
//
// This is not ICMP. Sending an ICMP echo needs a raw socket and therefore
// elevated privileges on every supported platform, and DevNest never asks for
// elevation. The alternative, shelling out to the system ping and parsing its
// output, depends on the machine's language settings, which is not a
// foundation to build a cross-platform tool on.
//
// A TCP probe also answers the question people usually mean. "Is this host up"
// almost always means "is the service answering", and plenty of hosts drop
// ICMP while accepting connections perfectly well.
//
// An unreachable host is a result, not an error, for the same reason a site
// being down is a result in Monitor.
func Ping(ctx context.Context, prober Prober, request PingRequest) (PingResult, error) {
	host, err := ParseHost(request.Host)
	if err != nil {
		return PingResult{}, err
	}

	port := request.Port
	if port == 0 {
		port = 443
	}
	if port < 1 || port > 65535 {
		return PingResult{}, errors.New(errors.CodeInvalidInput,
			"invalid port %d", port).
			WithHint("expected a value between 1 and 65535")
	}

	attempts := request.Attempts
	if attempts <= 0 {
		attempts = 4
	}
	if attempts > 1000 {
		return PingResult{}, errors.New(errors.CodeInvalidInput,
			"%d attempts is more than this command will send", attempts)
	}

	result := PingResult{
		Host:      host,
		Port:      port,
		Method:    "tcp",
		Sent:      attempts,
		Addresses: []string{},
		Probes:    make([]Probe, 0, attempts),
	}

	// A resolution failure is reported alongside the probes rather than
	// aborting: probing an address literal needs no DNS at all, and a host
	// that fails to resolve will fail every probe with a message that says so.
	if addresses, err := prober.ResolveHost(ctx, host); err == nil {
		result.Addresses = addresses
	}

	durations := make([]time.Duration, 0, attempts)

	for number := 1; number <= attempts; number++ {
		if err := ctx.Err(); err != nil {
			return PingResult{}, errors.Wrap(err, errors.CodeCancelled, "cancelled")
		}
		if number > 1 && request.Interval > 0 {
			if err := wait(ctx, request.Interval); err != nil {
				return PingResult{}, err
			}
		}

		probe := Probe{Number: number}
		elapsed, err := prober.Probe(ctx, host, port)
		if err != nil {
			report := errors.Classify(err)
			if report.Code == errors.CodeCancelled {
				return PingResult{}, err
			}
			probe.Error = report.Message
		} else {
			probe.OK = true
			probe.ResponseMs = elapsed.Milliseconds()
			durations = append(durations, elapsed)
			result.Received++
		}

		result.Probes = append(result.Probes, probe)

		if request.OnProbe != nil {
			request.OnProbe(probe)
		}
	}

	result.Reachable = result.Received > 0
	result.LossPercent = round2(float64(result.Sent-result.Received) * 100 / float64(result.Sent))
	result.Statistics = summarise(durations)

	return result, nil
}
