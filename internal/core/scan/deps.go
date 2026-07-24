package scan

import (
	"context"
	"io"

	"github.com/devnest/devnest/internal/platform/fs"
)

// Inspector is everything this module needs from a filesystem.
//
// Read-only, all four. The scanner cannot move a file, create a directory, or
// open a socket, and the interface is what enforces that rather than a comment
// asking nicely.
type Inspector interface {
	// Resolve returns an absolute path with symlinks resolved.
	Resolve(path string) (string, error)
	// Stat describes one path.
	Stat(path string) (fs.Entry, error)
	// Walk visits entries under a root in a deterministic order.
	Walk(ctx context.Context, options fs.WalkOptions, visit func(fs.Entry) error) error
	// Open returns a reader over a file's contents, for the commands that
	// have to look inside.
	Open(path string) (io.ReadCloser, error)
}
