package log

import (
	"context"
	"time"
)

// HTTPRequest describes one access-log summary.
type HTTPRequest struct {
	Path string
	// Top caps each ranked listing. Zero means the default.
	Top int
}

// HTTPResult is what one pass over an access log found.
type HTTPResult struct {
	Path      string `json:"path"`
	Bytes     int64  `json:"bytes"`
	Lines     int    `json:"lines"`
	Requests  int    `json:"requests"`
	Unparsed  int    `json:"unparsedLines"`
	UniqueIPs int    `json:"uniqueClients"`

	Methods       []Count `json:"methods"`
	StatusClasses []Count `json:"statusClasses"`
	StatusCodes   []Count `json:"statusCodes"`
	TopPaths      []Count `json:"topPaths"`
	TopClients    []Count `json:"topClients"`

	TotalResponseBytes   int64 `json:"totalResponseBytes"`
	AverageResponseBytes int64 `json:"averageResponseBytes"`

	// RankingTruncated says that the file held more distinct paths or clients
	// than the counter tracks, so the ranking is of what was tracked.
	RankingTruncated bool  `json:"rankingTruncated"`
	DurationMs       int64 `json:"durationMs"`
}

// SummarizeHTTP reports on an HTTP access log.
//
// Lines that are not access-log entries are counted as unparsed rather than
// rejected. A log with a startup banner at the top is still an access log, and
// a tool that refuses it is a tool people stop reaching for.
func SummarizeHTTP(ctx context.Context, reader Reader, request HTTPRequest) (HTTPResult, error) {
	started := time.Now()

	from, err := open(reader, request.Path)
	if err != nil {
		return HTTPResult{}, err
	}
	defer from.close()

	totals, err := collectAccess(ctx, from)
	if err != nil {
		return HTTPResult{}, err
	}

	top := request.Top
	if top < 1 {
		top = defaultTop
	}

	return HTTPResult{
		Path:      from.path,
		Bytes:     from.bytes,
		Lines:     totals.lines,
		Requests:  totals.requests,
		Unparsed:  totals.unparsed,
		UniqueIPs: totals.clients.unique(),

		Methods:       totals.methods.top(0, totals.requests),
		StatusClasses: totals.classes.ordered(statusClasses(), totals.requests),
		StatusCodes:   totals.codes.top(top, totals.requests),
		TopPaths:      totals.paths.top(top, totals.requests),
		TopClients:    totals.clients.top(top, totals.requests),

		TotalResponseBytes:   totals.sent,
		AverageResponseBytes: totals.averageSent(),

		RankingTruncated: totals.paths.overflow || totals.clients.overflow,
		DurationMs:       millis(started),
	}, nil
}

// statusClasses is the fixed order every status listing uses.
func statusClasses() []string {
	return []string{"1xx", "2xx", "3xx", "4xx", "5xx"}
}
