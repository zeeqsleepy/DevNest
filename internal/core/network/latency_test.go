package network

import (
	"context"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
)

func TestLatencyTakesTheRequestedNumberOfAttempts(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{
		okResponse(200, 10), okResponse(200, 20), okResponse(200, 30),
	}}

	result, err := Latency(context.Background(), requester, LatencyRequest{
		URL: "example.com", Attempts: 3,
	})
	if err != nil {
		t.Fatalf("Latency: %v", err)
	}

	if len(requester.calls) != 3 {
		t.Errorf("sent %d requests, want 3", len(requester.calls))
	}
	if result.Successful != 3 || result.Failed != 0 {
		t.Errorf("successful = %d, failed = %d", result.Successful, result.Failed)
	}
	if result.Statistics.MinMs != 10 || result.Statistics.MaxMs != 30 {
		t.Errorf("statistics = %+v", result.Statistics)
	}
	if result.Statistics.AverageMs != 20 {
		t.Errorf("AverageMs = %d, want 20", result.Statistics.AverageMs)
	}
}

func TestLatencyDefaultsToThreeAttempts(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{okResponse(200, 10)}}

	result, err := Latency(context.Background(), requester, LatencyRequest{URL: "example.com"})
	if err != nil {
		t.Fatalf("Latency: %v", err)
	}
	if result.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", result.Attempts)
	}
}

// An intermittent failure is exactly what a latency check is for, so one bad
// attempt must not stop the run.
func TestLatencyContinuesPastAFailedAttempt(t *testing.T) {
	requester := &fakeRequester{
		responses: []net.Response{okResponse(200, 10), {}, okResponse(200, 30)},
		errs: []error{
			nil,
			errors.New(errors.CodeNetwork, "connection reset"),
			nil,
		},
	}

	result, err := Latency(context.Background(), requester, LatencyRequest{
		URL: "example.com", Attempts: 3,
	})
	if err != nil {
		t.Fatalf("Latency: %v", err)
	}

	if result.Successful != 2 || result.Failed != 1 {
		t.Errorf("successful = %d, failed = %d, want 2 and 1", result.Successful, result.Failed)
	}
	if len(result.Samples) != 3 {
		t.Fatalf("samples = %d, want one per attempt", len(result.Samples))
	}
	if result.Samples[1].Error == "" {
		t.Error("the failed attempt carries no reason")
	}
	// Statistics describe the attempts that worked; a failure has no duration
	// to average in.
	if result.Statistics.MinMs != 10 || result.Statistics.MaxMs != 30 {
		t.Errorf("statistics = %+v", result.Statistics)
	}
}

func TestLatencyReportsEveryAttemptFailing(t *testing.T) {
	requester := &fakeRequester{errs: []error{
		errors.New(errors.CodeNetwork, "refused"),
		errors.New(errors.CodeNetwork, "refused"),
	}}

	result, err := Latency(context.Background(), requester, LatencyRequest{
		URL: "example.com", Attempts: 2,
	})
	if err != nil {
		t.Fatalf("Latency: %v", err)
	}
	if result.Successful != 0 || result.Failed != 2 {
		t.Errorf("successful = %d, failed = %d", result.Successful, result.Failed)
	}
	if result.Statistics != (Statistics{}) {
		t.Errorf("statistics = %+v, want zeroes when nothing succeeded", result.Statistics)
	}
}

// This is a measurement tool, not a load generator.
func TestLatencyRefusesAnAbsurdNumberOfAttempts(t *testing.T) {
	_, err := Latency(context.Background(), &fakeRequester{}, LatencyRequest{
		URL: "example.com", Attempts: 100000,
	})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestLatencyStopsOnCancellation(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{okResponse(200, 10)}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Latency(ctx, requester, LatencyRequest{URL: "example.com", Attempts: 5})
	assertCode(t, err, errors.CodeCancelled)

	if len(requester.calls) != 0 {
		t.Errorf("sent %d requests after cancellation", len(requester.calls))
	}
}

// A pause that ignores cancellation is why interrupting a command sometimes
// takes several seconds to do anything.
func TestLatencyIntervalIsInterruptible(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{okResponse(200, 10)}}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	started := time.Now()
	_, err := Latency(ctx, requester, LatencyRequest{
		URL: "example.com", Attempts: 5, Interval: 10 * time.Second,
	})
	elapsed := time.Since(started)

	assertCode(t, err, errors.CodeCancelled)
	if elapsed > 2*time.Second {
		t.Errorf("took %v to notice cancellation during an interval", elapsed)
	}
}

func TestLatencyRejectsABadURL(t *testing.T) {
	_, err := Latency(context.Background(), &fakeRequester{}, LatencyRequest{URL: "ftp://example.com"})
	assertCode(t, err, errors.CodeInvalidInput)
}
