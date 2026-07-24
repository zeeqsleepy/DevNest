package git

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/proc"
)

// reference is the moment every age is measured against, so a test that passes
// today passes in a year.
var reference = time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)

// fakeGit answers scripted git invocations and records what it was asked.
//
// The recording is half the point: this module's contract includes never
// running a subcommand that writes, and the only way to assert that is to look
// at what it actually ran.
type fakeGit struct {
	responses map[string]proc.Output
	missing   bool

	invocations [][]string
}

func newFakeGit() *fakeGit {
	return &fakeGit{responses: map[string]proc.Output{}}
}

// answers scripts a response for a subcommand, keyed by the first argument
// after the global flags.
func (f *fakeGit) answers(subcommand, stdout string) *fakeGit {
	f.responses[subcommand] = proc.Output{Stdout: stdout}
	return f
}

func (f *fakeGit) fails(subcommand, stderr string, code int) *fakeGit {
	f.responses[subcommand] = proc.Output{Stderr: stderr, ExitCode: code}
	return f
}

func (f *fakeGit) Run(_ context.Context, command proc.Command) (proc.Output, error) {
	f.invocations = append(f.invocations, command.Args)

	key, ok := subcommandOf(command.Args)
	if !ok {
		return proc.Output{ExitCode: 1}, nil
	}

	// rev-parse answers two different questions, so it is keyed by both words.
	if key == "rev-parse" {
		for _, argument := range command.Args {
			if argument == "--show-toplevel" || argument == "--abbrev-ref" || argument == "--short" {
				key = "rev-parse " + argument
				break
			}
		}
	}
	if key == "log" {
		key = logKey(command.Args)
	}

	response, scripted := f.responses[key]
	if !scripted {
		return proc.Output{}, nil
	}
	return response, nil
}

func (f *fakeGit) Lookup(name string) []string {
	if f.missing || name != "git" {
		return nil
	}
	return []string{"/usr/bin/git"}
}

// Resolve satisfies Locator; the fake filesystem is a single directory.
func (f *fakeGit) Resolve(path string) (string, error) {
	if strings.TrimSpace(path) == "" || path == "." {
		return "/repo", nil
	}
	return path, nil
}

// subcommandOf finds the git subcommand, skipping the global flags this module
// always passes.
func subcommandOf(args []string) (string, bool) {
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-c", "--no-pager":
			if args[index] == "-c" {
				index++
			}
		default:
			return args[index], true
		}
	}
	return "", false
}

// logKey distinguishes the several log invocations the summary makes.
func logKey(args []string) string {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "--max-parents=0"):
		return "log first"
	case strings.Contains(joined, "-1"):
		return "log last"
	default:
		return "log"
	}
}

func assertCode(t *testing.T, err error, want errors.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with code %q, got nil", want)
	}
	if got := errors.CodeOf(err); got != want {
		t.Fatalf("code = %q, want %q (%v)", got, want, err)
	}
}

// repository scripts a small but complete repository.
func repositoryFake() *fakeGit {
	return newFakeGit().
		answers("rev-parse --show-toplevel", "/repo\n").
		answers("rev-parse --abbrev-ref", "main\n").
		answers("rev-parse --short", "3c404ac\n").
		answers("remote", "origin\thttps://example.com/repo.git (fetch)\n"+
			"origin\thttps://example.com/repo.git (push)\n").
		answers("status", " M internal/core/git/git.go\n?? notes.txt\nA  new.go\n").
		answers("rev-list", "128\n").
		answers("for-each-ref", "main\n").
		answers("tag", "v0.1.0\nv0.2.0\n").
		answers("log last", "2026-07-20T09:00:00Z\n").
		answers("log first", "2025-01-15T09:00:00Z\n")
}

func TestSummaryReportsWhatARepositoryIs(t *testing.T) {
	system := repositoryFake()

	result, err := Summary(context.Background(), system, system, SummaryRequest{Now: reference})
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}

	if result.Root != "/repo" || result.Branch != "main" || result.Head != "3c404ac" {
		t.Errorf("result = %+v, want the repository identified", result)
	}
	if result.Commits != 128 || result.Tags != 2 || result.Branches != 1 {
		t.Errorf("counts = %+v, want 128 commits, 2 tags, 1 branch", result)
	}
	if len(result.Remotes) != 1 || result.Remotes[0].Name != "origin" {
		t.Errorf("remotes = %+v, want fetch and push folded into one entry", result.Remotes)
	}
	if result.AgeDays != 555 || result.IdleDays != 4 {
		t.Errorf("age = %d, idle = %d, want them measured against the given time",
			result.AgeDays, result.IdleDays)
	}
}

func TestSummaryCountsTheWorkingTree(t *testing.T) {
	system := repositoryFake()

	result, err := Summary(context.Background(), system, system, SummaryRequest{Now: reference})
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}

	tree := result.Tree
	if tree.Modified != 1 || tree.Untracked != 1 || tree.Staged != 1 {
		t.Errorf("tree = %+v, want one of each", tree)
	}
	// Untracked files alone do not make a tree dirty, but staged ones do.
	if tree.Clean {
		t.Error("a tree with staged and modified files was reported as clean")
	}
}

func TestSummaryReportsADetachedHead(t *testing.T) {
	system := repositoryFake().answers("rev-parse --abbrev-ref", "HEAD\n")

	result, err := Summary(context.Background(), system, system, SummaryRequest{Now: reference})
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !result.Detached {
		t.Error("a detached HEAD was not reported as detached")
	}
}

// A repository with no commits is a real state, not a failure: git init and
// nothing else.
func TestSummaryHandlesARepositoryWithNoCommits(t *testing.T) {
	system := newFakeGit().
		answers("rev-parse --show-toplevel", "/repo\n").
		fails("rev-parse --abbrev-ref", "fatal: ambiguous argument 'HEAD'", 128).
		fails("rev-list", "fatal: bad revision", 128).
		fails("log last", "fatal: your current branch does not have any commits yet", 128).
		fails("log first", "fatal: bad revision", 128)

	result, err := Summary(context.Background(), system, system, SummaryRequest{Now: reference})
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if result.Commits != 0 || result.LastCommit != nil || result.FirstCommit != nil {
		t.Errorf("result = %+v, want an empty repository described rather than an error", result)
	}
}

func TestOpenReportsAMissingGit(t *testing.T) {
	system := repositoryFake()
	system.missing = true

	_, err := Summary(context.Background(), system, system, SummaryRequest{})
	assertCode(t, err, errors.CodeNotFound)
}

func TestOpenReportsSomethingThatIsNotARepository(t *testing.T) {
	system := newFakeGit().fails("rev-parse --show-toplevel", "fatal: not a git repository", 128)

	_, err := Summary(context.Background(), system, system, SummaryRequest{})
	assertCode(t, err, errors.CodeInvalidInput)
}

// The module's central promise. Every invocation it makes has to be a
// subcommand that reports, and this is the test that would fail if somebody
// added a fetch to "make the summary more accurate".
func TestNothingRunsAWritingSubcommand(t *testing.T) {
	system := repositoryFake().
		answers("for-each-ref", "main\x002026-07-20T09:00:00Z\x00Ana\x00fix\x00origin/main\x00*\n").
		answers("log", "Ana\x00ana@example.com\x002026-07-20T09:00:00Z\n").
		answers("rev-list", "abc123 path/to/file\n").
		answers("cat-file", "abc123 blob 4096\n")

	ctx := context.Background()
	_, _ = Summary(ctx, system, system, SummaryRequest{Now: reference})
	_, _ = Branches(ctx, system, system, BranchRequest{Now: reference})
	_, _ = Stale(ctx, system, system, BranchRequest{Now: reference}, true)
	_, _ = Contributors(ctx, system, system, ContributorRequest{Now: reference})
	_, _ = Large(ctx, system, system, LargeRequest{})

	// Subcommands that always write. `remote` and `tag` are absent because
	// this module uses their listing forms; the writing forms of those are
	// checked as two words below.
	writing := map[string]bool{
		"commit": true, "push": true, "pull": true, "fetch": true, "merge": true,
		"rebase": true, "reset": true, "checkout": true, "switch": true,
		"clean": true, "gc": true, "prune": true, "branch": true, "rm": true,
		"mv": true, "stash": true, "apply": true, "am": true, "cherry-pick": true,
		"revert": true, "filter-branch": true, "update-ref": true, "config": true,
	}
	writingPairs := map[string]bool{
		"remote add": true, "remote remove": true, "remote set-url": true,
		"remote rename": true, "tag -d": true, "tag --delete": true,
	}

	for _, invocation := range system.invocations {
		subcommand, ok := subcommandOf(invocation)
		if !ok {
			continue
		}

		if writing[subcommand] {
			t.Errorf("ran a subcommand that can write: git %s", strings.Join(invocation, " "))
		}

		for index, argument := range invocation {
			if argument == subcommand && index+1 < len(invocation) {
				if writingPairs[subcommand+" "+invocation[index+1]] {
					t.Errorf("ran a subcommand that can write: git %s",
						strings.Join(invocation, " "))
				}
				break
			}
		}
	}

	if len(system.invocations) == 0 {
		t.Fatal("nothing ran at all, so this test proved nothing")
	}
}
