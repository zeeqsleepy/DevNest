package network

import (
	"context"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
)

// LatencyRequest describes one latency measurement.
type LatencyRequest struct {
	URL      string
	Method   string
	Headers  []net.Header
	Attempts int
	// Interval waits between attempts. A short pause keeps a run from looking
	// like a burst of traffic to whoever is on the other end.
	Interval time.Duration
	// OnSample is called after every attempt with its result, so a caller can
	// report progress live while a run is still measuring. It may be nil. It
	// is called from the measuring loop, so it must not block for long.
	OnSample func(Attempt)
}

// Attempt is one measurement.
type Attempt struct {
	Number     int    `json:"number"`
	ResponseMs int64  `json:"responseMs"`
	StatusCode int    `json:"statusCode,omitempty"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
}

// LatencyResult summarises a run.
type LatencyResult struct {
	URL        string     `json:"url"`
	Method     string     `json:"method"`
	Attempts   int        `json:"attempts"`
	Successful int        `json:"successful"`
	Failed     int        `json:"failed"`
	Statistics Statistics `json:"statistics"`
	Samples    []Attempt  `json:"samples"`
}

// Latency measures how long a URL takes to answer, several times over.
//
// Attempts run in sequence with a pause between them, not in parallel. Firing
// them at once would measure how well the server handles concurrency, which is
// a different question, and it would make DevNest look like a load generator
// to anyone watching their logs. This is a measurement tool, not a load
// testing one.
//
// Connections are not reused between attempts. A reused connection reports
// almost no setup cost for every attempt after the first, which flatters the
// numbers and hides the thing most likely to be slow.
//
// A failed attempt is recorded and the run continues: an intermittent failure
// is exactly what a latency check is for, and stopping at the first one would
// hide it.
func Latency(ctx context.Context, requester Requester, request LatencyRequest) (LatencyResult, error) {
	target, err := ParseTarget(request.URL)
	if err != nil {
		return LatencyResult{}, err
	}

	method, err := ParseMethod(request.Method)
	if err != nil {
		return LatencyResult{}, err
	}

	attempts := request.Attempts
	if attempts <= 0 {
		attempts = 3
	}
	if attempts > 1000 {
		return LatencyResult{}, errors.New(errors.CodeInvalidInput,
			"%d attempts is more than this command will send", attempts).
			WithHint("DevNest measures latency; it is not a load testing tool")
	}

	result := LatencyResult{
		URL:      target.URL,
		Method:   method,
		Attempts: attempts,
		Samples:  make([]Attempt, 0, attempts),
	}
	durations := make([]time.Duration, 0, attempts)

	for number := 1; number <= attempts; number++ {
		if err := ctx.Err(); err != nil {
			return LatencyResult{}, errors.Wrap(err, errors.CodeCancelled, "cancelled")
		}
		if number > 1 && request.Interval > 0 {
			if err := wait(ctx, request.Interval); err != nil {
				return LatencyResult{}, err
			}
		}

		sample := measure(ctx, requester, method, target.URL, request.Headers, number)
		result.Samples = append(result.Samples, sample)

		if sample.OK {
			result.Successful++
			durations = append(durations, time.Duration(sample.ResponseMs)*time.Millisecond)
		} else {
			result.Failed++
		}

		if request.OnSample != nil {
			request.OnSample(sample)
		}
	}

	result.Statistics = summarise(durations)
	return result, nil
}

func measure(
	ctx context.Context,
	requester Requester,
	method, url string,
	headers []net.Header,
	number int,
) Attempt {
	attempt := Attempt{Number: number}

	response, err := requester.Request(ctx, net.Request{
		Method:  method,
		URL:     url,
		Headers: headers,
	})
	if err != nil {
		attempt.Error = errors.Classify(err).Message
		return attempt
	}

	attempt.ResponseMs = response.Timing.TotalMs
	attempt.StatusCode = response.StatusCode
	attempt.OK = true
	return attempt
}

// wait pauses, but stops immediately if the run is cancelled. A sleep that
// ignores cancellation is the reason interrupting a command sometimes takes
// several seconds to do anything.
func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), errors.CodeCancelled, "cancelled")
	case <-timer.C:
		return nil
	}
}
