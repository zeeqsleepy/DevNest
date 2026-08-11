package git

import (
	"context"
	"sort"
	"strings"
)

// HotspotRequest asks where the commits concentrate.
type HotspotRequest struct {
	Path string
	// Limit caps how many files are reported. Zero means all of them.
	Limit int
	// Since narrows the history to commits after a date, in the form git
	// accepts (2026-01-01, "3 months ago"). Empty means the whole history.
	Since string
}

// HotspotFile is one file and how often a commit has touched it.
type HotspotFile struct {
	Path string `json:"path"`
	// Commits is how many commits changed this file, which is a proxy for
	// where the risk concentrates: the file that half of every change touches
	// is where a regression is most likely, whatever the file actually does.
	Commits int `json:"commits"`
}

// HotspotResult is the listing.
type HotspotResult struct {
	Root string `json:"root"`
	// Files is the reported listing, newest activity most commits first.
	Files []HotspotFile `json:"files"`
	// DistinctFiles is how many different files any commit in the window
	// touched, which may be more than Files reports when Limit cut it short.
	DistinctFiles int `json:"distinctFiles"`
	// Commits is how many commits were read.
	Commits int `json:"commits"`
	// Truncated says the listing is a subset, so a reader does not wonder
	// where the rest of the files went.
	Truncated bool `json:"truncated"`
}

// Hotspot reports the files a repository changes most often.
//
// One pass over the log, using --name-only so each commit lists the files it
// touched, and counting how many commits each path appears in. This is a proxy
// for where change concentrates, not a measurement of risk — a file that every
// change edits, or a file that never stops being rewritten, is where a
// regression is most likely to land.
func Hotspot(ctx context.Context, runner Runner, locator Locator, request HotspotRequest) (HotspotResult, error) {
	repository, err := open(ctx, runner, locator, request.Path)
	if err != nil {
		return HotspotResult{}, err
	}

	args := []string{"log", "--format=", "--name-only"}
	if trimmed := strings.TrimSpace(request.Since); trimmed != "" {
		args = append(args, "--since", trimmed)
	}

	output, err := repository.run(ctx, walkTimeout, args...)
	if err != nil {
		return HotspotResult{}, err
	}
	if output.ExitCode != 0 {
		return HotspotResult{}, gitFailed(args, output)
	}

	result := HotspotResult{Root: repository.Root, Files: []HotspotFile{}}
	touched := map[string]int{}

	// git separates each commit's listing with a blank line. Splitting on the
	// blank lines gives one record per commit; a record is the list of files
	// that commit changed, which with --format= carries no header line.
	records := splitRecords(output.Stdout)
	for _, record := range records {
		result.Commits++
		for _, path := range record {
			touched[path]++
		}
	}

	for path, commits := range touched {
		result.Files = append(result.Files, HotspotFile{Path: path, Commits: commits})
	}
	result.DistinctFiles = len(result.Files)

	// Most change first, then by path, so two runs over one repository produce
	// identical output even when two files have the same count.
	sort.Slice(result.Files, func(first, second int) bool {
		left, right := result.Files[first], result.Files[second]
		if left.Commits != right.Commits {
			return left.Commits > right.Commits
		}
		return left.Path < right.Path
	})

	if request.Limit > 0 && len(result.Files) > request.Limit {
		result.Files = result.Files[:request.Limit]
		result.Truncated = true
	}

	return result, nil
}

// splitRecords splits git's blank-line-separated records into each record's
// non-empty file paths.
func splitRecords(stdout string) [][]string {
	normalised := strings.ReplaceAll(stdout, "\r\n", "\n")
	records := strings.Split(normalised, "\n\n")

	found := make([][]string, 0, len(records))
	for _, record := range records {
		paths := make([]string, 0, 4)
		for _, line := range strings.Split(record, "\n") {
			if path := strings.TrimSpace(line); path != "" {
				paths = append(paths, path)
			}
		}
		if len(paths) > 0 {
			found = append(found, paths)
		}
	}
	return found
}
