package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/core/git"
)

func TestGitSummaryTextReadsAsASentenceAboutTheRepository(t *testing.T) {
	first := time.Date(2025, time.January, 15, 9, 0, 0, 0, time.UTC)
	last := time.Date(2026, time.July, 20, 9, 0, 0, 0, time.UTC)

	result := git.SummaryResult{
		Root:    "/repo",
		Branch:  "main",
		Head:    "3c404ac",
		Remotes: []git.Remote{{Name: "origin", URL: "https://example.com/repo.git"}},
		Commits: 128, Branches: 3, Tags: 2,
		Tree:        git.WorkingTree{Modified: 1, Untracked: 2},
		FirstCommit: &first, LastCommit: &last,
		AgeDays: 555, IdleDays: 4,
	}

	got := render(t, gitSummaryText(result))
	for _, want := range []string{"main", "3c404ac", "origin", "128", "1 modified, 2 untracked", "555"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestGitSummaryTextNamesADetachedHead(t *testing.T) {
	result := git.SummaryResult{Root: "/repo", Branch: "HEAD", Detached: true}

	if got := render(t, gitSummaryText(result)); !strings.Contains(got, "detached HEAD") {
		t.Errorf("output = %q, want the detached state spelled out", got)
	}
}

func TestTreeSummaryDistinguishesCleanFromUntrackedOnly(t *testing.T) {
	cases := []struct {
		tree git.WorkingTree
		want string
	}{
		{git.WorkingTree{Clean: true}, "clean"},
		{git.WorkingTree{Clean: true, Untracked: 3}, "3 untracked"},
		{git.WorkingTree{Staged: 1, Modified: 2}, "1 staged, 2 modified"},
		{git.WorkingTree{Conflicts: 1}, "1 conflicted"},
	}

	for _, testCase := range cases {
		if got := treeSummary(testCase.tree); got != testCase.want {
			t.Errorf("treeSummary(%+v) = %q, want %q", testCase.tree, got, testCase.want)
		}
	}
}

func branch(name string, ageDays int, current bool, upstream string) git.Branch {
	return git.Branch{
		Name:       name,
		Current:    current,
		LastCommit: time.Date(2026, time.July, 20, 9, 0, 0, 0, time.UTC),
		AgeDays:    ageDays,
		Author:     "Ana",
		Upstream:   upstream,
		Stale:      ageDays >= 90,
	}
}

func TestGitBranchesTextMarksTheCurrentBranchAndUnpushedOnes(t *testing.T) {
	result := git.BranchResult{
		Branches: []git.Branch{
			branch("main", 1, true, "origin/main"),
			branch("feature/slow", 200, false, ""),
		},
		Count: 2, StaleDays: 90, StaleCount: 1,
	}

	got := render(t, gitBranchesText(result))
	if !strings.Contains(got, "* main") {
		t.Errorf("output = %q, want the current branch marked", got)
	}
	if !strings.Contains(got, "never pushed") {
		t.Errorf("output = %q, want a branch with no upstream described", got)
	}
	if !strings.Contains(got, "1 quiet") {
		t.Errorf("output = %q, want the stale count", got)
	}
}

// The commands are printed for a person to read and run. The text has to say
// that DevNest does not run them, because a list of commands in a tool's output
// reads like something the tool did.
func TestGitStaleTextSaysItDidNotRunTheCommands(t *testing.T) {
	result := git.StaleResult{
		BranchResult: git.BranchResult{
			Branches: []git.Branch{branch("feature/slow", 200, false, "")},
			Count:    1, StaleDays: 90, StaleCount: 1,
		},
		Commands: []string{"git branch -d feature/slow"},
	}

	got := render(t, gitStaleText(result))
	if !strings.Contains(got, "git branch -d feature/slow") {
		t.Errorf("output = %q, want the command printed", got)
	}
	if !strings.Contains(got, "does not run them") {
		t.Errorf("output = %q, want it to say the commands were not run", got)
	}
}

func TestGitStaleTextHandlesATidyRepository(t *testing.T) {
	got := render(t, gitStaleText(git.StaleResult{
		BranchResult: git.BranchResult{StaleDays: 90},
	}))

	if !strings.Contains(got, "No branch has been quiet") {
		t.Errorf("output = %q, want a sentence rather than an empty table", got)
	}
}

func TestGitContributorsTextShowsSharesAndTruncation(t *testing.T) {
	moment := time.Date(2026, time.July, 20, 9, 0, 0, 0, time.UTC)
	result := git.ContributorResult{
		Contributors: []git.Contributor{
			{Name: "Ana", Email: "ana@example.com", Commits: 80, First: moment, Last: moment,
				IdleDays: 4, Percent: 80},
		},
		Count: 5, Commits: 100, Truncated: true,
	}

	got := render(t, gitContributorsText(result))
	for _, want := range []string{"Ana", "80.0%", "--limit 0"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestGitLargeTextReportsWhatItExamined(t *testing.T) {
	result := git.LargeResult{
		Objects: []git.Object{
			{Hash: "aaa1111bbbb2222", Path: "assets/video.mp4", Bytes: 50 << 20},
		},
		Count: 1, TotalBytes: 50 << 20, Scanned: 12000,
	}

	got := render(t, gitLargeText(result))
	for _, want := range []string{"50.0 MB", "assets/video.mp4", "aaa1111b", "12,000"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestGitPathDefaultsToHere(t *testing.T) {
	path, err := gitPath(nil)
	if err != nil || path != "." {
		t.Errorf("gitPath(nil) = %q, %v, want the current directory", path, err)
	}

	if _, err := gitPath([]string{"a", "b"}); err == nil {
		t.Error("two repositories were accepted")
	}
}
