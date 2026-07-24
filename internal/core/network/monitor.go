package network

import (
	"context"
	"fmt"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
)

// MonitorRequest describes one availability check.
type MonitorRequest struct {
	// URL is the site to check.
	URL string
	// Method defaults to GET. HEAD is lighter but enough servers mishandle it
	// that it is not the default.
	Method string
	// Headers are sent with the request.
	Headers []net.Header
	// ExpectStatus, when set, is the status code that counts as healthy.
	// Without it any 2xx or 3xx is healthy.
	ExpectStatus int
	// MaxResponseMs, when set, marks the site unhealthy if it answers more
	// slowly than this even though it answered.
	MaxResponseMs int64
}

// Monitor statuses.
const (
	StatusUp   = "up"
	StatusSlow = "slow"
	StatusDown = "down"
)

// MonitorResult is one check.
type MonitorResult struct {
	URL        string    `json:"url"`
	FinalURL   string    `json:"finalUrl,omitempty"`
	Status     string    `json:"status"`
	Healthy    bool      `json:"healthy"`
	StatusCode int       `json:"statusCode"`
	StatusText string    `json:"statusText,omitempty"`
	ResponseMs int64     `json:"responseMs"`
	CheckedAt  time.Time `json:"checkedAt"`
	Redirects  int       `json:"redirects"`
	Reason     string    `json:"reason,omitempty"`
}

// Monitor checks whether a site is answering, and how quickly.
//
// A site being down is a result, not an error. The command succeeded: it asked
// the question and got an answer. That distinction is what lets the exit code
// mean "the site is down" rather than "DevNest broke", which is the difference
// between a usable cron entry and a confusing one.
//
// Only a failure to ask the question at all (an unusable URL, a cancelled
// run) comes back as an error.
func Monitor(ctx context.Context, requester Requester, request MonitorRequest) (MonitorResult, error) {
	target, err := ParseTarget(request.URL)
	if err != nil {
		return MonitorResult{}, err
	}

	method, err := ParseMethod(request.Method)
	if err != nil {
		return MonitorResult{}, err
	}
	if request.ExpectStatus != 0 && (request.ExpectStatus < 100 || request.ExpectStatus > 599) {
		return MonitorResult{}, errors.New(errors.CodeInvalidInput,
			"invalid status code %d", request.ExpectStatus).
			WithHint("expected a value between 100 and 599")
	}

	result := MonitorResult{
		URL:       target.URL,
		CheckedAt: time.Now().UTC(),
	}

	response, err := requester.Request(ctx, net.Request{
		Method:  method,
		URL:     target.URL,
		Headers: request.Headers,
	})
	if err != nil {
		report := errors.Classify(err)
		if report.Code == errors.CodeCancelled {
			return MonitorResult{}, err
		}
		result.Status = StatusDown
		result.Reason = report.Message
		return result, nil
	}

	result.FinalURL = response.FinalURL
	result.StatusCode = response.StatusCode
	result.StatusText = response.Status
	result.ResponseMs = response.Timing.TotalMs
	result.Redirects = len(response.Redirects)

	applyHealth(&result, request)
	return result, nil
}

// applyHealth decides up, slow, or down from the response.
func applyHealth(result *MonitorResult, request MonitorRequest) {
	switch {
	case request.ExpectStatus != 0 && result.StatusCode != request.ExpectStatus:
		result.Status = StatusDown
		result.Reason = fmt.Sprintf("expected status %d, got %d",
			request.ExpectStatus, result.StatusCode)
		return

	case request.ExpectStatus == 0 && (result.StatusCode < 200 || result.StatusCode >= 400):
		result.Status = StatusDown
		result.Reason = "the server answered with " + result.StatusText
		return
	}

	if request.MaxResponseMs > 0 && result.ResponseMs > request.MaxResponseMs {
		result.Status = StatusSlow
		result.Reason = fmt.Sprintf("answered in %dms, slower than the %dms limit",
			result.ResponseMs, request.MaxResponseMs)
		return
	}

	result.Status = StatusUp
	result.Healthy = true
}
