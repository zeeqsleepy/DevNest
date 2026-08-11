package network

import (
	"context"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

func TestPingReportsAReachableHost(t *testing.T) {
	prober := &fakeProber{
		durations: []time.Duration{
			10 * time.Millisecond,
			20 * time.Millisecond,
			30 * time.Millisecond,
		},
		addresses: []string{"93.184.216.34"},
	}

	result, err := Ping(context.Background(), prober, PingRequest{
		Host: "example.com", Attempts: 3,
	})
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if !result.Reachable {
		t.Error("Reachable = false for a host that answered")
	}
	if result.Sent != 3 || result.Received != 3 {
		t.Errorf("sent = %d, received = %d", result.Sent, result.Received)
	}
	if result.LossPercent != 0 {
		t.Errorf("LossPercent = %v, want 0", result.LossPercent)
	}
	if result.Statistics.MinMs != 10 || result.Statistics.MaxMs != 30 {
		t.Errorf("statistics = %+v", result.Statistics)
	}
	if len(result.Addresses) != 1 {
		t.Errorf("Addresses = %v", result.Addresses)
	}
}

// The method is reported rather than assumed, because a host that drops ICMP
// but answers on 443 is reachable by this measure and not by the other.
func TestPingReportsThatItUsedTCP(t *testing.T) {
	result, err := Ping(context.Background(), &fakeProber{}, PingRequest{
		Host: "example.com", Attempts: 1,
	})
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if result.Method != "tcp" {
		t.Errorf("Method = %q, want %q", result.Method, "tcp")
	}
}

// An unreachable host is a result, not an error.
func TestPingTreatsAnUnreachableHostAsAResult(t *testing.T) {
	prober := &fakeProber{errs: []error{
		errors.New(errors.CodeNetwork, "connection refused"),
		errors.New(errors.CodeNetwork, "connection refused"),
	}}

	result, err := Ping(context.Background(), prober, PingRequest{
		Host: "example.com", Attempts: 2,
	})
	if err != nil {
		t.Fatalf("Ping returned an error for an unreachable host: %v", err)
	}

	if result.Reachable {
		t.Error("Reachable = true when nothing answered")
	}
	if result.LossPercent != 100 {
		t.Errorf("LossPercent = %v, want 100", result.LossPercent)
	}
	if result.Probes[0].Error == "" {
		t.Error("the failed probe carries no reason")
	}
}

func TestPingReportsPartialLoss(t *testing.T) {
	prober := &fakeProber{
		durations: []time.Duration{10 * time.Millisecond, 0, 30 * time.Millisecond, 0},
		errs: []error{
			nil,
			errors.New(errors.CodeNetwork, "timed out"),
			nil,
			errors.New(errors.CodeNetwork, "timed out"),
		},
	}

	result, err := Ping(context.Background(), prober, PingRequest{
		Host: "example.com", Attempts: 4,
	})
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if result.Received != 2 {
		t.Errorf("Received = %d, want 2", result.Received)
	}
	if result.LossPercent != 50 {
		t.Errorf("LossPercent = %v, want 50", result.LossPercent)
	}
	if !result.Reachable {
		t.Error("a host that answered twice out of four is reachable")
	}
}

// Probing an address literal needs no DNS at all, so a resolution failure must
// not abort the run.
func TestPingContinuesWhenResolutionFails(t *testing.T) {
	prober := &fakeProber{
		durations:  []time.Duration{10 * time.Millisecond},
		resolveErr: errors.New(errors.CodeNotFound, "cannot resolve"),
	}

	result, err := Ping(context.Background(), prober, PingRequest{
		Host: "10.0.0.1", Attempts: 1,
	})
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if !result.Reachable {
		t.Error("the probe should still have run")
	}
	if len(result.Addresses) != 0 {
		t.Errorf("Addresses = %v, want an empty list", result.Addresses)
	}
}

func TestPingDefaultsToPort443(t *testing.T) {
	result, err := Ping(context.Background(), &fakeProber{}, PingRequest{
		Host: "example.com", Attempts: 1,
	})
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if result.Port != 443 {
		t.Errorf("Port = %d, want 443", result.Port)
	}
}

func TestPingRejectsABadPort(t *testing.T) {
	for _, port := range []int{-1, 70000} {
		_, err := Ping(context.Background(), &fakeProber{}, PingRequest{
			Host: "example.com", Port: port,
		})
		assertCode(t, err, errors.CodeInvalidInput)
	}
}

func TestPingRejectsABadHost(t *testing.T) {
	_, err := Ping(context.Background(), &fakeProber{}, PingRequest{Host: "  "})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestPingStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Ping(ctx, &fakeProber{}, PingRequest{Host: "example.com", Attempts: 4})
	assertCode(t, err, errors.CodeCancelled)
}

// OnProbe reports each probe as it lands, so a slow host can be watched
// rather than waited on silently.
func TestPingReportsEachProbeLive(t *testing.T) {
	prober := &fakeProber{
		durations: []time.Duration{24 * time.Millisecond, 5 * time.Millisecond},
	}

	var seen []Probe
	_, err := Ping(context.Background(), prober, PingRequest{
		Host:     "example.com",
		Attempts: 2,
		OnProbe:  func(probe Probe) { seen = append(seen, probe) },
	})
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if len(seen) != 2 {
		t.Fatalf("OnProbe called %d times, want 2", len(seen))
	}
	if !seen[0].OK || seen[0].ResponseMs != 24 {
		t.Errorf("first probe = %+v, want the scripted 24ms success", seen[0])
	}
	if !seen[1].OK || seen[1].Number != 2 {
		t.Errorf("second probe = %+v, want the second success", seen[1])
	}
}
