//go:build windows

package proc

import (
	"os"
	"path/filepath"
	"strings"
)

// candidates lists the file names a bare command could resolve to.
//
// Windows has no executable bit. What makes a file runnable is its extension
// being listed in PATHEXT, so "go" on PATH means go.exe, go.cmd, go.bat, or
// whichever of those exists first. A lookup that only tried the bare name
// would find almost nothing.
//
// The bare name is tried first, because a name given with its extension
// already ("go.exe") must not become "go.exe.exe".
func candidates(name string) []string {
	if hasKnownExtension(name) {
		return []string{name}
	}

	extensions := pathExtensions()
	found := make([]string, 0, len(extensions)+1)
	for _, extension := range extensions {
		found = append(found, name+extension)
	}
	return found
}

// pathExtensions reads PATHEXT, falling back to the standard set when it is
// absent or empty.
func pathExtensions() []string {
	raw := os.Getenv("PATHEXT")
	if strings.TrimSpace(raw) == "" {
		raw = ".COM;.EXE;.BAT;.CMD"
	}

	var extensions []string
	for _, extension := range strings.Split(raw, ";") {
		trimmed := strings.ToLower(strings.TrimSpace(extension))
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, ".") {
			trimmed = "." + trimmed
		}
		extensions = append(extensions, trimmed)
	}
	return extensions
}

func hasKnownExtension(name string) bool {
	extension := strings.ToLower(filepath.Ext(name))
	if extension == "" {
		return false
	}
	for _, known := range pathExtensions() {
		if extension == known {
			return true
		}
	}
	return false
}

// isExecutableFile reports whether a path is a file that could be run.
//
// The extension has already decided that by the time this is called, so what
// is left is whether the file exists and is not a directory.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// pathKey normalises a path for comparison. Windows filesystems are
// case-insensitive but case-preserving, so two spellings of one executable
// have to compare equal or every lookup reports duplicates that are not there.
func pathKey(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

// lookupName is the name a shell would use to run a file.
//
// The extension is dropped when it is one PATHEXT knows, because that is what
// makes the file runnable by its stem in the first place. A file called
// "notes.txt" keeps its name: nobody runs it either way.
func lookupName(name string) string {
	if hasKnownExtension(name) {
		return strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	}
	return strings.ToLower(name)
}
