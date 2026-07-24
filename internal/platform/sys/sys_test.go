package sys

import (
	"runtime"
	"strings"
	"testing"
)

// These tests read the real environment, because that is what this package is
// for. Nothing here writes anything, and t.Setenv puts back what it changed.

func TestDescribeReportsTheMachine(t *testing.T) {
	info := System{}.Describe()

	if info.OS != runtime.GOOS || info.Architecture != runtime.GOARCH {
		t.Errorf("os/arch = %q/%q, want %q/%q",
			info.OS, info.Architecture, runtime.GOOS, runtime.GOARCH)
	}
	if info.CPUs < 1 {
		t.Errorf("cpus = %d, want at least one", info.CPUs)
	}
	if !strings.HasPrefix(info.GoVersion, "go") {
		t.Errorf("go version = %q, want it to start with go", info.GoVersion)
	}
}

// Anything unavailable is an empty string rather than an error. A machine with
// no hostname is unusual and not a reason to refuse to describe the rest.
func TestDescribeDoesNotFailOnAMissingAnswer(t *testing.T) {
	t.Setenv("SHELL", "")
	t.Setenv("PSModulePath", "")
	t.Setenv("ComSpec", "")

	info := System{}.Describe()
	if info.OS == "" {
		t.Error("the operating system should always be known")
	}
}

func TestShellReadsTheConventionalVariables(t *testing.T) {
	t.Setenv("PSModulePath", "")
	t.Setenv("ComSpec", "")
	t.Setenv("SHELL", "/usr/bin/zsh")

	system := System{}
	if got := system.Shell(); got != "zsh" {
		t.Errorf("shell = %q, want zsh", got)
	}

	t.Setenv("SHELL", "")
	t.Setenv("PSModulePath", `C:\Program Files\PowerShell\Modules`)
	if got := system.Shell(); got != "powershell" {
		t.Errorf("shell = %q, want powershell", got)
	}
}

func TestEnvironReturnsAMap(t *testing.T) {
	t.Setenv("DEVNEST_TEST_VARIABLE", "present")

	values := System{}.Environ()
	if values["DEVNEST_TEST_VARIABLE"] != "present" {
		t.Errorf("value = %q, want the one just set", values["DEVNEST_TEST_VARIABLE"])
	}
	if len(values) == 0 {
		t.Error("the environment came back empty")
	}

	value, found := System{}.Lookup("DEVNEST_TEST_VARIABLE")
	if !found || value != "present" {
		t.Errorf("Lookup = %q, %v", value, found)
	}
}

func TestTerminalPrefersTheMostSpecificSignal(t *testing.T) {
	t.Setenv("WT_SESSION", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("TERM", "xterm-256color")

	system := System{}
	if got := system.Terminal(); got != "xterm-256color" {
		t.Errorf("terminal = %q, want the TERM value", got)
	}

	t.Setenv("WT_SESSION", "abc-123")
	if got := system.Terminal(); got != "windows-terminal" {
		t.Errorf("terminal = %q, want windows-terminal", got)
	}
}
