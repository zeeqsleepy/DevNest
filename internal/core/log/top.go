package log

import (
	"context"
	"time"
)

// TopRequest describes one most-requested-endpoints listing.
type TopRequest struct {
	Path string
	// Limit caps how many endpoints are reported. Zero means the default.
	Limit int
	// Clients ranks client addresses instead of endpoints. Same pass, same
	// counters, different projection.
	Clients bool
}

// TopResult lists the most requested endpoints, or the busiest clients.
type TopResult struct {
	Path     string `json:"path"`
	Subject  string `json:"subject"`
	Lines    int    `json:"lines"`
	Requests int    `json:"requests"`
	Unparsed int    `json:"unparsedLines"`
	Unique   int    `json:"unique"`

	Entries []Count `json:"entries"`

	RankingTruncated bool  `json:"rankingTruncated"`
	DurationMs       int64 `json:"durationMs"`
}

// TopRequests lists the endpoints a log saw most often.
//
// The query string is stripped before counting, so /search?q=cats and
// /search?q=dogs are one endpoint. Reporting them separately turns the listing
// into a sample of individual requests, which the raw file already is.
func TopRequests(ctx context.Context, reader Reader, request TopRequest) (TopResult, error) {
	started := time.Now()

	from, err := open(reader, request.Path)
	if err != nil {
		return TopResult{}, err
	}
	defer from.close()

	totals, err := collectAccess(ctx, from)
	if err != nil {
		return TopResult{}, err
	}

	limit := request.Limit
	if limit < 1 {
		limit = defaultTop
	}

	ranked, subject := totals.paths, "endpoint"
	if request.Clients {
		ranked, subject = totals.clients, "client"
	}

	return TopResult{
		Path:             from.path,
		Subject:          subject,
		Lines:            totals.lines,
		Requests:         totals.requests,
		Unparsed:         totals.unparsed,
		Unique:           ranked.unique(),
		Entries:          ranked.top(limit, totals.requests),
		RankingTruncated: ranked.overflow,
		DurationMs:       millis(started),
	}, nil
}
