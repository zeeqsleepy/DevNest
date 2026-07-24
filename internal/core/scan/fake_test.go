package scan

import (
	"context"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// fakeFS is an in-memory filesystem implementing Inspector.
//
// The module's tests run against this rather than a real disk: no cleanup, a
// read error on demand, and identical behaviour on every platform. The walk it
// implements applies the same rules the real one does, because the skipping is
// the part these tests are mostly about.
type fakeFS struct {
	files    map[string]string
	dirs     map[string]bool
	failRead map[string]error
}

func newFakeFS() *fakeFS {
	fake := &fakeFS{
		files:    make(map[string]string),
		dirs:     make(map[string]bool),
		failRead: make(map[string]error),
	}
	return fake.withDir(root())
}

// root is a rooted path valid on every supported platform.
func root(parts ...string) string {
	return filepath.Join(append([]string{filepath.FromSlash("/project")}, parts...)...)
}

// with places a file at a slash-separated path under the root, creating the
// directories above it.
func (f *fakeFS) with(path, content string) *fakeFS {
	full := root(strings.Split(path, "/")...)
	f.files[full] = content

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
		return fs.Entry{Path: clean, Name: filepath.Base(clean), IsDir: true}, nil
	}
	content, seen := f.files[clean]
	if !seen {
		return fs.Entry{}, errors.New(errors.CodeNotFound, "cannot read %s", clean)
	}
	return fs.Entry{
		Path:  clean,
		Name:  filepath.Base(clean),
		Bytes: int64(len(content)),
	}, nil
}

func (f *fakeFS) Open(path string) (io.ReadCloser, error) {
	clean := filepath.Clean(path)
	if err, failing := f.failRead[clean]; failing {
		return nil, err
	}
	content, seen := f.files[clean]
	if !seen {
		return nil, errors.New(errors.CodeNotFound, "cannot read %s", clean)
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

// Walk applies the option rules the real walker applies: depth, hidden files,
// exclusion globs, the Skip hook, and IncludeDirs.
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

	skipped := make(map[string]bool)

	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}

		relative, err := filepath.Rel(base, path)
		if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
			continue
		}
		segments := strings.Split(filepath.ToSlash(relative), "/")
		isDir := f.dirs[path]

		if options.MaxDepth > 0 && len(segments) > options.MaxDepth {
			continue
		}
		if skippedAncestor(skipped, path) {
			continue
		}
		if hidden(segments, options) || excluded(segments, options) {
			skipped[path] = true
			continue
		}
		if options.Skip != nil && options.Skip(path, isDir) {
			skipped[path] = true
			continue
		}
		if err, failing := f.failRead[path]; failing {
			if options.OnProblem == nil {
				return err
			}
			options.OnProblem(path, err)
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

// skippedAncestor reports whether anything above a path was skipped, which is
// what makes skipping a directory skip its contents.
func skippedAncestor(skipped map[string]bool, path string) bool {
	for parent := filepath.Dir(path); parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		if skipped[parent] {
			return true
		}
	}
	return false
}

func hidden(segments []string, options fs.WalkOptions) bool {
	if options.IncludeHidden {
		return false
	}
	for _, segment := range segments {
		if strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}

func excluded(segments []string, options fs.WalkOptions) bool {
	for _, segment := range segments {
		for _, pattern := range options.Exclude {
			if matched, err := filepath.Match(pattern, segment); err == nil && matched {
				return true
			}
		}
	}
	return false
}

// project is a small but realistic tree: source, tests, docs, configuration,
// vendored code, build output, and something generated.
func project() *fakeFS {
	return newFakeFS().
		with("main.go", "package main\n\n// entry point\nfunc main() {}\n").
		with("util.go", "package main\n\nfunc helper() {}\n").
		with("util_test.go", "package main\n\nfunc TestHelper(t *testing.T) {}\n").
		with("api.pb.go", "// Code generated by protoc. DO NOT EDIT.\npackage api\n").
		with("README.md", "# Project\n\nSome words.\n").
		with("go.mod", "module example\n").
		with("assets/logo.png", "\x89PNG\r\n\x1a\nbinary").
		with("docs/guide.md", "# Guide\n").
		with("src/app.ts", "// app\nconst a = 1;\n").
		with("src/app.test.ts", "test('a', () => {});\n").
		with("node_modules/left-pad/index.js", "module.exports = 1;\n").
		with("dist/bundle.js", "!function(){}();\n")
}

// assertCode fails the test unless err carries the expected classification.
func assertCode(t *testing.T, err error, want errors.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with code %q, got nil", want)
	}
	if got := errors.CodeOf(err); got != want {
		t.Fatalf("code = %q, want %q (%v)", got, want, err)
	}
}

// find returns the count recorded for a name.
func find(counts []Count, name string) (Count, bool) {
	for _, count := range counts {
		if count.Name == name {
			return count, true
		}
	}
	return Count{}, false
}

func requireCount(t *testing.T, counts []Count, name string, wantFiles int) {
	t.Helper()
	got, found := find(counts, name)
	if !found {
		t.Fatalf("%q is missing from the listing %v", name, counts)
	}
	if got.Files != wantFiles {
		t.Errorf("files for %q = %d, want %d", name, got.Files, wantFiles)
	}
}
