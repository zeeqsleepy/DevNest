package log

import (
	"io"

	"github.com/devnest/devnest/internal/platform/fs"
)

// Reader is everything this module needs from a filesystem.
//
// Three methods, all read-only. The module cannot walk a tree, move a file, or
// open a socket, and the interface is what enforces that rather than a comment
// asking nicely. A test satisfies it with a string.
type Reader interface {
	// Resolve returns an absolute path with symlinks resolved.
	Resolve(path string) (string, error)
	// Stat describes one path, which is where the file size comes from.
	Stat(path string) (fs.Entry, error)
	// Open returns a reader over the file's contents. The caller closes it.
	Open(path string) (io.ReadCloser, error)
}
