package clean

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// fakeFS is an in-memory tree implementing Remover.
//
// It records every removal, which is what most of these tests assert on: this
// module is judged by what it refuses to delete, and a fake that reports
// nothing removed is the only way to prove a refusal actually refused.
type fakeFS struct {
	files    map[string]int64
	dirs     map[string]bool
	symlinks map[string]bool
	// devices overrides which filesystem a path sits on.
	devices map[string]uint64
	// protected overrides the platform's protected-path table.
	protected map[string]string
	// failRemove makes a removal fail, as a locked file would.
	failRemove map[string]error

	removed []string
}

func newFakeFS() *fakeFS {
	fake := &fakeFS{
		files:      map[string]int64{},
		dirs:       map[string]bool{},
		symlinks:   map[string]bool{},
		devices:    map[string]uint64{},
		protected:  map[string]string{},
		failRemove: map[string]error{},
	}
	return fake.withDir(root())
}

func root(parts ...string) string {
	return filepath.Join(append([]string{filepath.FromSlash("/project")}, parts...)...)
}

// with places a file, creating the directories above it.
func (f *fakeFS) with(path string, size int64) *fakeFS {
	full := root(strings.Split(path, "/")...)
	f.files[full] = size

	directory := filepath.Dir(full)
	for directory != "" && directory != filepath.Dir(directory) {
		f.dirs[directory] = true
		directory = filepath.Dir(directory)
	}
	return f
}

func (f *fakeFS) withDir(path string) *fakeFS {
	f.dirs[path] = true
	return f
}

func (f *fakeFS) withSymlink(path string) *fakeFS {
	full := root(strings.Split(path, "/")...)
	f.dirs[full] = true
	f.symlinks[full] = true
	return f
}

func (f *fakeFS) Resolve(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New(errors.CodeInvalidInput, "empty path")
	}
	if path == "." {
		return root(), nil
	}
	return filepath.Clean(path), nil
}

func (f *fakeFS) Stat(path string) (fs.Entry, error) {
	clean := filepath.Clean(path)
	if f.dirs[clean] {
		return fs.Entry{
			Path:      clean,
			Name:      filepath.Base(clean),
			IsDir:     true,
			IsSymlink: f.symlinks[clean],
		}, nil
	}
	size, found := f.files[clean]
	if !found {
		return fs.Entry{}, errors.New(errors.CodeNotFound, "cannot read %s", clean)
	}
	return fs.Entry{Path: clean, Name: filepath.Base(clean), Bytes: size}, nil
}

func (f *fakeFS) Walk(ctx context.Context, options fs.WalkOptions, visit func(fs.Entry) error) error {
	base := filepath.Clean(options.Root)

	paths := make([]string, 0, len(f.files)+len(f.dirs))
	for path := range f.files {
		paths = append(paths, path)
	}
	for path := range f.dirs {
		if path != base {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	skipped := map[string]bool{}

	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}

		relative, err := filepath.Rel(base, path)
		if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
			continue
		}
		depth := len(strings.Split(filepath.ToSlash(relative), "/"))
		if options.MaxDepth > 0 && depth > options.MaxDepth {
			continue
		}

		isDir := f.dirs[path]
		if underSkipped(skipped, path) {
			continue
		}
		if options.Skip != nil && options.Skip(path, isDir) {
			skipped[path] = true
			continue
		}
		if isDir && !options.IncludeDirs {
			continue
		}

		entry, err := f.Stat(path)
		if err != nil {
			return err
		}
		if err := visit(entry); err != nil {
			return err
		}
	}
	return nil
}

func underSkipped(skipped map[string]bool, path string) bool {
	for parent := filepath.Dir(path); parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		if skipped[parent] {
			return true
		}
	}
	return false
}

func (f *fakeFS) Contains(root, target string) (bool, error) {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil {
		return false, nil
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false, nil
	}
	return true, nil
}

func (f *fakeFS) ProtectedReason(path string) string {
	return f.protected[filepath.Clean(path)]
}

func (f *fakeFS) DeviceID(path string) (uint64, bool) {
	if device, found := f.devices[filepath.Clean(path)]; found {
		return device, true
	}
	return 1, true
}

func (f *fakeFS) RemoveAll(path string) error {
	if err, failing := f.failRemove[filepath.Clean(path)]; failing {
		return err
	}

	f.removed = append(f.removed, filepath.Clean(path))
	delete(f.dirs, filepath.Clean(path))
	return nil
}

// project is a realistic tree: a Node project with a build directory, a Python
// cache, and ordinary source.
func project() *fakeFS {
	return newFakeFS().
		with("package.json", 400).
		with("src/index.js", 1200).
		with("node_modules/left-pad/index.js", 900).
		with("node_modules/react/index.js", 5000).
		with("dist/bundle.js", 3000).
		with("src/__pycache__/module.cpython-312.pyc", 700)
}

func assertCode(t *testing.T, err error, want errors.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with code %q, got nil", want)
	}
	if got := errors.CodeOf(err); got != want {
		t.Fatalf("code = %q, want %q (%v)", got, want, err)
	}
}

func names(candidates []Candidate) []string {
	found := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		found = append(found, candidate.Relative)
	}
	sort.Strings(found)
	return found
}

func scan(t *testing.T, system *fakeFS, request ScanRequest) ScanResult {
	t.Helper()
	if request.Root == "" {
		request.Root = root()
	}

	result, err := Scan(context.Background(), system, request)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return result
}

func TestScanFindsTheArtifactsAndAddsUpWhatTheyCost(t *testing.T) {
	result := scan(t, project(), ScanRequest{})

	want := []string{"dist", "node_modules", "src/__pycache__"}
	if got := names(result.Candidates); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("candidates = %v, want %v", got, want)
	}

	if result.TotalBytes != 900+5000+3000+700 {
		t.Errorf("totalBytes = %d, want the sum of every file inside them", result.TotalBytes)
	}
	if result.TotalFiles != 4 {
		t.Errorf("totalFiles = %d, want 4", result.TotalFiles)
	}
	// Largest first: the first line should be the one worth acting on.
	if result.Candidates[0].Relative != "node_modules" {
		t.Errorf("first candidate = %q, want the largest", result.Candidates[0].Relative)
	}
}

func TestScanChangesNothing(t *testing.T) {
	system := project()
	scan(t, system, ScanRequest{})

	if len(system.removed) != 0 {
		t.Fatalf("a scan removed %v", system.removed)
	}
}

// A generic name is evidence of nothing on its own. "build" beside a
// package.json is build output; "build" in a directory of photographs is
// somebody's work.
func TestScanRequiresAMarkerForGenericNames(t *testing.T) {
	withProject := newFakeFS().
		with("package.json", 100).
		with("build/output.js", 2000)

	if got := names(scan(t, withProject, ScanRequest{}).Candidates); len(got) != 1 {
		t.Errorf("candidates = %v, want the build directory beside a package.json", got)
	}

	bare := newFakeFS().
		with("notes.txt", 100).
		with("build/model.stl", 2000)

	result := scan(t, bare, ScanRequest{})
	if len(result.Candidates) != 0 {
		t.Errorf("candidates = %v, want nothing without a project marker", names(result.Candidates))
	}
	if len(result.Skipped) != 1 || !strings.Contains(result.Skipped[0].Reason, "project") {
		t.Errorf("skipped = %+v, want the reason recorded", result.Skipped)
	}
}

// node_modules needs no marker: nobody has a personal directory by that name.
func TestScanTakesUnambiguousNamesOnTheirOwn(t *testing.T) {
	system := newFakeFS().with("node_modules/anything/index.js", 10)

	if got := names(scan(t, system, ScanRequest{}).Candidates); len(got) != 1 {
		t.Errorf("candidates = %v, want node_modules without a marker", got)
	}
}

func TestScanDoesNotDescendIntoWhatItAlreadyMatched(t *testing.T) {
	system := newFakeFS().
		with("package.json", 100).
		with("node_modules/a/node_modules/b/index.js", 500)

	result := scan(t, system, ScanRequest{})

	if len(result.Candidates) != 1 {
		t.Fatalf("candidates = %v, want the outer directory only", names(result.Candidates))
	}
	if result.TotalBytes != 500 {
		t.Errorf("totalBytes = %d, want the nested file counted once", result.TotalBytes)
	}
}

func TestScanNeverEntersAVersionControlDirectory(t *testing.T) {
	system := newFakeFS().
		with("package.json", 100).
		with(".git/objects/dist/pack", 4000)

	if got := names(scan(t, system, ScanRequest{}).Candidates); len(got) != 0 {
		t.Errorf("candidates = %v, want nothing from inside .git", got)
	}
}

func TestScanRefusesAProtectedRootWithoutForce(t *testing.T) {
	system := project()
	system.protected[root()] = "it is your home directory"

	_, err := Scan(context.Background(), system, ScanRequest{Root: root()})
	assertCode(t, err, errors.CodeInvalidInput)

	if got := scan(t, system, ScanRequest{Root: root(), Force: true}); len(got.Candidates) == 0 {
		t.Error("--force did not lift the refusal")
	}
}

func TestScanSkipsProtectedPathsAndOtherFilesystems(t *testing.T) {
	system := project()
	system.devices[root("dist")] = 99

	result := scan(t, system, ScanRequest{Protect: []string{root("node_modules")}})

	if got := names(result.Candidates); strings.Join(got, ",") != "src/__pycache__" {
		t.Fatalf("candidates = %v, want only the unprotected, same-filesystem one", got)
	}

	reasons := strings.Join(skipReasons(result.Skipped), " | ")
	if !strings.Contains(reasons, "protected") {
		t.Errorf("skipped = %s, want the protected path explained", reasons)
	}
	if !strings.Contains(reasons, "different filesystem") {
		t.Errorf("skipped = %s, want the filesystem boundary explained", reasons)
	}
}

func TestScanRefusesToFollowASymlinkedCandidate(t *testing.T) {
	system := newFakeFS().with("package.json", 100).withSymlink("node_modules")

	result := scan(t, system, ScanRequest{})
	if len(result.Candidates) != 0 {
		t.Fatalf("candidates = %v, want a symlink left alone", names(result.Candidates))
	}
	if len(result.Skipped) != 1 || !strings.Contains(result.Skipped[0].Reason, "symbolic link") {
		t.Errorf("skipped = %+v, want the symlink explained", result.Skipped)
	}
}

func TestScanNarrowsToSelectedPatterns(t *testing.T) {
	result := scan(t, project(), ScanRequest{Patterns: []string{"node_modules"}})

	if got := names(result.Candidates); strings.Join(got, ",") != "node_modules" {
		t.Errorf("candidates = %v, want only the selected pattern", got)
	}
}

// A typed pattern that matches no rule fails loudly. Matching nothing quietly
// is how somebody believes a cleanup ran.
func TestScanRejectsAPatternThatIsNotARule(t *testing.T) {
	_, err := Scan(context.Background(), project(), ScanRequest{
		Root:     root(),
		Patterns: []string{"node_modlues"},
	})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestScanAcceptsConfiguredPatternsWithTheSameMarkerRule(t *testing.T) {
	system := newFakeFS().
		with("package.json", 100).
		with("tmpbuild/output", 800)

	result := scan(t, system, ScanRequest{Configured: []string{"tmpbuild"}})
	if got := names(result.Candidates); strings.Join(got, ",") != "tmpbuild" {
		t.Errorf("candidates = %v, want the configured name", got)
	}

	bare := newFakeFS().with("notes.txt", 10).with("tmpbuild/output", 800)
	if got := scan(t, bare, ScanRequest{Configured: []string{"tmpbuild"}}); len(got.Candidates) != 0 {
		t.Errorf("candidates = %v, want a configured name to still need a marker",
			names(got.Candidates))
	}
}

func TestScanRefusesAFileAndAMissingDirectory(t *testing.T) {
	system := project()

	_, err := Scan(context.Background(), system, ScanRequest{Root: root("package.json")})
	assertCode(t, err, errors.CodeInvalidInput)

	_, err = Scan(context.Background(), system, ScanRequest{Root: root("nowhere")})
	assertCode(t, err, errors.CodeNotFound)
}

func TestRulesAreListedSortedAndExplained(t *testing.T) {
	listing := Rules()
	if len(listing) == 0 {
		t.Fatal("there are no rules")
	}

	for index := 1; index < len(listing); index++ {
		if listing[index-1].Name > listing[index].Name {
			t.Fatalf("rules are not sorted at %d: %q after %q",
				index, listing[index].Name, listing[index-1].Name)
		}
	}
	for _, rule := range listing {
		if rule.Ecosystem == "" || rule.Regenerable == "" {
			t.Errorf("rule %q does not say what it is or what it costs to remove", rule.Name)
		}
	}
}

func skipReasons(skipped []Skip) []string {
	reasons := make([]string, 0, len(skipped))
	for _, skip := range skipped {
		reasons = append(reasons, skip.Reason)
	}
	return reasons
}
