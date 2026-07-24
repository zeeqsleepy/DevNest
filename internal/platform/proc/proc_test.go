package proc

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// These tests start real processes and read a real PATH.
//
// The platform layer is the process seam, and a test of it against a fake
// tests the fake. Everything above this layer uses fakes and never starts
// anything; here it is the point. The programs used are the ones every
// supported platform ships, and nothing is written anywhere.

// shell returns a command that prints its argument and exits zero.
func shell(text string) Command {
	if runtime.GOOS == "windows" {
		return Command{Name: "cmd", Args: []string{"/c", "echo " + text}}
	}
	return Command{Name: "sh", Args: []string{"-c", "echo " + text}}
}

func TestRunCapturesOutput(t *testing.T) {
	output, err := System{}.Run(context.Background(), shell("hello"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(output.Stdout, "hello") {
		t.Errorf("stdout = %q, want it to contain hello", output.Stdout)
	}
	if output.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", output.ExitCode)
	}
	if output.Duration <= 0 {
		t.Error("duration was not measured")
	}
}

// A non-zero exit is a result, not an error. A tool answering "unrecognised
// flag" has answered, and the caller decides what that means.
func TestRunReportsANonZeroExitAsAResult(t *testing.T) {
	command := Command{Name: "sh", Args: []string{"-c", "exit 3"}}
	if runtime.GOOS == "windows" {
		command = Command{Name: "cmd", Args: []string{"/c", "exit 3"}}
	}

	output, err := System{}.Run(context.Background(), command)
	if err != nil {
		t.Fatalf("Run: %v, want a result rather than an error", err)
	}
	if output.ExitCode != 3 {
		t.Errorf("exit code = %d, want 3", output.ExitCode)
	}
}

// A probe that hangs is worse than one that fails: a hang has no message and
// no exit code, and the user is left looking at a cursor.
func TestRunIsBoundedByItsTimeout(t *testing.T) {
	command := Command{Name: "sh", Args: []string{"-c", "sleep 5"}, Timeout: 100 * time.Millisecond}
	if runtime.GOOS == "windows" {
		// ping rather than timeout: timeout refuses to run when stdin is not
		// a console, which is exactly the situation a test runs in.
		command = Command{
			Name:    "cmd",
			Args:    []string{"/c", "ping", "-n", "6", "127.0.0.1"},
			Timeout: 100 * time.Millisecond,
		}
	}

	started := time.Now()
	_, err := System{}.Run(context.Background(), command)
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("a command that outlives its timeout should report one")
	}
	if got := errors.CodeOf(err); got != errors.CodeTimeout {
		t.Errorf("code = %q, want %q", got, errors.CodeTimeout)
	}
	if elapsed > 3*time.Second {
		t.Errorf("took %s, want the timeout to have cut it short", elapsed)
	}
}

func TestRunRejectsAnEmptyCommand(t *testing.T) {
	_, err := System{}.Run(context.Background(), Command{})
	if got := errors.CodeOf(err); got != errors.CodeInvalidInput {
		t.Errorf("code = %q, want %q", got, errors.CodeInvalidInput)
	}
}

func TestRunReportsAMissingProgram(t *testing.T) {
	_, err := System{}.Run(context.Background(), Command{Name: "devnest-no-such-program"})
	if err == nil {
		t.Fatal("running a program that does not exist should fail")
	}
	if got := errors.CodeOf(err); got != errors.CodeNotFound && got != errors.CodeIO {
		t.Errorf("code = %q, want it classified", got)
	}
}

func TestCombinedPrefersStdout(t *testing.T) {
	both := Output{Stdout: " out \n", Stderr: "err"}
	if got := both.Combined(); got != "out" {
		t.Errorf("Combined = %q, want the trimmed stdout", got)
	}

	// Version flags are split between the two streams by different tools for
	// no reason anybody remembers, so an empty stdout falls back.
	stderrOnly := Output{Stderr: "openjdk version \"21\"\n"}
	if got := stderrOnly.Combined(); !strings.Contains(got, "openjdk") {
		t.Errorf("Combined = %q, want stderr when stdout is empty", got)
	}
}

func TestLookupFindsEveryCopyInPathOrder(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()

	name := "devnest-fake-tool"
	writeExecutable(t, first, name)
	writeExecutable(t, second, name)

	t.Setenv("PATH", strings.Join([]string{first, second}, string(os.PathListSeparator)))

	found := System{}.Lookup(name)
	if len(found) != 2 {
		t.Fatalf("found %v, want both copies", found)
	}
	if filepath.Dir(found[0]) != first {
		t.Errorf("first match is in %q, want the earlier PATH entry", filepath.Dir(found[0]))
	}
}

func TestLookupReportsNothingForAMissingTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	system := System{}
	if found := system.Lookup("devnest-absent-tool"); len(found) != 0 {
		t.Errorf("found %v, want nothing", found)
	}
	if found := system.Lookup("   "); len(found) != 0 {
		t.Errorf("found %v for an empty name, want nothing", found)
	}
}

// An empty PATH entry means the current directory on most platforms, which is
// a security problem rather than a feature: it lets a file in whatever
// directory you happen to be in shadow a real tool.
func TestPathEntriesDropsEmptyEntries(t *testing.T) {
	separator := string(os.PathListSeparator)
	t.Setenv("PATH", separator+"/usr/bin"+separator+separator+"/bin"+separator)

	entries := System{}.PathEntries()
	if len(entries) != 2 {
		t.Fatalf("entries = %v, want the two real ones", entries)
	}
}

func TestStatDistinguishesMissingFromNotADirectory(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	entry, err := System{}.Stat(directory)
	if err != nil || !entry.Exists || !entry.IsDir {
		t.Errorf("stat of a directory = %+v, %v", entry, err)
	}

	entry, err = System{}.Stat(file)
	if err != nil || !entry.Exists || entry.IsDir {
		t.Errorf("stat of a file = %+v, %v", entry, err)
	}

	entry, err = System{}.Stat(filepath.Join(directory, "absent"))
	if err != nil {
		t.Errorf("stat of a missing path returned an error: %v", err)
	}
	if entry.Exists {
		t.Error("a missing path was reported as existing")
	}
}

func TestExecutablesListsRunnableFiles(t *testing.T) {
	directory := t.TempDir()
	writeExecutable(t, directory, "devnest-fake-tool")

	found := System{}.Executables(directory)
	if len(found) != 1 {
		t.Fatalf("found %v, want the one executable", found)
	}
	// The name is what a shell would type, which on Windows means the
	// extension is gone: go.exe and go.cmd are two copies of "go".
	if strings.Contains(found[0].Name, ".") {
		t.Errorf("name = %q, want the lookup name", found[0].Name)
	}

	system := System{}
	if unreadable := system.Executables(filepath.Join(directory, "absent")); unreadable != nil {
		t.Errorf("an unreadable directory returned %v, want nothing", unreadable)
	}
}

// writeExecutable creates a file the platform considers runnable.
func writeExecutable(t *testing.T, directory, name string) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(directory, name)

	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}
