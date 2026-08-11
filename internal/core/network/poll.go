package network

import (
	"context"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// PollRequest asks for a site to be checked more than once, at a steady cadence.
type PollRequest struct {
	// Monitor is the check to repeat.
	Monitor MonitorRequest
	// Interval waits between checks. The first check runs immediately.
	Interval time.Duration
	// Count bounds the run. Zero means until the context is cancelled.
	Count int
	// OnCheck is called after every check, including the last, so a caller
	// can report progress live. It may be nil. It is called from the poll
	// loop and must not block for long.
	OnCheck func(MonitorResult)
}

// PollResult summarises a series of checks.
type PollResult struct {
	URL string `json:"url"`
	// Checks is how many checks ran.
	Checks int `json:"checks"`
	Up     int `json:"up"`
	Slow   int `json:"slow"`
	Down   int `json:"down"`
	// Healthy reports whether the final check was healthy, which is what a
	// monitoring job should branch on: a site that blinked and recovered was
	// down, but it is down no longer.
	Healthy bool `json:"healthy"`
	// Latest is the most recent check, so a consumer sees the current state
	// without walking the history.
	Latest  MonitorResult   `json:"latest"`
	History []MonitorResult `json:"history"`
}

// Poll checks a site repeatedly until stopped.
//
// The first check runs immediately and the interval counts from when each
// check finished, so a slow host naturally spaces its own checks out. A check
// that finds the site down is still recorded: the site being down is the
// answer, and a monitoring loop that stopped on the first outage would be the
// wrong tool for its job.
//
// A run that ends because the context was cancelled has still produced the
// checks it produced, and the caller reports them. Only a failure to run the
// loop at all, such as an unusable URL, comes back as an error here.
func Poll(ctx context.Context, requester Requester, request PollRequest) (PollResult, error) {
	if request.Interval < 0 {
		return PollResult{}, errors.New(errors.CodeInvalidInput, "a monitoring interval cannot be negative").
			WithHint("pass a duration like --interval 5s, or omit it for a single check")
	}

	// Resolve and validate the URL up front, so a typo fails before the first
	// check rather than after a minute of monitoring the wrong name.
	if _, err := ParseTarget(request.Monitor.URL); err != nil {
		return PollResult{}, err
	}

	result := PollResult{
		URL:     request.Monitor.URL,
		History: []MonitorResult{},
	}

	for check := 1; ; check++ {
		if err := ctx.Err(); err != nil {
			return result, nil
		}

		checked, err := Monitor(ctx, requester, request.Monitor)
		if err != nil {
			if errors.CodeOf(err) == errors.CodeCancelled {
				return result, nil
			}
			return PollResult{}, err
		}

		result.Checks++
		switch checked.Status {
		case StatusUp:
			result.Up++
		case StatusSlow:
			result.Slow++
		default:
			result.Down++
		}
		result.Latest = checked
		result.Healthy = checked.Healthy
		result.History = append(result.History, checked)

		if request.OnCheck != nil {
			request.OnCheck(checked)
		}

		if request.Count > 0 && check >= request.Count {
			break
		}
		if request.Interval > 0 {
			if err := wait(ctx, request.Interval); err != nil {
				return result, nil
			}
		}
	}

	return result, nil
}
