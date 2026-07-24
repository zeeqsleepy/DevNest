package env

import (
	"context"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

// Every toolchain announces itself differently, and this is the one piece of
// logic in the module that has to cope with all of them.
func TestVersionReadsWhatToolsActuallyPrint(t *testing.T) {
	tests := map[string]string{
		"go version go1.25.0 windows/amd64":                   "1.25.0",
		"v22.1.0":                                             "22.1.0",
		"Python 3.12.1":                                       "3.12.1",
		"git version 2.44.0.windows.1":                        "2.44.0",
		"cargo 1.77.2 (e52e36006 2024-03-26)":                 "1.77.2",
		"Docker version 25.0.3, build 4debf41":                "25.0.3",
		"openjdk version \"21.0.1\" 2023-10-17":               "21.0.1",
		"Client Version: v1.29.0":                             "1.29.0",
		"GNU Make 4.4.1\nBuilt for x86_64-pc-linux-gnu":       "4.4.1",
		"Terraform v1.7.5\non windows_amd64":                  "1.7.5",
		"npm\n10.5.0":                                         "",
		"deno 1.41.3 (release, x86_64-pc-windows-msvc)":       "1.41.3",
		"PHP 8.3.4 (cli) (built: Mar 12 2024 21:32:23) (ZTS)": "8.3.4",
		"rustc 1.77.2 (25ef9e3d8 2024-04-09)":                 "1.77.2",
		"":                                                    "",
		"unrecognisable output with no numbers at all":        "",
		"amd64 only, no version here":                         "",
	}

	for output, want := range tests {
		if got := version(output); got != want {
			t.Errorf("version(%q) = %q, want %q", output, got, want)
		}
	}
}

// A tool that is not on PATH is never started. On a typical machine that skips
// most of the table without creating a single process, which is what keeps the
// command inside its budget.
func TestListNeverRunsWhatIsNotInstalled(t *testing.T) {
	machine := newFakeMachine().withTool("go", "/usr/bin/go", "go version go1.25.0 linux/amd64")

	result, err := List(context.Background(), machine, ListRequest{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if result.Found != 1 {
		t.Errorf("found = %d, want 1", result.Found)
	}
	if len(machine.ran) != 1 || machine.ran[0] != "/usr/bin/go" {
		t.Errorf("ran %v, want only the installed tool", machine.ran)
	}
	requireVersion(t, result.Tools, "go", "1.25.0")
}

// Missing tools are left out by default and kept on request, because "what is
// missing on this build agent" is a different question with the same shape.
func TestListIncludesMissingToolsOnRequest(t *testing.T) {
	machine := newFakeMachine().withTool("go", "/usr/bin/go", "go version go1.25.0 linux/amd64")

	quiet, err := List(context.Background(), machine, ListRequest{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(quiet.Tools) != 1 {
		t.Errorf("tools = %d, want only what was found", len(quiet.Tools))
	}

	full, err := List(context.Background(), machine, ListRequest{IncludeMissing: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(full.Tools) != quiet.Missing+quiet.Found {
		t.Errorf("tools = %d, want every entry in the table", len(full.Tools))
	}
	if node, found := findTool(full.Tools, "node"); !found || node.Found {
		t.Error("an absent tool should be listed as not found")
	}
}

// A tool that is installed and will not say what it is beats knowing nothing,
// so the probe failure is recorded and the run continues.
func TestListReportsAProbeThatFailed(t *testing.T) {
	machine := newFakeMachine().withTool("go", "/usr/bin/go", "")
	machine.failures["/usr/bin/go"] = errors.New(errors.CodeTimeout, "go did not answer within 2s")

	result, err := List(context.Background(), machine, ListRequest{})
	if err != nil {
		t.Fatalf("List: %v; a tool that will not answer is not a failed command", err)
	}

	tool, found := findTool(result.Tools, "go")
	if !found || !tool.Found {
		t.Fatal("the tool should still be reported as installed")
	}
	if tool.Version != "" || tool.Detail == "" {
		t.Errorf("tool = %+v, want no version and a reason", tool)
	}
}

// A name the table has never heard of is still located, and never run:
// inventing a version flag for an unknown program is how a listing turns into
// something with side effects.
func TestListLocatesUnknownToolsWithoutRunningThem(t *testing.T) {
	machine := newFakeMachine().withTool("housetool", "/opt/bin/housetool", "irrelevant")

	result, err := List(context.Background(), machine, ListRequest{Only: []string{"housetool"}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	tool, found := findTool(result.Tools, "housetool")
	if !found || !tool.Found {
		t.Fatal("the tool was not located")
	}
	if len(machine.ran) != 0 {
		t.Errorf("ran %v, want nothing run for an undescribed tool", machine.ran)
	}
}

// A tool resolvable from more than one place lists the copies that lose. This
// is the finding that explains most reports of an unexpected version.
func TestListReportsShadowedCopies(t *testing.T) {
	machine := newFakeMachine().
		withTool("python", "/usr/local/bin/python", "Python 3.12.1").
		withTool("python", "/usr/bin/python", "Python 3.9.0")

	result, err := List(context.Background(), machine, ListRequest{Only: []string{"python"}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	tool, _ := findTool(result.Tools, "python")
	if tool.Path != "/usr/local/bin/python" {
		t.Errorf("path = %q, want the first PATH match", tool.Path)
	}
	if len(tool.Shadowed) != 1 || tool.Shadowed[0] != "/usr/bin/python" {
		t.Errorf("shadowed = %v, want the second copy", tool.Shadowed)
	}
	// Only the copy that runs is probed. Running every copy of every tool
	// turns a summary into a minute of process creation.
	if len(machine.ran) != 1 {
		t.Errorf("ran %v, want only the winner probed", machine.ran)
	}
}

func TestSummarizeDescribesTheMachine(t *testing.T) {
	machine := newFakeMachine().
		withTool("go", "/usr/bin/go", "go version go1.25.0 linux/amd64").
		withPath("/usr/bin", true, true).
		withPath("/nowhere", false, false)

	result, err := Summarize(context.Background(), machine, SummaryRequest{})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	if result.Machine.OS != "linux" || result.Machine.CPUs != 8 {
		t.Errorf("machine = %+v, want what the platform layer reported", result.Machine)
	}
	if result.PathEntries != 2 {
		t.Errorf("path entries = %d, want 2", result.PathEntries)
	}
	if result.PathProblems != 1 {
		t.Errorf("path problems = %d, want the missing directory", result.PathProblems)
	}
	if result.Found != 1 {
		t.Errorf("tools found = %d, want 1", result.Found)
	}
}
