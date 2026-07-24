package security

import (
	"context"
	"io"

	"github.com/devnest/devnest/internal/platform/fs"
)

// Hasher is everything this module needs in order to compute a digest.
//
// It is narrow on purpose: the security module cannot walk a tree, cannot move
// a file, and cannot open a socket. What it can do is read one named file and
// hash a stream, and the interface says so.
type Hasher interface {
	// Resolve turns a path into an absolute one with symlinks resolved.
	Resolve(path string) (string, error)
	// Stat describes one path.
	Stat(path string) (fs.Entry, error)
	// Digest hashes a file in a single pass.
	Digest(ctx context.Context, path string, algorithms []fs.Algorithm) ([]fs.Checksum, error)
	// DigestReader hashes any stream, which is how text input is handled
	// without a second implementation of the hashing itself.
	DigestReader(ctx context.Context, reader io.Reader, algorithms []fs.Algorithm) ([]fs.Checksum, error)
}
