package clean

import (
	"context"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

// Every test in this file is about what does not get deleted. The success path
// is one test; the rest are the guards, each asserting that the fake recorded
// no removal at all.

func apply(t *testing.T, system *fakeFS, request ApplyRequest) ApplyResult {
	t.Helper()
	if request.Root == "" {
		request.Root = root()
	}
	request.Confirmed = true

	result, err := Apply(context.Background(), system, request)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return result
}

func TestApplyRemovesTheCandidatesAndReportsWhatItFreed(t *testing.T) {
	system := project()

	result := apply(t, system, ApplyRequest{})

	if result.Count != 3 {
		t.Fatalf("count = %d, want three directories removed", result.Count)
	}
	if len(system.removed) != 3 {
		t.Fatalf("removed = %v, want three", system.removed)
	}
	if result.BytesFreed != 900+5000+3000+700 {
		t.Errorf("bytesFreed = %d, want everything inside them", result.BytesFreed)
	}
	if result.FilesFreed != 4 {
		t.Errorf("filesFreed = %d, want 4", result.FilesFreed)
	}
}

// The prompt belongs to the interface layer, but the module refuses to act on
// an unconfirmed request rather than assuming the caller remembered to ask.
func TestApplyRefusesWithoutConfirmation(t *testing.T) {
	system := project()

	_, err := Apply(context.Background(), system, ApplyRequest{
		ScanRequest: ScanRequest{Root: root()},
	})
	assertCode(t, err, errors.CodeInvalidInput)

	if len(system.removed) != 0 {
		t.Fatalf("removed %v without confirmation", system.removed)
	}
}

func TestApplyRefusesAProtectedRootWithoutForce(t *testing.T) {
	system := project()
	system.protected[root()] = "it is a filesystem root"

	_, err := Apply(context.Background(), system, ApplyRequest{
		ScanRequest: ScanRequest{Root: root()},
		Confirmed:   true,
	})
	assertCode(t, err, errors.CodeInvalidInput)

	if len(system.removed) != 0 {
		t.Fatalf("removed %v from a protected root", system.removed)
	}
}

func TestApplyLeavesProtectedPathsAlone(t *testing.T) {
	system := project()

	result := apply(t, system, ApplyRequest{
		ScanRequest: ScanRequest{Protect: []string{root("node_modules")}},
	})

	for _, removed := range system.removed {
		if strings.Contains(removed, "node_modules") {
			t.Fatalf("removed %q, which was protected", removed)
		}
	}
	if len(result.Skipped) == 0 {
		t.Error("the protected path was not reported as skipped")
	}
}

func TestApplyDoesNotCrossAFilesystemBoundary(t *testing.T) {
	system := project()
	system.devices[root("node_modules")] = 77

	apply(t, system, ApplyRequest{})

	for _, removed := range system.removed {
		if strings.Contains(removed, "node_modules") {
			t.Fatalf("removed %q, which is on another filesystem", removed)
		}
	}
}

func TestApplyNeverRemovesASymlink(t *testing.T) {
	system := newFakeFS().with("package.json", 10).withSymlink("dist")

	apply(t, system, ApplyRequest{})

	if len(system.removed) != 0 {
		t.Fatalf("removed %v, and one of them is a symbolic link", system.removed)
	}
}

// The tree can change between the scan and the removal. Every guard runs again
// against the directory as it is in the moment before it is deleted.
func TestApplyRechecksEachCandidateImmediatelyBeforeRemovingIt(t *testing.T) {
	system := project()

	// The directory turns into a symlink after the scan has seen it, which is
	// the shape of the race this recheck exists for. The scan itself already
	// ran against the tree as it was, so this is the second look catching it.
	system.symlinks[root("node_modules")] = true

	result := apply(t, system, ApplyRequest{})

	for _, removed := range system.removed {
		if strings.Contains(removed, "node_modules") {
			t.Fatalf("removed %q after it became a symbolic link", removed)
		}
	}

	reasons := strings.Join(skipReasons(result.Skipped), " | ")
	if !strings.Contains(reasons, "symbolic link") {
		t.Errorf("skipped = %s, want the changed candidate explained", reasons)
	}
}

// One directory that cannot be removed, usually because a process is holding a
// file open, does not stop the rest. Stopping halfway leaves a tree nobody can
// reason about.
func TestApplyContinuesPastAFailureAndReportsIt(t *testing.T) {
	system := project()
	system.failRemove[root("node_modules")] = errors.New(errors.CodeIO,
		"the directory is in use by another process")

	result := apply(t, system, ApplyRequest{})

	if len(result.Failed) != 1 {
		t.Fatalf("failed = %+v, want the locked directory recorded", result.Failed)
	}
	if !strings.Contains(result.Failed[0].Reason, "in use") {
		t.Errorf("reason = %q, want the operating system's explanation", result.Failed[0].Reason)
	}
	if result.Count != 2 {
		t.Errorf("count = %d, want the other two removed anyway", result.Count)
	}
	if result.BytesFreed != 3000+700 {
		t.Errorf("bytesFreed = %d, want only what was actually removed", result.BytesFreed)
	}
}

func TestApplyStopsWhenCancelled(t *testing.T) {
	system := project()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Apply(ctx, system, ApplyRequest{
		ScanRequest: ScanRequest{Root: root()},
		Confirmed:   true,
	})
	assertCode(t, err, errors.CodeCancelled)

	if len(system.removed) != 0 {
		t.Fatalf("removed %v after being cancelled", system.removed)
	}
}

func TestApplyNarrowsToSelectedPatterns(t *testing.T) {
	system := project()

	apply(t, system, ApplyRequest{ScanRequest: ScanRequest{Patterns: []string{"dist"}}})

	if len(system.removed) != 1 || !strings.HasSuffix(system.removed[0], "dist") {
		t.Fatalf("removed = %v, want only the selected pattern", system.removed)
	}
}
