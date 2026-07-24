// Package git is DevNest's repository module: what a repository is, which
// branches have gone quiet, who has been committing, and what is making the
// history large.
//
// # It shells out, and that is the design
//
// This module runs the git executable rather than embedding a git library. Any
// machine with a repository to inspect has git installed, its command line is
// a stable and documented interface, and a git implementation is an enormous
// surface to carry for read-only reporting. When git is absent the command says
// so in a sentence rather than failing somewhere obscure.
//
// # Read-only, with no exceptions
//
// Nothing here commits, pushes, rebases, fetches, or deletes a branch. Every
// subcommand it runs is one that reports. `stale` will print the deletion
// commands for the branches it found, and printing them is as far as it goes:
// the user reads the list and decides.
//
// Each operation names its subcommand and arguments as literals in this
// package, nothing is assembled from user input, and there is no shell
// anywhere in the path, so a branch named `--upload-pack=rm` is a string git
// rejects rather than an argument anything acts on.
package git

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/proc"
)

// Timeouts. Reading a summary is quick; walking every object in a large
// repository is not, and a limit that fits the first would make the second
// impossible.
const (
	quickTimeout = 15 * time.Second
	walkTimeout  = 90 * time.Second
)

// outputLimit is how much git output a command will hold. Two megabytes is
// tens of thousands of branches or commits, and a repository past that wants a
// purpose-built tool rather than a summary.
const outputLimit = 2 << 20

// separator joins fields in git's format strings.
//
// A null byte would be the obvious choice, and it cannot be used: an argument
// vector is null-terminated on every platform, so no argument can contain one,
// and the operating system rejects the whole invocation. The unit separator,
// 0x1F, is the next best thing. Git forbids ASCII control characters in
// reference names outright, so a branch cannot contain it, and an author name
// or a subject line holding one is a case nobody has ever produced.
//
// A tab or a pipe would be wrong for the usual reason: both appear in commit
// subjects, and a subject with a tab in it would silently become two fields.
const separator = "\x1f"

// repository is a located repository, ready to be asked questions.
type repository struct {
	// Root is the top level of the working tree, which is what every result
	// reports rather than the path the user happened to type.
	Root   string
	runner Runner
}

// open finds the repository containing a path and confirms git can run.
//
// Both failures are ones a user can act on and neither should arrive as a
// wrapped exit status: git being absent is an installation problem, and a
// directory not being a repository is a typo.
func open(ctx context.Context, runner Runner, locator Locator, path string) (*repository, error) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}

	resolved, err := locator.Resolve(path)
	if err != nil {
		return nil, err
	}

	if len(runner.Lookup("git")) == 0 {
		return nil, errors.New(errors.CodeNotFound, "git is not on PATH").
			WithHint("this command reports on a repository by asking git about it; " +
				"install git, or check that PATH has not been narrowed")
	}

	found := &repository{Root: resolved, runner: runner}

	top, err := found.text(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	if top == "" {
		return nil, errors.New(errors.CodeInvalidInput,
			"%s is not inside a git repository", resolved).
			WithHint("run this from a repository, or pass the path of one")
	}

	found.Root = top
	return found, nil
}

// run executes one git subcommand inside the repository.
func (r *repository) run(ctx context.Context, timeout time.Duration, args ...string) (proc.Output, error) {
	// -c color.ui=false and --no-pager, because a user's global configuration
	// can otherwise colour or paginate output that is about to be parsed.
	full := append([]string{"-c", "color.ui=false", "--no-pager"}, args...)

	return r.runner.Run(ctx, proc.Command{
		Name:    "git",
		Args:    full,
		Dir:     r.Root,
		Timeout: timeout,
		Limit:   outputLimit,
	})
}

// text runs a subcommand expected to print one line, and returns it trimmed.
//
// A non-zero exit comes back as an empty string rather than an error: plenty
// of these questions have "there is no answer" as a legitimate reply. A
// repository with no commits has no HEAD, and that is a state to report, not a
// failure.
func (r *repository) text(ctx context.Context, args ...string) (string, error) {
	output, err := r.run(ctx, quickTimeout, args...)
	if err != nil {
		return "", err
	}
	if output.ExitCode != 0 {
		return "", nil
	}
	return strings.TrimSpace(output.Stdout), nil
}

// lines runs a subcommand and splits its output into lines, dropping empties.
func (r *repository) lines(ctx context.Context, timeout time.Duration, args ...string) ([]string, error) {
	output, err := r.run(ctx, timeout, args...)
	if err != nil {
		return nil, err
	}
	if output.ExitCode != 0 {
		return nil, gitFailed(args, output)
	}

	raw := strings.Split(strings.ReplaceAll(output.Stdout, "\r\n", "\n"), "\n")
	found := make([]string, 0, len(raw))
	for _, line := range raw {
		if trimmed := strings.TrimRight(line, "\r"); strings.TrimSpace(trimmed) != "" {
			found = append(found, trimmed)
		}
	}
	return found, nil
}

// gitFailed turns a non-zero exit into an error carrying what git said.
//
// git's own messages are good, and rewriting them into something vaguer helps
// nobody. What this adds is the classification and the subcommand, because the
// exit status alone does not say which of several calls failed.
func gitFailed(args []string, output proc.Output) error {
	message := strings.TrimSpace(output.Stderr)
	if message == "" {
		message = strings.TrimSpace(output.Stdout)
	}
	if message == "" {
		message = "git exited with status " + strconv.Itoa(output.ExitCode)
	}

	return errors.New(errors.CodeInternal, "git %s: %s", args[0], firstLine(message))
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return strings.TrimSpace(text[:index])
	}
	return text
}

// fields splits a null-separated record.
func fields(line string) []string {
	return strings.Split(line, separator)
}

// parseTime reads the strict ISO 8601 form git prints for %cI and %aI.
func parseTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// daysSince reports whole days between a moment and now, never negative. A
// commit dated in the future, which happens with a wrong clock or a rewritten
// history, is reported as zero days old rather than as a negative age.
func daysSince(moment, now time.Time) int {
	days := int(now.Sub(moment).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}
