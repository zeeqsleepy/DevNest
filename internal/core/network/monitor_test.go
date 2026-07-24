package network

import (
	"context"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
)

func TestMonitorReportsAHealthySite(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{okResponse(200, 42)}}

	result, err := Monitor(context.Background(), requester, MonitorRequest{URL: "example.com"})
	if err != nil {
		t.Fatalf("Monitor: %v", err)
	}

	if result.Status != StatusUp || !result.Healthy {
		t.Errorf("status = %q, healthy = %v", result.Status, result.Healthy)
	}
	if result.StatusCode != 200 {
		t.Errorf("StatusCode = %d", result.StatusCode)
	}
	if result.ResponseMs != 42 {
		t.Errorf("ResponseMs = %d, want 42", result.ResponseMs)
	}
	if result.CheckedAt.IsZero() {
		t.Error("CheckedAt was not set")
	}
}

// A site being down is a result, not an error. Only that distinction lets the
// exit code mean "the site is down" rather than "DevNest broke".
func TestMonitorTreatsAnUnreachableSiteAsAResult(t *testing.T) {
	requester := &fakeRequester{
		errs: []error{errors.New(errors.CodeNetwork, "cannot reach example.com")},
	}

	result, err := Monitor(context.Background(), requester, MonitorRequest{URL: "example.com"})
	if err != nil {
		t.Fatalf("Monitor returned an error for an unreachable site: %v", err)
	}

	if result.Status != StatusDown || result.Healthy {
		t.Errorf("status = %q, healthy = %v", result.Status, result.Healthy)
	}
	if !strings.Contains(result.Reason, "cannot reach") {
		t.Errorf("reason = %q, want it to explain what happened", result.Reason)
	}
}

// Cancellation is not a site being down; it is the run being stopped.
func TestMonitorPropagatesCancellation(t *testing.T) {
	requester := &fakeRequester{
		errs: []error{errors.New(errors.CodeCancelled, "cancelled")},
	}

	_, err := Monitor(context.Background(), requester, MonitorRequest{URL: "example.com"})
	assertCode(t, err, errors.CodeCancelled)
}

func TestMonitorStatusRanges(t *testing.T) {
	tests := []struct {
		status  int
		healthy bool
	}{
		{200, true},
		{204, true},
		{301, true},
		{399, true},
		{404, false},
		{500, false},
		{100, false},
	}

	for _, test := range tests {
		t.Run(statusText(test.status), func(t *testing.T) {
			requester := &fakeRequester{responses: []net.Response{okResponse(test.status, 10)}}

			result, err := Monitor(context.Background(), requester, MonitorRequest{URL: "example.com"})
			if err != nil {
				t.Fatalf("Monitor: %v", err)
			}
			if result.Healthy != test.healthy {
				t.Errorf("status %d: healthy = %v, want %v",
					test.status, result.Healthy, test.healthy)
			}
		})
	}
}

func TestMonitorExpectStatus(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{okResponse(301, 10)}}

	result, err := Monitor(context.Background(), requester, MonitorRequest{
		URL: "example.com", ExpectStatus: 200,
	})
	if err != nil {
		t.Fatalf("Monitor: %v", err)
	}

	if result.Healthy {
		t.Error("a 301 satisfied --expect-status 200")
	}
	if !strings.Contains(result.Reason, "200") {
		t.Errorf("reason = %q, want it to name the expected status", result.Reason)
	}
}

func TestMonitorRejectsAnImpossibleExpectedStatus(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{okResponse(200, 10)}}

	_, err := Monitor(context.Background(), requester, MonitorRequest{
		URL: "example.com", ExpectStatus: 42,
	})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestMonitorMaxResponse(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{okResponse(200, 800)}}

	result, err := Monitor(context.Background(), requester, MonitorRequest{
		URL: "example.com", MaxResponseMs: 500,
	})
	if err != nil {
		t.Fatalf("Monitor: %v", err)
	}

	if result.Status != StatusSlow {
		t.Errorf("status = %q, want %q", result.Status, StatusSlow)
	}
	if result.Healthy {
		t.Error("a site slower than the stated limit should not count as healthy")
	}
	if !strings.Contains(result.Reason, "500") {
		t.Errorf("reason = %q, want it to name the limit", result.Reason)
	}
}

func TestMonitorRejectsABadURL(t *testing.T) {
	requester := &fakeRequester{}

	_, err := Monitor(context.Background(), requester, MonitorRequest{URL: "file:///etc/passwd"})
	assertCode(t, err, errors.CodeInvalidInput)

	if len(requester.calls) != 0 {
		t.Error("a request was sent for a url that should have been rejected")
	}
}

func TestMonitorUsesTheRequestedMethod(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{okResponse(200, 10)}}

	_, err := Monitor(context.Background(), requester, MonitorRequest{
		URL: "example.com", Method: "head",
	})
	if err != nil {
		t.Fatalf("Monitor: %v", err)
	}
	if requester.calls[0].Method != "HEAD" {
		t.Errorf("method = %q, want HEAD", requester.calls[0].Method)
	}
}
