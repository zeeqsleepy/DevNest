package git

import (
	"context"
	"sort"
	"strings"
	"time"
)

// DefaultStaleDays is how long a branch has to be quiet before this module
// calls it stale.
//
// Ninety days is a quarter: long enough that a branch someone is genuinely
// working on slowly is not flagged, short enough to catch the ones nobody
// remembers opening.
const DefaultStaleDays = 90

// BranchRequest asks about the branches in a repository.
type BranchRequest struct {
	Path string
	// StaleDays is the age at which a branch counts as stale. Zero means
	// DefaultStaleDays. Only Stale uses it; Branches reports the flag for
	// every branch so one listing answers both questions.
	StaleDays int
	// Merged, when set, restricts the listing to branches already merged into
	// the current branch. It is what makes the stale listing actionable: a
	// merged branch that has been quiet for a quarter is one nobody will miss.
	Merged bool
	// Now is the moment ages are measured against. Zero means the current time.
	Now time.Time
}

// Branch is one local branch.
type Branch struct {
	Name string `json:"name"`
	// Current marks the branch HEAD is on, which is the one a deletion command
	// would fail on, so it is worth having in the data rather than inferred.
	Current    bool      `json:"current"`
	LastCommit time.Time `json:"lastCommit"`
	AgeDays    int       `json:"ageDays"`
	Author     string    `json:"author"`
	Subject    string    `json:"subject"`
	// Upstream is the tracking branch, empty when there is none. A branch with
	// no upstream has never been pushed, which changes what deleting it costs.
	Upstream string `json:"upstream,omitempty"`
	Stale    bool   `json:"stale"`
}

// BranchResult is the listing.
type BranchResult struct {
	Root      string   `json:"root"`
	Branches  []Branch `json:"branches"`
	Count     int      `json:"count"`
	StaleDays int      `json:"staleDays"`
	// StaleCount is reported by both commands, so a plain listing already
	// answers "how many of these should I look at".
	StaleCount int `json:"staleCount"`
}

// StaleResult is the stale listing, plus the commands that would remove them.
type StaleResult struct {
	BranchResult
	// Commands are the git invocations that would delete these branches. They
	// are printed and never run: this module reports, and the user decides.
	// The field is populated only when the caller asks for it.
	Commands []string `json:"commands,omitempty"`
}

// Branches lists the local branches, newest activity first.
func Branches(ctx context.Context, runner Runner, locator Locator, request BranchRequest) (BranchResult, error) {
	repository, err := open(ctx, runner, locator, request.Path)
	if err != nil {
		return BranchResult{}, err
	}
	return branchListing(ctx, repository, request)
}

// Stale lists the branches nobody has touched for a while.
//
// A stale branch is a report, never an action. The deletion commands, when
// asked for, are text: they are printed for a person to read, edit, and run
// themselves. A tool that deletes branches on a timer is a tool that will one
// day delete the branch somebody was about to open a pull request from.
func Stale(ctx context.Context, runner Runner, locator Locator, request BranchRequest, withCommands bool) (StaleResult, error) {
	repository, err := open(ctx, runner, locator, request.Path)
	if err != nil {
		return StaleResult{}, err
	}

	listing, err := branchListing(ctx, repository, request)
	if err != nil {
		return StaleResult{}, err
	}

	stale := make([]Branch, 0, len(listing.Branches))
	for _, branch := range listing.Branches {
		if branch.Stale && !branch.Current {
			stale = append(stale, branch)
		}
	}

	listing.Branches = stale
	listing.Count = len(stale)

	result := StaleResult{BranchResult: listing}
	if withCommands {
		result.Commands = deletionCommands(stale)
	}
	return result, nil
}

// branchListing is the shared listing both commands are views of.
func branchListing(ctx context.Context, repository *repository, request BranchRequest) (BranchResult, error) {
	now := request.Now
	if now.IsZero() {
		now = time.Now()
	}

	staleDays := request.StaleDays
	if staleDays <= 0 {
		staleDays = DefaultStaleDays
	}

	args := []string{
		"for-each-ref",
		"--format=%(refname:short)" + separator +
			"%(committerdate:iso-strict)" + separator +
			"%(authorname)" + separator +
			"%(contents:subject)" + separator +
			"%(upstream:short)" + separator +
			"%(HEAD)",
		"refs/heads",
	}
	if request.Merged {
		args = append(args, "--merged")
	}

	lines, err := repository.lines(ctx, quickTimeout, args...)
	if err != nil {
		return BranchResult{}, err
	}

	result := BranchResult{
		Root:      repository.Root,
		Branches:  make([]Branch, 0, len(lines)),
		StaleDays: staleDays,
	}

	for _, line := range lines {
		parts := fields(line)
		if len(parts) < 6 || strings.TrimSpace(parts[0]) == "" {
			continue
		}

		branch := Branch{
			Name:     parts[0],
			Author:   parts[2],
			Subject:  parts[3],
			Upstream: parts[4],
			Current:  strings.TrimSpace(parts[5]) == "*",
		}

		if moment, ok := parseTime(parts[1]); ok {
			branch.LastCommit = moment
			branch.AgeDays = daysSince(moment, now)
			branch.Stale = branch.AgeDays >= staleDays
		}

		if branch.Stale {
			result.StaleCount++
		}
		result.Branches = append(result.Branches, branch)
	}

	// Oldest last: a listing is read from the top, and the branches worth
	// noticing are the ones that have not moved.
	sort.SliceStable(result.Branches, func(first, second int) bool {
		return result.Branches[first].LastCommit.After(result.Branches[second].LastCommit)
	})

	result.Count = len(result.Branches)
	return result, nil
}

// deletionCommands writes the commands that would delete these branches.
//
// -d rather than -D, deliberately: -d refuses a branch that is not merged, so
// a command copied out of this list without reading it still cannot throw work
// away. Somebody who genuinely wants the unmerged branch gone has to change
// the letter themselves, which is the point at which they will think about it.
func deletionCommands(branches []Branch) []string {
	commands := make([]string, 0, len(branches))
	for _, branch := range branches {
		commands = append(commands, "git branch -d "+branch.Name)
	}
	return commands
}
