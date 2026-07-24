//go:build linux

package proc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// processName reads the name Linux publishes for a process.
//
// /proc/<pid>/comm is the short name and is readable for any process on the
// machine. The command line would be more informative and is deliberately not
// used: it can contain a password passed as an argument, and this name is
// printed, exported, and pasted into tickets.
func processName(pid int) string {
	path := filepath.Join("/proc", strconv.Itoa(pid), "comm")

	contents, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(contents))
}
