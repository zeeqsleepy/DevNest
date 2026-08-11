//go:build !windows

package fs

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// pathKey normalises a path for comparison. Linux and macOS filesystems are
// treated as case-sensitive, which is correct on Linux and conservative on a
// case-insensitive macOS volume: two paths differing in case compare as
// different, so a containment check errs towards refusing.
func pathKey(path string) string {
	return filepath.Clean(path)
}

// sameFile reports whether two paths name the same file and no move is
// needed. On a case-sensitive filesystem that can only be the identical path.
func sameFile(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

// isHidden reports whether an entry should be treated as hidden.
func isHidden(_, name string) bool {
	return strings.HasPrefix(name, ".")
}

// protectedReason names why a bulk operation must not run at this path.
//
// These are the directories where a mistyped path turns a routine command into
// an incident. None of them is a place anyone means to reorganise.
func protectedReason(path string) string {
	cleaned := filepath.Clean(path)

	if cleaned == string(filepath.Separator) {
		return "it is the filesystem root"
	}

	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(home) == cleaned {
		return "it is your home directory"
	}

	system := []string{
		"/bin", "/boot", "/dev", "/etc", "/lib", "/lib64", "/proc", "/root",
		"/sbin", "/sys", "/usr", "/var",
		"/Applications", "/Library", "/System", "/Volumes",
	}
	for _, directory := range system {
		if cleaned == directory {
			return "it is a system directory"
		}
	}

	return ""
}

// deviceID reports which filesystem a path lives on.
//
// Crossing a mount point during a bulk operation is how a cleanup aimed at a
// project directory reaches a network share or an external disk mounted inside
// it. On Unix the device number answers that in one stat call.
func deviceID(path string) (uint64, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(stat.Dev), true
}
