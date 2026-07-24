//go:build windows

package fs

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// pathKey normalises a path for comparison. Windows filesystems are
// case-insensitive but case-preserving, so two paths differing only in case
// name the same file and must compare equal.
func pathKey(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

// isHidden reports whether an entry should be treated as hidden. Windows has a
// real hidden attribute; the leading dot is also honoured because
// cross-platform tools write dotfiles here too.
func isHidden(path, name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attributes, err := syscall.GetFileAttributes(pointer)
	if err != nil {
		return false
	}
	return attributes&syscall.FILE_ATTRIBUTE_HIDDEN != 0
}

// protectedReason names why a bulk operation must not run at this path.
//
// These are the directories where a mistyped path turns a routine command into
// an incident. None of them is a place anyone means to reorganise.
func protectedReason(path string) string {
	cleaned := filepath.Clean(path)
	key := pathKey(cleaned)

	if volume := filepath.VolumeName(cleaned); volume != "" {
		if key == pathKey(volume+string(filepath.Separator)) {
			return "it is the root of a drive"
		}
	}
	if cleaned == string(filepath.Separator) {
		return "it is a filesystem root"
	}

	if home, err := os.UserHomeDir(); err == nil && pathKey(home) == key {
		return "it is your home directory"
	}

	for _, variable := range []string{"SystemRoot", "ProgramFiles", "ProgramFiles(x86)", "ProgramData"} {
		directory := os.Getenv(variable)
		if directory != "" && pathKey(directory) == key {
			return "it is a system directory"
		}
	}

	for _, prefix := range []string{`c:\windows`, `c:\program files`, `c:\program files (x86)`} {
		if key == prefix {
			return "it is a system directory"
		}
	}

	return ""
}

// deviceID has no cheap answer on Windows.
//
// A volume serial number is available, but only by opening a handle to the
// file and calling GetFileInformationByHandle, which is a syscall per entry
// during a walk. Windows also mounts a second volume inside a directory far
// less often than Unix does. Callers are told the answer is unavailable and
// fall back to containment, which is the check that carries the weight in any
// case.
func deviceID(string) (uint64, bool) { return 0, false }
