package env

import (
	"context"
	"testing"
)

func TestInspectPathFlagsWhatIsWrong(t *testing.T) {
	machine := newFakeMachine().
		withPath("/usr/bin", true, true).
		withPath("/gone", false, false).
		withPath("/etc/hosts", true, false).
		withPath("/usr/bin", true, true)

	result, err := InspectPath(context.Background(), machine, PathRequest{})
	if err != nil {
		t.Fatalf("InspectPath: %v", err)
	}

	if len(result.Entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(result.Entries))
	}
	if result.Problems != 3 {
		t.Errorf("problems = %d, want 3", result.Problems)
	}

	assertProblem(t, result.Entries[1].Problems, ProblemMissing)
	assertProblem(t, result.Entries[2].Problems, ProblemNotDirectory)
	assertProblem(t, result.Entries[3].Problems, ProblemDuplicate)

	// Positions are one-based and in PATH order, because the order is the
	// answer: it is what decides which copy of a tool runs.
	if result.Entries[0].Position != 1 {
		t.Errorf("first position = %d, want 1", result.Entries[0].Position)
	}
}

// The shadow check is the expensive half, so it is off unless asked for.
func TestInspectPathFindsShadowsOnlyOnRequest(t *testing.T) {
	machine := newFakeMachine().
		withPath("/usr/local/bin", true, true).
		withPath("/usr/bin", true, true).
		withExecutables("/usr/local/bin", "python", "pip").
		withExecutables("/usr/bin", "python", "git")

	quiet, err := InspectPath(context.Background(), machine, PathRequest{})
	if err != nil {
		t.Fatalf("InspectPath: %v", err)
	}
	if len(quiet.Shadowed) != 0 {
		t.Errorf("shadowed = %v, want none without --shadows", quiet.Shadowed)
	}

	full, err := InspectPath(context.Background(), machine, PathRequest{Shadows: true})
	if err != nil {
		t.Fatalf("InspectPath: %v", err)
	}
	if len(full.Shadowed) != 1 {
		t.Fatalf("shadowed = %v, want only python", full.Shadowed)
	}

	shadow := full.Shadowed[0]
	if shadow.Name != "python" {
		t.Errorf("name = %q, want python", shadow.Name)
	}
	if shadow.Winner != "/usr/local/bin/python" {
		t.Errorf("winner = %q, want the earlier entry", shadow.Winner)
	}
	if len(shadow.Hidden) != 1 || shadow.Hidden[0] != "/usr/bin/python" {
		t.Errorf("hidden = %v, want the later copy", shadow.Hidden)
	}

	// The executable count comes from the same listing, so the directories
	// are read once rather than twice.
	if full.Entries[0].Executables != 2 {
		t.Errorf("executables = %d, want 2", full.Entries[0].Executables)
	}
}

// A tool with two copies is not a shadow when one of them is a different name.
func TestInspectPathIgnoresUnrelatedNames(t *testing.T) {
	machine := newFakeMachine().
		withPath("/a", true, true).
		withPath("/b", true, true).
		withExecutables("/a", "go").
		withExecutables("/b", "node")

	result, err := InspectPath(context.Background(), machine, PathRequest{Shadows: true})
	if err != nil {
		t.Fatalf("InspectPath: %v", err)
	}
	if len(result.Shadowed) != 0 {
		t.Errorf("shadowed = %v, want none", result.Shadowed)
	}
}

func TestWhichListsEveryCopyInOrder(t *testing.T) {
	machine := newFakeMachine().
		withTool("python", "/usr/local/bin/python", "Python 3.12.1").
		withTool("python", "/usr/bin/python", "Python 3.9.0")

	result, err := Which(context.Background(), machine, WhichRequest{
		Name:    "python",
		Version: true,
	})
	if err != nil {
		t.Fatalf("Which: %v", err)
	}

	if !result.Shadowed {
		t.Error("two copies should be reported as shadowed")
	}
	if result.Winner != "/usr/local/bin/python" {
		t.Errorf("winner = %q, want the first PATH match", result.Winner)
	}
	if len(result.Locations) != 2 {
		t.Fatalf("locations = %d, want 2", len(result.Locations))
	}
	if result.Locations[0].Version != "3.12.1" || result.Locations[1].Version != "3.9.0" {
		t.Errorf("versions = %q and %q, want each copy asked separately",
			result.Locations[0].Version, result.Locations[1].Version)
	}
}

// An unknown program is located, never invoked. Running an arbitrary
// executable with an invented flag is how a lookup turns into something that
// has side effects.
func TestWhichNeverRunsAnUnknownProgram(t *testing.T) {
	machine := newFakeMachine().withTool("housetool", "/opt/bin/housetool", "irrelevant")

	result, err := Which(context.Background(), machine, WhichRequest{
		Name:    "housetool",
		Version: true,
	})
	if err != nil {
		t.Fatalf("Which: %v", err)
	}
	if len(result.Locations) != 1 {
		t.Fatalf("locations = %d, want 1", len(result.Locations))
	}
	if len(machine.ran) != 0 {
		t.Errorf("ran %v, want nothing run", machine.ran)
	}
}

func TestWhichFindingNothingIsAResult(t *testing.T) {
	result, err := Which(context.Background(), newFakeMachine(), WhichRequest{Name: "absent"})
	if err != nil {
		t.Fatalf("Which: %v", err)
	}
	if len(result.Locations) != 0 || result.Winner != "" {
		t.Errorf("result = %+v, want nothing found", result)
	}
	if result.Locations == nil {
		t.Error("an empty listing must be an empty slice, never null in JSON")
	}
}

func TestWhichRejectsAnEmptyName(t *testing.T) {
	_, err := Which(context.Background(), newFakeMachine(), WhichRequest{Name: "  "})
	assertCode(t, err, "INVALID_INPUT")
}

func assertProblem(t *testing.T, problems []PathProblem, want PathProblem) {
	t.Helper()
	for _, problem := range problems {
		if problem == want {
			return
		}
	}
	t.Errorf("problems = %v, want it to include %q", problems, want)
}
