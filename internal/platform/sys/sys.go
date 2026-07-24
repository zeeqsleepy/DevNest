// Package sys is the operating-system half of the platform layer: what
// machine this is, who is running, and which shell they are in.
//
// Everything here is a question with a fixed answer for the life of the
// process, and every answer is cheap. Nothing in this package runs a program
// or opens a file, which is why the environment summary is fast enough to be
// the first thing anybody types.
package sys

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Info describes the machine and the session.
type Info struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	CPUs         int    `json:"cpus"`
	GoVersion    string `json:"goVersion"`
	Hostname     string `json:"hostname"`
	Home         string `json:"home"`
	Shell        string `json:"shell"`
	Terminal     string `json:"terminal"`
}

// System answers questions about the machine. Its zero value is ready to use.
type System struct{}

// Describe reports what is known about the machine and the session.
//
// Anything unavailable is reported as an empty string rather than as an error.
// A machine with no hostname is unusual and not a reason to refuse to describe
// the rest of it.
func (s System) Describe() Info {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return Info{
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		CPUs:         runtime.NumCPU(),
		GoVersion:    runtime.Version(),
		Hostname:     hostname,
		Home:         home,
		Shell:        s.Shell(),
		Terminal:     s.Terminal(),
	}
}

// Shell names the shell this process was started from, as far as the
// environment says.
//
// There is no reliable way to ask. A process knows its parent's identity only
// through what the parent chose to put in the environment, and every shell
// advertises itself differently. This reads the conventional variables and
// reports what it finds; an empty answer means the environment did not say,
// which is different from there being no shell.
func (s System) Shell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return filepath.Base(shell)
	}

	// PowerShell exports its module path whether or not it is the login
	// shell, so it is evidence rather than proof; it is checked before
	// ComSpec, which is set on Windows regardless of what is running.
	if os.Getenv("PSModulePath") != "" {
		return "powershell"
	}
	if shell := os.Getenv("ComSpec"); shell != "" {
		return strings.TrimSuffix(filepath.Base(shell), filepath.Ext(shell))
	}
	return ""
}

// Terminal names the terminal program, when it says so.
func (s System) Terminal() string {
	for _, name := range []string{"WT_SESSION", "TERM_PROGRAM", "TERM"} {
		if value := os.Getenv(name); value != "" {
			if name == "WT_SESSION" {
				return "windows-terminal"
			}
			return value
		}
	}
	return ""
}

// Environ returns the whole environment as a map.
//
// A map rather than the usual slice of "key=value": every caller of this in
// DevNest is looking things up or filtering, and doing that against a slice
// means every caller writing the same split.
func (s System) Environ() map[string]string {
	pairs := os.Environ()
	values := make(map[string]string, len(pairs))

	for _, pair := range pairs {
		name, value, found := strings.Cut(pair, "=")
		if found && name != "" {
			values[name] = value
		}
	}
	return values
}

// Lookup reads one environment variable.
func (s System) Lookup(name string) (string, bool) {
	return os.LookupEnv(name)
}
