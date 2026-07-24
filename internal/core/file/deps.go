package file

import (
	"context"

	"github.com/devnest/devnest/internal/platform/fs"
)

// Inspector is everything this module needs in order to look at a filesystem
// without changing it.
type Inspector interface {
	// Resolve returns an absolute path with symlinks resolved.
	Resolve(path string) (string, error)
	// Contains reports whether target lies inside root. Both must be resolved.
	Contains(root, target string) (bool, error)
	// ProtectedReason names why a bulk operation must not run at a path.
	ProtectedReason(path string) string
	// Stat describes one path.
	Stat(path string) (fs.Entry, error)
	// Walk visits entries under a root in a deterministic order.
	Walk(ctx context.Context, options fs.WalkOptions, visit func(fs.Entry) error) error
	// Digest computes checksums in a single pass over a file.
	Digest(ctx context.Context, path string, algorithms []fs.Algorithm) ([]fs.Checksum, error)
}

// Mover adds the operations that change the disk.
//
// The split is deliberate: an operation that takes an Inspector cannot move
// anything, and the signature says so without anyone reading the body. Only
// Organize and Rename take a Mover, and only when the caller has asked them to
// apply their plan.
type Mover interface {
	Inspector
	// Exists reports whether a path is already taken.
	Exists(path string) (bool, error)
	// EnsureDir creates a directory and any missing parents.
	EnsureDir(path string) error
	// Move renames source to destination, refusing to replace anything.
	Move(source, destination string) error
}
