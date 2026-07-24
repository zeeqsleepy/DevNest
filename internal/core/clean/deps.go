package clean

import (
	"context"

	"github.com/devnest/devnest/internal/platform/fs"
)

// Inspector is everything the read-only half of this module needs.
//
// Nothing in it can delete anything. Scan takes an Inspector, so no amount of
// misreading its body can turn a listing into a removal: the type it was given
// has no method that destroys.
type Inspector interface {
	// Resolve turns a path into an absolute one with symlinks resolved.
	Resolve(path string) (string, error)
	// Stat describes one path.
	Stat(path string) (fs.Entry, error)
	// Walk visits a tree.
	Walk(ctx context.Context, options fs.WalkOptions, visit func(fs.Entry) error) error
	// Contains reports whether a resolved target lies inside a resolved root.
	Contains(root, target string) (bool, error)
	// ProtectedReason names why a path must not be operated on in bulk.
	ProtectedReason(path string) string
	// DeviceID identifies the filesystem a path sits on, where the platform
	// can say. The second value is false where it cannot.
	DeviceID(path string) (uint64, bool)
}

// Remover is an Inspector that can also delete. Only Apply takes one, and it
// is the only interface in DevNest with a destructive method on it.
type Remover interface {
	Inspector
	// RemoveAll deletes a directory and everything under it.
	RemoveAll(path string) error
}
