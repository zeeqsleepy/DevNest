package log

import (
	"context"
	"time"
)

// StatusRequest describes one status code summary.
type StatusRequest struct {
	Path string
	// Top caps the list of individual codes. Zero means the default.
	Top int
}

// StatusResult breaks an access log down by response status.
type StatusResult struct {
	Path     string `json:"path"`
	Lines    int    `json:"lines"`
	Requests int    `json:"requests"`
	Unparsed int    `json:"unparsedLines"`

	// Classes always holds all five families, including the ones with no
	// requests. A summary that silently omits 5xx leaves the reader unable to
	// tell "none" from "not measured".
	Classes []Count `json:"classes"`
	Codes   []Count `json:"codes"`

	// Errors is the 4xx and 5xx count together, which is the number people
	// actually watch.
	Errors     int   `json:"errorResponses"`
	DurationMs int64 `json:"durationMs"`
}

// SummarizeStatus reports the distribution of response codes.
//
// It reads the same collection as SummarizeHTTP, so the two commands can never
// disagree about how many requests a file holds.
func SummarizeStatus(ctx context.Context, reader Reader, request StatusRequest) (StatusResult, error) {
	started := time.Now()

	from, err := open(reader, request.Path)
	if err != nil {
		return StatusResult{}, err
	}
	defer from.close()

	totals, err := collectAccess(ctx, from)
	if err != nil {
		return StatusResult{}, err
	}

	top := request.Top
	if top < 1 {
		top = defaultTop
	}

	classes := totals.classes.ordered(statusClasses(), totals.requests)

	return StatusResult{
		Path:       from.path,
		Lines:      totals.lines,
		Requests:   totals.requests,
		Unparsed:   totals.unparsed,
		Classes:    classes,
		Codes:      totals.codes.top(top, totals.requests),
		Errors:     classes[3].Count + classes[4].Count,
		DurationMs: millis(started),
	}, nil
}
