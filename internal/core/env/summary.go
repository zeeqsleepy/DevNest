package env

import (
	"context"
	"time"

	"github.com/devnest/devnest/internal/platform/sys"
)

// SummaryRequest describes one environment summary.
type SummaryRequest struct {
	// Timeout bounds each toolchain probe. Zero means the platform default.
	Timeout time.Duration
	// IncludeMissing keeps undetected tools in the listing.
	IncludeMissing bool
}

// SummaryResult is the whole picture: the machine, the session, the tools.
type SummaryResult struct {
	Machine sys.Info `json:"machine"`
	Tools   []Tool   `json:"tools"`
	Found   int      `json:"toolsFound"`
	Missing int      `json:"toolsMissing"`
	// PathEntries and PathProblems are the headline numbers from the PATH
	// inspection. The detail is a command of its own, because the full
	// listing is forty lines nobody asked for here.
	PathEntries  int   `json:"pathEntries"`
	PathProblems int   `json:"pathProblems"`
	DurationMs   int64 `json:"durationMs"`
}

// Summarize describes the machine and what is installed on it.
//
// This is the command somebody runs on a machine that is not theirs, or on
// their own after something stopped working. It answers "what is this
// machine" in one screen, and every number in it has a command that shows the
// detail behind it.
//
// The PATH check here is the cheap half: entries, duplicates, and dead
// directories. Finding shadowed executables means reading every directory on
// PATH, which belongs in the command that exists to look for them.
func Summarize(ctx context.Context, deps Inspector, request SummaryRequest) (SummaryResult, error) {
	started := time.Now()

	tools, err := List(ctx, deps, ListRequest{
		Timeout:        request.Timeout,
		IncludeMissing: request.IncludeMissing,
	})
	if err != nil {
		return SummaryResult{}, err
	}

	paths, err := InspectPath(ctx, deps, PathRequest{})
	if err != nil {
		return SummaryResult{}, err
	}

	return SummaryResult{
		Machine:      deps.Describe(),
		Tools:        tools.Tools,
		Found:        tools.Found,
		Missing:      tools.Missing,
		PathEntries:  len(paths.Entries),
		PathProblems: paths.Problems,
		DurationMs:   time.Since(started).Milliseconds(),
	}, nil
}
