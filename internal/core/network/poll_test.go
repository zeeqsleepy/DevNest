package network

import (
	"context"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
)

// A monitoring loop reports each check as it happens, so a person watching a
// site sees its state change rather than waiting for the run to end.
func TestPollReportsEachCheckLive(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{
		okResponse(200, 10), okResponse(200, 12),
	}}

	var seen []MonitorResult
	result, err := Poll(context.Background(), requester, PollRequest{
		Monitor:  MonitorRequest{URL: "example.com"},
		Interval: time.Millisecond,
		Count:    2,
		OnCheck:  func(checked MonitorResult) { seen = append(seen, checked) },
	})
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if len(seen) != 2 {
		t.Fatalf("OnCheck called %d times, want 2", len(seen))
	}
	if result.Checks != 2 || result.Up != 2 {
		t.Errorf("result = %+v, want two up checks", result)
	}
	if !result.Healthy || result.Latest.StatusCode != 200 {
		t.Errorf("latest = %+v, want a healthy 200", result.Latest)
	}
}

// Zero counts as "until stopped", and stopping is the context being cancelled.
// The checks that did run are still reported to the caller.
func TestPollStopsOnCancellationAndKeepsTheChecks(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{okResponse(200, 10)}}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	result, err := Poll(ctx, requester, PollRequest{
		Monitor:  MonitorRequest{URL: "example.com"},
		Interval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if result.Checks == 0 {
		t.Error("an interrupted run reported no checks at all")
	}
	if result.Checks != len(result.History) {
		t.Errorf("Checks = %d but History has %d entries", result.Checks, len(result.History))
	}
}

// A site that is down is a check result, not a reason to stop the loop; a
// monitoring run that stopped on the first outage could not watch a site
// recover.
func TestPollContinuesPastADownCheck(t *testing.T) {
	requester := &fakeRequester{
		responses: []net.Response{okResponse(200, 10), okResponse(200, 12)},
		errs:      []error{nil, errors.New(errors.CodeNetwork, "connection refused")},
	}

	result, err := Poll(context.Background(), requester, PollRequest{
		Monitor: MonitorRequest{URL: "example.com"},
		Count:   2,
	})
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if result.Down != 1 || result.Up != 1 {
		t.Errorf("up = %d, down = %d, want one of each", result.Up, result.Down)
	}
	// The final check was the outage, so the outcome is unhealthy.
	if result.Healthy {
		t.Error("Healthy = true although the final check was down")
	}
}

func TestPollRejectsANegativeInterval(t *testing.T) {
	_, err := Poll(context.Background(), &fakeRequester{}, PollRequest{
		Monitor:  MonitorRequest{URL: "example.com"},
		Interval: -time.Second,
	})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestPollRejectsABadURLBeforeFirstCheck(t *testing.T) {
	requester := &fakeRequester{}
	_, err := Poll(context.Background(), requester, PollRequest{
		Monitor: MonitorRequest{URL: "ftp://example.com"},
	})
	assertCode(t, err, errors.CodeInvalidInput)

	if len(requester.calls) != 0 {
		t.Errorf("sent %d requests for an unusable target", len(requester.calls))
	}
}
