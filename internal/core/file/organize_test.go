package file

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func organizeFixture() *fakeFS {
	return newFakeFS().
		addFile(root("photo.jpg"), "a").
		addFile(root("scan.png"), "bb").
		addFile(root("manual.pdf"), "ccc").
		addFile(root("notes.txt"), "dddd").
		addFile(root("clip.mp4"), "eeeee").
		addFile(root("README"), "f")
}

func organizeRequest(apply bool) OrganizeRequest {
	return OrganizeRequest{
		Selection: Selection{Root: root()},
		Grouping:  GroupByCategory,
		Apply:     apply,
	}
}

func TestOrganizePlansCategoryFolders(t *testing.T) {
	system := organizeFixture()

	result, err := Organize(context.Background(), system, organizeRequest(false))
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}

	want := map[string]string{
		root("photo.jpg"):  root("Images", "jpg", "photo.jpg"),
		root("scan.png"):   root("Images", "png", "scan.png"),
		root("manual.pdf"): root("Documents", "pdf", "manual.pdf"),
		root("notes.txt"):  root("Documents", "txt", "notes.txt"),
		root("clip.mp4"):   root("Videos", "mp4", "clip.mp4"),
		root("README"):     root("Other", "no-extension", "README"),
	}

	if len(result.Moves) != len(want) {
		t.Fatalf("planned %d moves, want %d", len(result.Moves), len(want))
	}
	for _, move := range result.Moves {
		if move.Destination != want[move.Source] {
			t.Errorf("%s -> %s, want %s", move.Source, move.Destination, want[move.Source])
		}
		if move.Status != MovePlanned {
			t.Errorf("%s status = %q, want %q", move.Source, move.Status, MovePlanned)
		}
	}
}

// The default is a dry run. Nothing may move until the caller asks for it.
func TestOrganizeDryRunChangesNothing(t *testing.T) {
	system := organizeFixture()
	before := system.paths()

	result, err := Organize(context.Background(), system, organizeRequest(false))
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}

	if result.Applied {
		t.Error("Applied = true for a dry run")
	}
	if len(system.moved) != 0 {
		t.Errorf("moved %v during a dry run", system.moved)
	}
	if got := system.paths(); len(got) != len(before) {
		t.Errorf("the tree changed during a dry run: %v", got)
	}
}

func TestOrganizeAppliesMoves(t *testing.T) {
	system := organizeFixture()

	result, err := Organize(context.Background(), system, organizeRequest(true))
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}

	if result.Moved != 6 {
		t.Errorf("Moved = %d, want 6", result.Moved)
	}
	if !system.has(root("Images", "jpg", "photo.jpg")) {
		t.Error("photo.jpg was not moved into Images/jpg")
	}
	if system.has(root("photo.jpg")) {
		t.Error("photo.jpg is still in the root")
	}
	if system.contentOf(root("Images", "jpg", "photo.jpg")) != "a" {
		t.Error("the file content did not survive the move")
	}
}

func TestOrganizeGroupsByExtension(t *testing.T) {
	system := organizeFixture()
	request := organizeRequest(false)
	request.Grouping = GroupByExtension

	result, err := Organize(context.Background(), system, request)
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}

	for _, move := range result.Moves {
		if move.Source == root("photo.jpg") && move.Destination != root("jpg", "photo.jpg") {
			t.Errorf("destination = %s, want a flat extension folder", move.Destination)
		}
	}
}

// Running organize twice must leave the same result as running it once.
func TestOrganizeIsIdempotent(t *testing.T) {
	system := organizeFixture()

	if _, err := Organize(context.Background(), system, organizeRequest(true)); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	after := system.paths()

	second, err := Organize(context.Background(), system, organizeRequest(true))
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}

	if second.Moved != 0 {
		t.Errorf("second pass moved %d files, want 0", second.Moved)
	}
	if len(system.paths()) != len(after) {
		t.Error("the second pass changed the tree")
	}
}

func TestOrganizeConflictPolicies(t *testing.T) {
	build := func() *fakeFS {
		return newFakeFS().
			addFile(root("photo.jpg"), "new").
			addFile(root("Images", "jpg", "photo.jpg"), "existing")
	}

	t.Run("skip leaves the file alone", func(t *testing.T) {
		system := build()
		request := organizeRequest(true)
		request.OnConflict = ConflictSkip

		result, err := Organize(context.Background(), system, request)
		if err != nil {
			t.Fatalf("Organize: %v", err)
		}
		if result.Skipped != 1 || result.Moved != 0 {
			t.Errorf("skipped = %d, moved = %d, want 1 and 0", result.Skipped, result.Moved)
		}
		if system.contentOf(root("Images", "jpg", "photo.jpg")) != "existing" {
			t.Error("the existing file was overwritten")
		}
		if !system.has(root("photo.jpg")) {
			t.Error("the skipped file was moved anyway")
		}
	})

	t.Run("rename numbers the new file", func(t *testing.T) {
		system := build()
		request := organizeRequest(true)
		request.OnConflict = ConflictRename

		result, err := Organize(context.Background(), system, request)
		if err != nil {
			t.Fatalf("Organize: %v", err)
		}
		if result.Moved != 1 {
			t.Fatalf("Moved = %d, want 1", result.Moved)
		}
		if !system.has(root("Images", "jpg", "photo (2).jpg")) {
			t.Errorf("expected a numbered destination, tree is %v", system.paths())
		}
		if system.contentOf(root("Images", "jpg", "photo.jpg")) != "existing" {
			t.Error("the existing file was overwritten")
		}
	})

	t.Run("fail refuses the whole operation", func(t *testing.T) {
		system := build()
		request := organizeRequest(true)
		request.OnConflict = ConflictFail

		_, err := Organize(context.Background(), system, request)
		assertCode(t, err, errors.CodeConflict)
		if len(system.moved) != 0 {
			t.Errorf("moved %v despite the refusal", system.moved)
		}
	})
}

func TestOrganizeRefusesProtectedDirectory(t *testing.T) {
	system := organizeFixture()
	system.protected[root()] = "it is your home directory"

	_, err := Organize(context.Background(), system, organizeRequest(true))
	assertCode(t, err, errors.CodeInvalidInput)

	request := organizeRequest(true)
	request.Force = true
	if _, err := Organize(context.Background(), system, request); err != nil {
		t.Fatalf("--force should permit it: %v", err)
	}
}

func TestOrganizeSkipsHiddenFilesByDefault(t *testing.T) {
	system := organizeFixture().addFile(root(".env"), "secret")

	result, err := Organize(context.Background(), system, organizeRequest(false))
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}
	for _, move := range result.Moves {
		if filepath.Base(move.Source) == ".env" {
			t.Error("a hidden file was included without --include-hidden")
		}
	}
}

// Without --recursive, an existing folder structure must be left alone.
func TestOrganizeIsNotRecursiveByDefault(t *testing.T) {
	system := organizeFixture().addFile(root("project", "main.go"), "package main")

	result, err := Organize(context.Background(), system, organizeRequest(false))
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}
	for _, move := range result.Moves {
		if filepath.Base(move.Source) == "main.go" {
			t.Error("a file in a subdirectory was included without --recursive")
		}
	}
}

func TestOrganizeReportsFailuresWithoutAbortingTheRest(t *testing.T) {
	system := organizeFixture()
	system.failMove[root("photo.jpg")] = errors.New(errors.CodePermissionDenied, "access is denied")

	result, err := Organize(context.Background(), system, organizeRequest(true))
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}

	if result.Failed != 1 {
		t.Errorf("Failed = %d, want 1", result.Failed)
	}
	if result.Moved != 5 {
		t.Errorf("Moved = %d, want the other five files to have moved", result.Moved)
	}

	var failed *Move
	for index := range result.Moves {
		if result.Moves[index].Status == MoveFailed {
			failed = &result.Moves[index]
		}
	}
	if failed == nil || failed.Reason == "" {
		t.Fatal("the failure was not reported with a reason")
	}
}

func TestOrganizeRejectsAFilePath(t *testing.T) {
	system := organizeFixture()
	request := organizeRequest(false)
	request.Root = root("photo.jpg")

	_, err := Organize(context.Background(), system, request)
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestOrganizeRejectsAMissingPath(t *testing.T) {
	request := organizeRequest(false)
	request.Root = root("nowhere")

	_, err := Organize(context.Background(), newFakeFS(), request)
	assertCode(t, err, errors.CodeNotFound)
}

func TestOrganizeReportsUnreadableEntries(t *testing.T) {
	system := organizeFixture()
	system.failRead[root("photo.jpg")] = errors.New(errors.CodePermissionDenied, "access is denied")

	result, err := Organize(context.Background(), system, organizeRequest(true))
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}
	if len(result.Problems) != 1 {
		t.Fatalf("problems = %v, want one", result.Problems)
	}
	if result.Problems[0].Code != string(errors.CodePermissionDenied) {
		t.Errorf("problem code = %q", result.Problems[0].Code)
	}
	if result.Moved != 5 {
		t.Errorf("Moved = %d, want the readable files to have moved", result.Moved)
	}
}

func TestOrganizeCancellationStopsBetweenFiles(t *testing.T) {
	system := organizeFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := Organize(ctx, system, organizeRequest(true))
	if err == nil {
		if result.Moved != 0 {
			t.Errorf("Moved = %d after cancellation, want 0", result.Moved)
		}
		return
	}
	assertCode(t, err, errors.CodeCancelled)
}
