package git

import (
	"context"
	"sort"
	"strings"
	"time"
)

// ContributorRequest asks who has been committing.
type ContributorRequest struct {
	Path string
	// Limit caps how many contributors are reported. Zero means all of them.
	Limit int
	// Since narrows the history to commits after a date, in the form git
	// accepts (2026-01-01, "3 months ago"). Empty means the whole history.
	Since string
	// Now is the moment ages are measured against. Zero means the current time.
	Now time.Time
}

// Contributor is one author and what they have committed.
//
// Email is included because names collide and repositories routinely carry the
// same person under two spellings. It is not a secret: it is in every commit
// object in the repository already.
type Contributor struct {
	Name    string    `json:"name"`
	Email   string    `json:"email"`
	Commits int       `json:"commits"`
	First   time.Time `json:"firstCommit"`
	Last    time.Time `json:"lastCommit"`
	// IdleDays is how long since their last commit, which is the number that
	// answers "who still works on this".
	IdleDays int     `json:"idleDays"`
	Percent  float64 `json:"percent"`
}

// ContributorResult is the listing.
type ContributorResult struct {
	Root         string        `json:"root"`
	Contributors []Contributor `json:"contributors"`
	Count        int           `json:"count"`
	// Commits is the total the percentages are of, which may be more than the
	// listed contributors account for when Limit cut the list short.
	Commits int `json:"commits"`
	// Truncated says the listing is a subset, so a reader does not add up the
	// percentages and wonder where the rest went.
	Truncated bool `json:"truncated"`
}

// Contributors reports commit counts and activity by author.
//
// One pass over the log, aggregating in memory. The alternative, git shortlog,
// gives counts but not dates, and asking twice would mean two walks of the same
// history that could disagree if a commit arrived between them.
func Contributors(ctx context.Context, runner Runner, locator Locator, request ContributorRequest) (ContributorResult, error) {
	repository, err := open(ctx, runner, locator, request.Path)
	if err != nil {
		return ContributorResult{}, err
	}

	now := request.Now
	if now.IsZero() {
		now = time.Now()
	}

	args := []string{"log", "--format=%an" + separator + "%ae" + separator + "%cI"}
	if trimmed := strings.TrimSpace(request.Since); trimmed != "" {
		args = append(args, "--since", trimmed)
	}

	lines, err := repository.lines(ctx, walkTimeout, args...)
	if err != nil {
		return ContributorResult{}, err
	}

	result := ContributorResult{Root: repository.Root, Contributors: []Contributor{}}
	byIdentity := make(map[string]*Contributor, 16)

	for _, line := range lines {
		parts := fields(line)
		if len(parts) < 3 {
			continue
		}

		name, email := parts[0], strings.ToLower(strings.TrimSpace(parts[1]))
		moment, dated := parseTime(parts[2])

		contributor, seen := byIdentity[email]
		if !seen {
			contributor = &Contributor{Name: name, Email: email}
			byIdentity[email] = contributor
		}
		contributor.Commits++
		result.Commits++

		if !dated {
			continue
		}
		if contributor.Last.IsZero() || moment.After(contributor.Last) {
			contributor.Last = moment
		}
		if contributor.First.IsZero() || moment.Before(contributor.First) {
			contributor.First = moment
		}
	}

	for _, contributor := range byIdentity {
		contributor.IdleDays = daysSince(contributor.Last, now)
		contributor.Percent = share(contributor.Commits, result.Commits)
		result.Contributors = append(result.Contributors, *contributor)
	}

	// Most commits first, then by name, so two runs over one repository
	// produce identical output even when two people have the same count.
	sort.Slice(result.Contributors, func(first, second int) bool {
		left, right := result.Contributors[first], result.Contributors[second]
		if left.Commits != right.Commits {
			return left.Commits > right.Commits
		}
		return left.Email < right.Email
	})

	result.Count = len(result.Contributors)
	if request.Limit > 0 && len(result.Contributors) > request.Limit {
		result.Contributors = result.Contributors[:request.Limit]
		result.Truncated = true
	}

	return result, nil
}

// share keeps percentages to one decimal place, so two runs over one
// repository produce byte-identical output.
func share(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	value := float64(part) / float64(whole) * 100
	return float64(int(value*10+0.5)) / 10
}
