//go:build !windows

package proc

import (
	"os"
	"path/filepath"
)

// candidates lists the file names a bare command could resolve to.
//
// Exactly one, everywhere except Windows: the name as given. There is no
// extension convention, and appending one would look for files nobody has.
func candidates(name string) []string {
	return []string{name}
}

// isExecutableFile reports whether a path is a regular file with an execute
// bit set for somebody.
//
// Checking for any execute bit rather than the current user's is deliberate.
// Working out whether this process can run a file means resolving user and
// group membership, and getting that subtly wrong reports a tool as missing
// when it is there. Listing a file the user cannot run is the better failure:
// the error when they try says so plainly.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

// pathKey normalises a path for comparison. Case matters here, so cleaning is
// all that is wanted: /usr/bin/Go and /usr/bin/go are two different files.
func pathKey(path string) string {
	return filepath.Clean(path)
}

// lookupName is the name a shell would use to run a file, which everywhere
// except Windows is the file name itself.
func lookupName(name string) string { return name }
