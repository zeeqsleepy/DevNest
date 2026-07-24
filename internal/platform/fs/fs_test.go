package fs

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

// These tests use a real temporary directory, and they are not tagged.
//
// The platform layer is the filesystem seam: a test of it that does not touch
// a disk proves nothing about how a rename behaves, how Windows treats a
// hidden attribute, or whether a path comparison is case-sensitive. Everything
// above this layer uses fakes and never touches a real disk; here it is the
// whole point. t.TempDir cleans up after itself, so nothing depends on the
// state of the machine running the tests.

func write(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent of %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func tree(t *testing.T) string {
	t.Helper()
	base := t.TempDir()

	write(t, filepath.Join(base, "photo.jpg"), "image bytes")
	write(t, filepath.Join(base, "manual.pdf"), "document bytes")
	write(t, filepath.Join(base, "notes.txt"), "some notes")
	write(t, filepath.Join(base, "src", "main.go"), "package main")
	write(t, filepath.Join(base, "src", "deep", "util.go"), "package util")
	write(t, filepath.Join(base, ".hidden"), "hidden")

	return base
}

func names(t *testing.T, options WalkOptions) []string {
	t.Helper()

	var seen []string
	err := System{}.Walk(context.Background(), options, func(entry Entry) error {
		seen = append(seen, entry.Name)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	return seen
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestWalkVisitsFilesRecursively(t *testing.T) {
	seen := names(t, WalkOptions{Root: tree(t)})

	for _, wanted := range []string{"photo.jpg", "manual.pdf", "notes.txt", "main.go", "util.go"} {
		if !contains(seen, wanted) {
			t.Errorf("visited %v, missing %q", seen, wanted)
		}
	}
	if contains(seen, ".hidden") {
		t.Error("a hidden file was visited by default")
	}
}

func TestWalkIncludesHiddenOnRequest(t *testing.T) {
	seen := names(t, WalkOptions{Root: tree(t), IncludeHidden: true})

	if !contains(seen, ".hidden") {
		t.Errorf("visited %v, want the hidden file included", seen)
	}
}

func TestWalkRespectsMaxDepth(t *testing.T) {
	seen := names(t, WalkOptions{Root: tree(t), MaxDepth: 1})

	if contains(seen, "main.go") {
		t.Errorf("visited %v, want nothing below the first level", seen)
	}
	if !contains(seen, "photo.jpg") {
		t.Errorf("visited %v, want the top-level files", seen)
	}

	seen = names(t, WalkOptions{Root: tree(t), MaxDepth: 2})
	if !contains(seen, "main.go") {
		t.Errorf("visited %v, want the second level at depth 2", seen)
	}
	if contains(seen, "util.go") {
		t.Errorf("visited %v, want nothing at the third level", seen)
	}
}

// Excluding a directory must skip what is inside it, not merely the directory
// entry. This is also the optimisation that keeps a scan off a huge dependency
// folder rather than walking into it and discarding the results.
func TestWalkExcludesDirectoryContents(t *testing.T) {
	seen := names(t, WalkOptions{Root: tree(t), Exclude: []string{"src"}})

	if contains(seen, "main.go") {
		t.Errorf("visited %v, want the excluded directory skipped entirely", seen)
	}
}

func TestWalkExcludeAcceptsGlobs(t *testing.T) {
	seen := names(t, WalkOptions{Root: tree(t), Exclude: []string{"*.jpg"}})

	if contains(seen, "photo.jpg") {
		t.Errorf("visited %v, want the glob applied", seen)
	}
	if !contains(seen, "manual.pdf") {
		t.Errorf("visited %v, want unrelated files kept", seen)
	}
}

func TestWalkIncludesDirectoriesOnRequest(t *testing.T) {
	seen := names(t, WalkOptions{Root: tree(t), IncludeDirs: true})

	if !contains(seen, "src") {
		t.Errorf("visited %v, want directories included", seen)
	}
}

// Two runs over an unchanged tree must produce identical output, or a report
// cannot be diffed against an earlier one.
func TestWalkIsDeterministic(t *testing.T) {
	base := tree(t)

	first := names(t, WalkOptions{Root: base})
	second := names(t, WalkOptions{Root: base})

	if strings.Join(first, "|") != strings.Join(second, "|") {
		t.Errorf("two walks produced different orders:\n%v\n%v", first, second)
	}
}

func TestWalkCarriesFileMetadata(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "sized.txt"), "0123456789")

	var entry Entry
	err := System{}.Walk(context.Background(), WalkOptions{Root: base}, func(found Entry) error {
		entry = found
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if entry.Bytes != 10 {
		t.Errorf("Bytes = %d, want 10", entry.Bytes)
	}
	if entry.IsDir {
		t.Error("a file was reported as a directory")
	}
	if entry.ModTime.IsZero() {
		t.Error("ModTime is zero")
	}
	if !filepath.IsAbs(entry.Path) {
		t.Errorf("Path = %q, want an absolute path", entry.Path)
	}
}

func TestWalkStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := System{}.Walk(ctx, WalkOptions{Root: tree(t)}, func(Entry) error {
		t.Error("visited an entry after cancellation")
		return nil
	})
	if errors.CodeOf(err) != errors.CodeCancelled {
		t.Errorf("code = %q, want %q (%v)", errors.CodeOf(err), errors.CodeCancelled, err)
	}
}

func TestWalkPropagatesAVisitorError(t *testing.T) {
	sentinel := errors.New(errors.CodeConflict, "stop here")

	err := System{}.Walk(context.Background(), WalkOptions{Root: tree(t)},
		func(Entry) error { return sentinel })

	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want the visitor's own error", err)
	}
}

func TestWalkMissingRootIsNotFound(t *testing.T) {
	err := System{}.Walk(context.Background(),
		WalkOptions{Root: filepath.Join(t.TempDir(), "absent")},
		func(Entry) error { return nil })

	if errors.CodeOf(err) != errors.CodeNotFound {
		t.Errorf("code = %q, want %q (%v)", errors.CodeOf(err), errors.CodeNotFound, err)
	}
}

// A walk across forty thousand files that gives up on the first unreadable one
// is not useful, so a problem handler turns it into a report and continues.
func TestWalkReportsProblemsInsteadOfFailingWhenAskedTo(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "readable.txt"), "fine")

	var reported []string
	options := WalkOptions{
		Root:      filepath.Join(base, "absent"),
		OnProblem: func(path string, _ error) { reported = append(reported, path) },
	}

	err := System{}.Walk(context.Background(), options, func(Entry) error { return nil })
	if err != nil {
		t.Fatalf("Walk: %v, want the problem reported rather than returned", err)
	}
	if len(reported) != 1 {
		t.Errorf("reported = %v, want one problem", reported)
	}
}

// A move must never replace an existing file, whatever the platform's rename
// does by default: on Unix it silently overwrites, on Windows it fails.
func TestMoveRefusesToReplace(t *testing.T) {
	base := t.TempDir()
	source := write(t, filepath.Join(base, "a.txt"), "source")
	destination := write(t, filepath.Join(base, "b.txt"), "destination")

	err := System{}.Move(source, destination)
	if errors.CodeOf(err) != errors.CodeConflict {
		t.Fatalf("code = %q, want %q (%v)", errors.CodeOf(err), errors.CodeConflict, err)
	}

	content, readErr := os.ReadFile(destination)
	if readErr != nil {
		t.Fatalf("read destination: %v", readErr)
	}
	if string(content) != "destination" {
		t.Error("the destination was overwritten")
	}
	if _, err := os.Stat(source); err != nil {
		t.Error("the source was removed despite the refusal")
	}
}

func TestMoveSucceedsIntoANewName(t *testing.T) {
	base := t.TempDir()
	source := write(t, filepath.Join(base, "a.txt"), "content")
	destination := filepath.Join(base, "sub", "b.txt")

	system := System{}
	if err := system.EnsureDir(filepath.Dir(destination)); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := system.Move(source, destination); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Error("the source still exists after the move")
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "content" {
		t.Errorf("destination content = %q, %v", content, err)
	}
}

func TestMoveMissingSourceIsNotFound(t *testing.T) {
	base := t.TempDir()

	err := System{}.Move(filepath.Join(base, "absent"), filepath.Join(base, "new"))
	if errors.CodeOf(err) != errors.CodeNotFound {
		t.Errorf("code = %q, want %q (%v)", errors.CodeOf(err), errors.CodeNotFound, err)
	}
}

func TestEnsureDirIsRepeatable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c")

	system := System{}
	if err := system.EnsureDir(path); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := system.EnsureDir(path); err != nil {
		t.Fatalf("second EnsureDir: %v", err)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Errorf("Stat = %v, %v", info, err)
	}
}

func TestExists(t *testing.T) {
	base := t.TempDir()
	path := write(t, filepath.Join(base, "a.txt"), "x")
	system := System{}

	if found, err := system.Exists(path); err != nil || !found {
		t.Errorf("Exists(file) = %v, %v", found, err)
	}
	if found, err := system.Exists(filepath.Join(base, "absent")); err != nil || found {
		t.Errorf("Exists(absent) = %v, %v", found, err)
	}
}

func TestStatDescribesAFileAndADirectory(t *testing.T) {
	base := t.TempDir()
	path := write(t, filepath.Join(base, "a.txt"), "0123")
	system := System{}

	file, err := system.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if file.Bytes != 4 || file.IsDir {
		t.Errorf("file = %+v", file)
	}

	directory, err := system.Stat(base)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !directory.IsDir {
		t.Error("a directory was not reported as one")
	}
}

func TestStatMissingPathIsNotFound(t *testing.T) {
	_, err := System{}.Stat(filepath.Join(t.TempDir(), "absent"))

	if errors.CodeOf(err) != errors.CodeNotFound {
		t.Errorf("code = %q, want %q", errors.CodeOf(err), errors.CodeNotFound)
	}
}

func TestContainsRejectsAnEscapingPath(t *testing.T) {
	base := t.TempDir()
	system := System{}

	inside, err := system.Contains(base, filepath.Join(base, "sub", "file.txt"))
	if err != nil || !inside {
		t.Errorf("Contains(inside) = %v, %v", inside, err)
	}

	outside, err := system.Contains(base, filepath.Join(base, "..", "elsewhere"))
	if err != nil {
		t.Fatalf("Contains: %v", err)
	}
	if outside {
		t.Error("a path outside the root was reported as contained")
	}
}

func TestContainsTreatsTheRootAsInsideItself(t *testing.T) {
	base := t.TempDir()

	inside, err := System{}.Contains(base, base)
	if err != nil || !inside {
		t.Errorf("Contains(root, root) = %v, %v", inside, err)
	}
}

// On Windows two spellings differing only in case name the same file, so a
// containment check has to fold case or it will refuse a legitimate path.
func TestContainsFollowsThePlatformCaseRules(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive path comparison is a Windows behaviour")
	}

	base := t.TempDir()
	inside, err := System{}.Contains(strings.ToUpper(base), filepath.Join(base, "file.txt"))
	if err != nil {
		t.Fatalf("Contains: %v", err)
	}
	if !inside {
		t.Error("a path differing only in case was reported as outside the root")
	}
}

// Destinations are checked for containment before they are created, so a path
// that does not exist yet still has to resolve.
func TestResolveHandlesAPathThatDoesNotExistYet(t *testing.T) {
	base := t.TempDir()

	resolved, err := System{}.Resolve(filepath.Join(base, "not-created-yet.txt"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !filepath.IsAbs(resolved) {
		t.Errorf("Resolve returned %q, want an absolute path", resolved)
	}
}

func TestResolveMakesARelativePathAbsolute(t *testing.T) {
	resolved, err := System{}.Resolve(".")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !filepath.IsAbs(resolved) {
		t.Errorf("Resolve(\".\") = %q, want an absolute path", resolved)
	}
}

// An empty path resolves to the working directory rather than failing, which
// matches filepath.Abs. Callers reject an empty path before they get here; see
// the module's own prepare step.
func TestResolveTreatsAnEmptyPathAsTheWorkingDirectory(t *testing.T) {
	resolved, err := System{}.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	working, err := System{}.Resolve(".")
	if err != nil {
		t.Fatalf("Resolve(\".\"): %v", err)
	}
	if resolved != working {
		t.Errorf("Resolve(\"\") = %q, want the working directory %q", resolved, working)
	}
}

func TestPathIdentityFoldsCaseOnlyWhereThePlatformDoes(t *testing.T) {
	upper := PathIdentity(filepath.FromSlash("/Work/Photos"))
	lower := PathIdentity(filepath.FromSlash("/work/photos"))

	if runtime.GOOS == "windows" {
		if upper != lower {
			t.Errorf("%q and %q should be the same path on Windows", upper, lower)
		}
		return
	}
	if upper == lower {
		t.Errorf("%q and %q should be different paths on this platform", upper, lower)
	}
}

// A protected path is one where a mistyped argument turns a routine command
// into an incident.
func TestProtectedReasonNamesTheHomeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this system: %v", err)
	}

	system := System{}
	if reason := system.ProtectedReason(home); reason == "" {
		t.Error("the home directory was not reported as protected")
	}
	if reason := system.ProtectedReason(t.TempDir()); reason != "" {
		t.Errorf("an ordinary directory was reported as protected: %q", reason)
	}
}

func TestProtectedReasonNamesTheFilesystemRoot(t *testing.T) {
	root := string(filepath.Separator)
	if runtime.GOOS == "windows" {
		root = filepath.VolumeName(os.Getenv("SystemDrive")) + string(filepath.Separator)
		if root == string(filepath.Separator) {
			root = `C:\`
		}
	}

	system := System{}
	if reason := system.ProtectedReason(root); reason == "" {
		t.Errorf("%q was not reported as protected", root)
	}
}

func TestOpenStreamsAFile(t *testing.T) {
	path := write(t, filepath.Join(t.TempDir(), "app.log"), "one\ntwo\n")

	reader, err := System{}.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(content) != "one\ntwo\n" {
		t.Errorf("content = %q, want the file's contents", content)
	}
}

// A missing file is classified, not passed through raw: the caller branches on
// the code and the user reads a message naming the path.
func TestOpenClassifiesAMissingFile(t *testing.T) {
	_, err := System{}.Open(filepath.Join(t.TempDir(), "absent.log"))
	if err == nil {
		t.Fatal("Open accepted a path that does not exist")
	}
	if got := errors.CodeOf(err); got != errors.CodeNotFound {
		t.Errorf("code = %q, want %q", got, errors.CodeNotFound)
	}
}

func TestRemoveAllDeletesATreeAndIsQuietAboutWhatIsGone(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "node_modules", "package", "dist")

	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("build a tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "index.js"), []byte("1"), 0o600); err != nil {
		t.Fatalf("write a file: %v", err)
	}

	target := filepath.Join(root, "node_modules")
	if err := (System{}).RemoveAll(target); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("the tree is still there: %v", err)
	}

	// Removing what is already gone is how a second run of a cleanup behaves,
	// and it is not a failure.
	if err := (System{}).RemoveAll(target); err != nil {
		t.Errorf("removing a missing path reported %v", err)
	}
}

func TestDeviceIDAgreesWithItself(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "child")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatalf("create a directory: %v", err)
	}

	first, ok := (System{}).DeviceID(root)
	second, alsoOK := (System{}).DeviceID(nested)

	if ok != alsoOK {
		t.Fatalf("availability differs between two paths on one filesystem: %v and %v", ok, alsoOK)
	}
	if !ok {
		t.Skipf("this platform does not report device identity, which is expected on Windows")
	}
	if first != second {
		t.Errorf("device %d and %d differ for two paths in one temporary directory", first, second)
	}
}

func TestDeviceIDSaysNothingAboutAMissingPath(t *testing.T) {
	if _, ok := (System{}).DeviceID(filepath.Join(t.TempDir(), "nowhere")); ok {
		t.Error("a missing path was given a device identity")
	}
}
