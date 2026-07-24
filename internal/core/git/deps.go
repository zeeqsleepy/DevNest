package git

import (
	"context"

	"github.com/devnest/devnest/internal/platform/proc"
)

// Runner is everything this module needs in order to ask git a question.
//
// Two methods, and neither of them can write anything: this module builds an
// argument vector, hands it over, and reads what came back. Every command it
// builds is a read-only git subcommand, and that is a property of the table in
// commands.go rather than of this interface, which is why the table is short
// and in one place.
type Runner interface {
	// Run executes a command and waits for it.
	Run(ctx context.Context, command proc.Command) (proc.Output, error)
	// Lookup reports where a program resolves on PATH, or nothing.
	Lookup(name string) []string
}

// Locator finds the repository a path belongs to. Resolving is enough: git
// itself decides what is and is not a repository, and asking it is more
// reliable than looking for a .git directory that may be a file in a worktree.
type Locator interface {
	Resolve(path string) (string, error)
}
