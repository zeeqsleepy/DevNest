package file

import (
	"context"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func renameFixture() *fakeFS {
	return newFakeFS().
		addFile(root("IMG_0001.jpg"), "a").
		addFile(root("IMG_0002.jpg"), "b").
		addFile(root("notes.txt"), "c")
}

func renameRequest() RenameRequest {
	return RenameRequest{Selection: Selection{Root: root()}}
}

func destinationsOf(result RenameResult) map[string]string {
	names := make(map[string]string, len(result.Renames))
	for _, rename := range result.Renames {
		names[rename.OldName] = rename.NewName
	}
	return names
}

func TestRenameAppliesRulesInAFixedOrder(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*RenameRequest)
		oldName string
		want    string
	}{
		{
			name:    "prefix",
			mutate:  func(r *RenameRequest) { r.Prefix = "holiday-" },
			oldName: "notes.txt",
			want:    "holiday-notes.txt",
		},
		{
			name:    "suffix goes before the extension",
			mutate:  func(r *RenameRequest) { r.Suffix = "-final" },
			oldName: "notes.txt",
			want:    "notes-final.txt",
		},
		{
			name:    "replace",
			mutate:  func(r *RenameRequest) { r.Replace = []Replacement{{From: "IMG_", To: "photo-"}} },
			oldName: "IMG_0001.jpg",
			want:    "photo-0001.jpg",
		},
		{
			name:    "replace with nothing deletes",
			mutate:  func(r *RenameRequest) { r.Replace = []Replacement{{From: "IMG_", To: ""}} },
			oldName: "IMG_0001.jpg",
			want:    "0001.jpg",
		},
		{
			name:    "lowercase",
			mutate:  func(r *RenameRequest) { r.Lowercase = true },
			oldName: "IMG_0001.jpg",
			want:    "img_0001.jpg",
		},
		{
			name:    "uppercase",
			mutate:  func(r *RenameRequest) { r.Uppercase = true },
			oldName: "notes.txt",
			want:    "NOTES.txt",
		},
		{
			name: "replacement runs before the prefix",
			mutate: func(r *RenameRequest) {
				r.Replace = []Replacement{{From: "IMG_", To: ""}}
				r.Prefix = "photo-"
			},
			oldName: "IMG_0001.jpg",
			want:    "photo-0001.jpg",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := renameRequest()
			test.mutate(&request)

			result, err := RenameFiles(context.Background(), renameFixture(), request)
			if err != nil {
				t.Fatalf("RenameFiles: %v", err)
			}
			if got := destinationsOf(result)[test.oldName]; got != test.want {
				t.Errorf("%s -> %q, want %q", test.oldName, got, test.want)
			}
		})
	}
}

func TestRenameSequence(t *testing.T) {
	system := newFakeFS().
		addFile(root("b.txt"), "b").
		addFile(root("a.txt"), "a").
		addFile(root("c.txt"), "c")

	request := renameRequest()
	request.Sequence = Sequence{
		Enabled: true, Start: 1, Padding: 3,
		Separator: "-", Position: SequenceBefore,
	}
	request.Prefix = "trip"

	result, err := RenameFiles(context.Background(), system, request)
	if err != nil {
		t.Fatalf("RenameFiles: %v", err)
	}

	names := destinationsOf(result)
	// Numbers are assigned in path order, so a preview and the run that
	// applies it produce the same names.
	want := map[string]string{
		"a.txt": "trip001-a.txt",
		"b.txt": "trip002-b.txt",
		"c.txt": "trip003-c.txt",
	}
	for oldName, wanted := range want {
		if names[oldName] != wanted {
			t.Errorf("%s -> %q, want %q", oldName, names[oldName], wanted)
		}
	}
}

func TestRenameSequenceAsSuffix(t *testing.T) {
	system := newFakeFS().addFile(root("photo.jpg"), "a")

	request := renameRequest()
	request.Sequence = Sequence{Enabled: true, Start: 7, Padding: 2, Separator: "_", Position: SequenceAfter}

	result, err := RenameFiles(context.Background(), system, request)
	if err != nil {
		t.Fatalf("RenameFiles: %v", err)
	}
	if got := destinationsOf(result)["photo.jpg"]; got != "photo_07.jpg" {
		t.Errorf("photo.jpg -> %q, want photo_07.jpg", got)
	}
}

// The preview is the default. Nothing may be renamed without Apply.
func TestRenamePreviewChangesNothing(t *testing.T) {
	system := renameFixture()

	request := renameRequest()
	request.Prefix = "x-"

	result, err := RenameFiles(context.Background(), system, request)
	if err != nil {
		t.Fatalf("RenameFiles: %v", err)
	}
	if result.Applied {
		t.Error("Applied = true for a preview")
	}
	if len(system.moved) != 0 {
		t.Errorf("renamed %v during a preview", system.moved)
	}
	if !system.has(root("notes.txt")) {
		t.Error("the original file is gone")
	}
}

func TestRenameApplies(t *testing.T) {
	system := renameFixture()

	request := renameRequest()
	request.Prefix = "x-"
	request.Apply = true

	result, err := RenameFiles(context.Background(), system, request)
	if err != nil {
		t.Fatalf("RenameFiles: %v", err)
	}
	if result.Renamed != 3 {
		t.Errorf("Renamed = %d, want 3", result.Renamed)
	}
	if !system.has(root("x-notes.txt")) {
		t.Errorf("the renamed file is missing, tree is %v", system.paths())
	}
}

// Two files heading for one name must abort the whole batch, with nothing
// changed. A half-renamed directory is worse than a refusal.
func TestRenameRefusesCollidingDestinations(t *testing.T) {
	system := newFakeFS().
		addFile(root("a.txt"), "a").
		addFile(root("b.txt"), "b")

	request := renameRequest()
	request.Replace = []Replacement{{From: "a", To: "same"}, {From: "b", To: "same"}}
	request.Apply = true

	_, err := RenameFiles(context.Background(), system, request)
	assertCode(t, err, errors.CodeConflict)

	if len(system.moved) != 0 {
		t.Errorf("renamed %v despite the conflict", system.moved)
	}
	if !strings.Contains(err.Error(), "same name") {
		t.Errorf("error = %q, want it to explain the collision", err.Error())
	}
}

func TestRenameRefusesAnExistingName(t *testing.T) {
	system := newFakeFS().
		addFile(root("draft.txt"), "a").
		addFile(root("final.txt"), "b")

	request := renameRequest()
	request.Match = "draft.txt"
	request.Replace = []Replacement{{From: "draft", To: "final"}}
	request.Apply = true

	_, err := RenameFiles(context.Background(), system, request)
	assertCode(t, err, errors.CodeConflict)
	if system.contentOf(root("final.txt")) != "b" {
		t.Error("the existing file was replaced")
	}
}

// A file being renamed away is not an obstacle to the file taking its name, so
// shifting a whole batch by one is allowed.
func TestRenameAllowsNamesFreedByTheSameBatch(t *testing.T) {
	system := newFakeFS().
		addFile(root("a.txt"), "a").
		addFile(root("b.txt"), "b")

	request := renameRequest()
	request.Suffix = "-v2"
	request.Apply = true

	result, err := RenameFiles(context.Background(), system, request)
	if err != nil {
		t.Fatalf("RenameFiles: %v", err)
	}
	if result.Renamed != 2 {
		t.Errorf("Renamed = %d, want 2", result.Renamed)
	}
}

func TestRenameMatchSelectsASubset(t *testing.T) {
	system := renameFixture()

	request := renameRequest()
	request.Match = "*.jpg"
	request.Prefix = "photo-"
	request.Apply = true

	result, err := RenameFiles(context.Background(), system, request)
	if err != nil {
		t.Fatalf("RenameFiles: %v", err)
	}
	if result.Renamed != 2 {
		t.Errorf("Renamed = %d, want only the two jpg files", result.Renamed)
	}
	if !system.has(root("notes.txt")) {
		t.Error("a file outside the match was renamed")
	}
}

func TestRenameReportsUnchangedNames(t *testing.T) {
	system := newFakeFS().addFile(root("photo.jpg"), "a")

	request := renameRequest()
	request.Lowercase = true
	request.Apply = true

	result, err := RenameFiles(context.Background(), system, request)
	if err != nil {
		t.Fatalf("RenameFiles: %v", err)
	}
	if result.Unchanged != 1 {
		t.Errorf("Unchanged = %d, want 1", result.Unchanged)
	}
	if len(system.moved) != 0 {
		t.Error("a file whose name was already correct was still moved")
	}
}

func TestRenameRequiresARule(t *testing.T) {
	_, err := RenameFiles(context.Background(), renameFixture(), renameRequest())
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestRenameRejectsContradictoryCaseRules(t *testing.T) {
	request := renameRequest()
	request.Lowercase = true
	request.Uppercase = true

	_, err := RenameFiles(context.Background(), renameFixture(), request)
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestRenameRefusesProtectedDirectory(t *testing.T) {
	system := renameFixture()
	system.protected[root()] = "it is the filesystem root"

	request := renameRequest()
	request.Prefix = "x-"

	_, err := RenameFiles(context.Background(), system, request)
	assertCode(t, err, errors.CodeInvalidInput)
}

// The result is the rollback record: every old and new name, whether or not
// the batch was applied.
func TestRenameResultCarriesTheRollbackMapping(t *testing.T) {
	system := renameFixture()

	request := renameRequest()
	request.Prefix = "x-"
	request.Apply = true

	result, err := RenameFiles(context.Background(), system, request)
	if err != nil {
		t.Fatalf("RenameFiles: %v", err)
	}

	for _, rename := range result.Renames {
		if rename.Source == "" || rename.Destination == "" {
			t.Errorf("incomplete record: %+v", rename)
		}
		if rename.Status == RenameDone && rename.Source == rename.Destination {
			t.Errorf("a renamed file records the same path twice: %+v", rename)
		}
	}
}
