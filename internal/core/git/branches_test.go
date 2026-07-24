package git

import (
	"context"
	"strings"
	"testing"
)

// branchLine builds one for-each-ref record in the format the module asks for.
func branchLine(name, date, author, subject, upstream string, current bool) string {
	head := " "
	if current {
		head = "*"
	}
	return strings.Join([]string{name, date, author, subject, upstream, head}, separator)
}

func branchFake() *fakeGit {
	return repositoryFake().answers("for-each-ref", strings.Join([]string{
		branchLine("main", "2026-07-23T10:00:00Z", "Ana", "fix the parser", "origin/main", true),
		branchLine("feature/slow", "2026-01-02T10:00:00Z", "Budi", "start the thing", "", false),
		branchLine("hotfix/old", "2025-11-01T10:00:00Z", "Ana", "patch", "origin/hotfix/old", false),
	}, "\n")+"\n")
}

func TestBranchesListsNewestActivityFirst(t *testing.T) {
	system := branchFake()

	result, err := Branches(context.Background(), system, system, BranchRequest{Now: reference})
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}

	if result.Count != 3 {
		t.Fatalf("count = %d, want 3", result.Count)
	}
	if result.Branches[0].Name != "main" || result.Branches[2].Name != "hotfix/old" {
		t.Errorf("order = %s, want newest first",
			result.Branches[0].Name+" ... "+result.Branches[2].Name)
	}
	if !result.Branches[0].Current {
		t.Error("the checked-out branch is not marked as current")
	}
	if result.Branches[1].Upstream != "" {
		t.Errorf("upstream = %q, want it empty for a branch never pushed",
			result.Branches[1].Upstream)
	}
}

func TestBranchesMarkStalenessAgainstTheGivenDay(t *testing.T) {
	system := branchFake()

	result, err := Branches(context.Background(), system, system, BranchRequest{Now: reference})
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}

	if result.StaleDays != DefaultStaleDays {
		t.Errorf("staleDays = %d, want the default", result.StaleDays)
	}
	if result.Branches[0].Stale {
		t.Error("a branch committed to yesterday was called stale")
	}
	if result.StaleCount != 2 {
		t.Errorf("staleCount = %d, want the two quiet branches", result.StaleCount)
	}
}

func TestBranchesRespectsACustomStalenessWindow(t *testing.T) {
	system := branchFake()

	result, err := Branches(context.Background(), system, system, BranchRequest{
		Now:       reference,
		StaleDays: 400,
	})
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}

	if result.StaleCount != 0 {
		t.Errorf("staleCount = %d, want nothing stale at 400 days", result.StaleCount)
	}
}

// The current branch is never in the stale listing. Suggesting the deletion of
// the branch somebody is standing on is a suggestion that cannot be taken.
func TestStaleLeavesOutTheCurrentBranch(t *testing.T) {
	system := repositoryFake().answers("for-each-ref", strings.Join([]string{
		branchLine("main", "2020-01-01T10:00:00Z", "Ana", "old", "origin/main", true),
		branchLine("feature/slow", "2026-01-02T10:00:00Z", "Budi", "start", "", false),
	}, "\n")+"\n")

	result, err := Stale(context.Background(), system, system, BranchRequest{Now: reference}, false)
	if err != nil {
		t.Fatalf("Stale: %v", err)
	}

	for _, branch := range result.Branches {
		if branch.Current {
			t.Fatalf("the current branch %q is in the stale listing", branch.Name)
		}
	}
}

// The commands are text, and they use -d rather than -D: a command copied
// without reading it still cannot throw away unmerged work.
func TestStalePrintsDeletionCommandsWithoutRunningThem(t *testing.T) {
	system := branchFake()

	result, err := Stale(context.Background(), system, system, BranchRequest{Now: reference}, true)
	if err != nil {
		t.Fatalf("Stale: %v", err)
	}

	if len(result.Commands) != result.Count {
		t.Fatalf("commands = %v, want one per stale branch", result.Commands)
	}
	for _, command := range result.Commands {
		if !strings.HasPrefix(command, "git branch -d ") {
			t.Errorf("command = %q, want the safe deletion form", command)
		}
	}

	for _, invocation := range system.invocations {
		if subcommand, ok := subcommandOf(invocation); ok && subcommand == "branch" {
			t.Fatalf("stale ran a branch subcommand: git %s", strings.Join(invocation, " "))
		}
	}
}

func TestStaleWithoutCommandsAsksForNone(t *testing.T) {
	system := branchFake()

	result, err := Stale(context.Background(), system, system, BranchRequest{Now: reference}, false)
	if err != nil {
		t.Fatalf("Stale: %v", err)
	}
	if result.Commands != nil {
		t.Errorf("commands = %v, want none unless asked for", result.Commands)
	}
}

func TestBranchesHandlesARepositoryWithNoBranches(t *testing.T) {
	system := repositoryFake().answers("for-each-ref", "")

	result, err := Branches(context.Background(), system, system, BranchRequest{Now: reference})
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	if result.Count != 0 || len(result.Branches) != 0 {
		t.Errorf("result = %+v, want an empty listing", result)
	}
}
