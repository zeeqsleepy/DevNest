package git

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// SummaryRequest asks what a repository is.
type SummaryRequest struct {
	Path string
	// Now is the moment ages are measured against. The zero value means the
	// current time; a test supplies a fixed one.
	Now time.Time
}

// Remote is one configured remote and where it points.
type Remote struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// WorkingTree counts what is uncommitted.
type WorkingTree struct {
	Staged    int `json:"staged"`
	Modified  int `json:"modified"`
	Untracked int `json:"untracked"`
	Conflicts int `json:"conflicts"`
	// Clean is the field worth branching on, and it is not simply "everything
	// is zero": a repository with untracked files is usually still considered
	// clean by the person looking at it, so this says explicitly what it means.
	Clean bool `json:"clean"`
}

// SummaryResult describes a repository.
type SummaryResult struct {
	Root    string   `json:"root"`
	Branch  string   `json:"branch"`
	Head    string   `json:"head,omitempty"`
	Remotes []Remote `json:"remotes"`

	Commits  int         `json:"commits"`
	Branches int         `json:"branches"`
	Tags     int         `json:"tags"`
	Tree     WorkingTree `json:"workingTree"`

	// FirstCommit and LastCommit are absent in a repository with no commits,
	// which is a real state a freshly initialised repository is in.
	FirstCommit *time.Time `json:"firstCommit,omitempty"`
	LastCommit  *time.Time `json:"lastCommit,omitempty"`
	AgeDays     int        `json:"ageDays"`
	IdleDays    int        `json:"idleDays"`

	// Detached reports a HEAD that is not on a branch, which is worth saying
	// plainly: a commit made here is easy to lose.
	Detached bool `json:"detached"`
}

// Summary reports what a repository is.
func Summary(ctx context.Context, runner Runner, locator Locator, request SummaryRequest) (SummaryResult, error) {
	repository, err := open(ctx, runner, locator, request.Path)
	if err != nil {
		return SummaryResult{}, err
	}

	now := request.Now
	if now.IsZero() {
		now = time.Now()
	}

	result := SummaryResult{Root: repository.Root, Remotes: []Remote{}}

	if result.Branch, err = repository.text(ctx, "rev-parse", "--abbrev-ref", "HEAD"); err != nil {
		return SummaryResult{}, err
	}
	result.Detached = result.Branch == "HEAD"

	if result.Head, err = repository.text(ctx, "rev-parse", "--short", "HEAD"); err != nil {
		return SummaryResult{}, err
	}

	if result.Remotes, err = remotes(ctx, repository); err != nil {
		return SummaryResult{}, err
	}

	if result.Tree, err = workingTree(ctx, repository); err != nil {
		return SummaryResult{}, err
	}

	if result.Commits, err = count(ctx, repository, "rev-list", "--count", "HEAD"); err != nil {
		return SummaryResult{}, err
	}

	branches, err := repository.lines(ctx, quickTimeout,
		"for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return SummaryResult{}, err
	}
	result.Branches = len(branches)

	tags, err := repository.lines(ctx, quickTimeout, "tag", "--list")
	if err != nil {
		return SummaryResult{}, err
	}
	result.Tags = len(tags)

	if err := dates(ctx, repository, &result, now); err != nil {
		return SummaryResult{}, err
	}

	return result, nil
}

// remotes lists the configured remotes, one entry per name.
//
// git remote -v prints two lines per remote, one for fetch and one for push,
// which are the same URL in almost every repository. They are folded into one
// entry, because two identical rows read as a bug.
func remotes(ctx context.Context, repository *repository) ([]Remote, error) {
	lines, err := repository.lines(ctx, quickTimeout, "remote", "-v")
	if err != nil {
		return nil, err
	}

	found := make([]Remote, 0, 2)
	seen := make(map[string]bool, 2)

	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) < 2 || seen[parts[0]] {
			continue
		}
		seen[parts[0]] = true
		found = append(found, Remote{Name: parts[0], URL: parts[1]})
	}
	return found, nil
}

// workingTree counts what is uncommitted, from the porcelain status format.
//
// The v1 porcelain format is a documented, stable interface: two status
// characters and a path. Parsing the human-readable output instead would break
// the first time somebody's locale changed.
func workingTree(ctx context.Context, repository *repository) (WorkingTree, error) {
	output, err := repository.run(ctx, quickTimeout, "status", "--porcelain")
	if err != nil {
		return WorkingTree{}, err
	}
	if output.ExitCode != 0 {
		return WorkingTree{}, gitFailed([]string{"status"}, output)
	}

	tree := WorkingTree{}

	for _, line := range strings.Split(output.Stdout, "\n") {
		if len(line) < 2 {
			continue
		}

		index, worktree := line[0], line[1]
		switch {
		case index == '?' && worktree == '?':
			tree.Untracked++
		case index == 'U' || worktree == 'U' || (index == 'A' && worktree == 'A') ||
			(index == 'D' && worktree == 'D'):
			tree.Conflicts++
		default:
			if index != ' ' {
				tree.Staged++
			}
			if worktree != ' ' {
				tree.Modified++
			}
		}
	}

	tree.Clean = tree.Staged == 0 && tree.Modified == 0 && tree.Conflicts == 0
	return tree, nil
}

// count runs a subcommand whose entire output is a number.
func count(ctx context.Context, repository *repository, args ...string) (int, error) {
	text, err := repository.text(ctx, args...)
	if err != nil || text == "" {
		return 0, err
	}

	number, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0, nil
	}
	return number, nil
}

// dates fills in the first and last commit times and the ages derived from them.
func dates(ctx context.Context, repository *repository, result *SummaryResult, now time.Time) error {
	last, err := repository.text(ctx, "log", "-1", "--format=%cI")
	if err != nil {
		return err
	}
	if moment, ok := parseTime(last); ok {
		result.LastCommit = &moment
		result.IdleDays = daysSince(moment, now)
	}

	first, err := repository.text(ctx, "log", "--reverse", "--format=%cI", "--max-parents=0")
	if err != nil {
		return err
	}
	// A repository with several root commits prints several lines; the first
	// is the oldest, which is the one the age is measured from.
	if moment, ok := parseTime(firstLine(first)); ok {
		result.FirstCommit = &moment
		result.AgeDays = daysSince(moment, now)
	}

	return nil
}
