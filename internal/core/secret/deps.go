package secret

import (
	"context"
	"io"

	"github.com/devnest/devnest/internal/platform/fs"
	"github.com/devnest/devnest/internal/platform/proc"
)

// Reader is everything the working-tree scan needs from a filesystem. Four
// read-only methods: this module cannot move, write, or delete anything.
type Reader interface {
	Resolve(path string) (string, error)
	Stat(path string) (fs.Entry, error)
	Open(path string) (io.ReadCloser, error)
	Walk(ctx context.Context, options fs.WalkOptions, visit func(fs.Entry) error) error
}

// Runner is what the history scan needs: the ability to ask git for the
// patches in a repository's history. Only History takes one, so a working-tree
// scan cannot start a process.
type Runner interface {
	Run(ctx context.Context, command proc.Command) (proc.Output, error)
	Lookup(name string) []string
}
