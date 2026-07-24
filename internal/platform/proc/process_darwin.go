//go:build darwin

package proc

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// nameTimeout bounds the lookup. It is a subprocess per process asked about,
// so it has to be quick or not at all.
const nameTimeout = 2 * time.Second

// processName asks ps for the executable name.
//
// macOS publishes this through libproc, which needs cgo, and DevNest builds
// without it so that releases stay static and cross-compilable. ps is on every
// macOS installation and its -o comm= output is one field with no header.
//
// The command name is asked for rather than the full command line, which could
// carry a credential passed as an argument into output that gets exported.
func processName(pid int) string {
	ctx, cancel := context.WithTimeout(context.Background(), nameTimeout)
	defer cancel()

	path, err := exec.LookPath("ps")
	if err != nil {
		return ""
	}

	command := exec.CommandContext(ctx, path, "-o", "comm=", "-p", strconv.Itoa(pid))
	var stdout bytes.Buffer
	command.Stdout = &stdout

	if err := command.Run(); err != nil {
		return ""
	}

	name := strings.TrimSpace(stdout.String())
	if name == "" {
		return ""
	}

	// ps prints the executable path; the last element is the name, which is
	// what every listing shows.
	if index := strings.LastIndex(name, "/"); index >= 0 && index+1 < len(name) {
		name = name[index+1:]
	}
	return name
}
