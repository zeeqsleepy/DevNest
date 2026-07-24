package data

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// fakeFS is an in-memory filesystem implementing Reader.
//
// The module's tests run against this rather than a real disk: no cleanup, no
// temporary directories, and identical behaviour on every platform.
type fakeFS struct {
	files map[string]string
	dirs  map[string]bool
	// size overrides the reported size of a file, so the size limit can be
	// tested without writing sixty-four megabytes to anything.
	size map[string]int64
}

func newFakeFS() *fakeFS {
	return &fakeFS{
		files: make(map[string]string),
		dirs:  map[string]bool{path(): true},
		size:  make(map[string]int64),
	}
}

// path builds a rooted path valid on every supported platform.
func path(parts ...string) string {
	return filepath.Join(append([]string{filepath.FromSlash("/project")}, parts...)...)
}

func (f *fakeFS) with(name, content string) *fakeFS {
	f.files[path(name)] = content
	return f
}

func (f *fakeFS) withSize(name string, bytes int64) *fakeFS {
	f.size[path(name)] = bytes
	return f
}

func (f *fakeFS) Resolve(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New(errors.CodeInvalidInput, "empty path")
	}
	return filepath.Clean(name), nil
}

func (f *fakeFS) Stat(name string) (fs.Entry, error) {
	clean := filepath.Clean(name)
	if f.dirs[clean] {
		return fs.Entry{Path: clean, Name: filepath.Base(clean), IsDir: true}, nil
	}
	content, seen := f.files[clean]
	if !seen {
		return fs.Entry{}, errors.New(errors.CodeNotFound, "cannot read %s", clean)
	}
	size := int64(len(content))
	if override, set := f.size[clean]; set {
		size = override
	}
	return fs.Entry{Path: clean, Name: filepath.Base(clean), Bytes: size}, nil
}

func (f *fakeFS) Open(name string) (io.ReadCloser, error) {
	clean := filepath.Clean(name)
	content, seen := f.files[clean]
	if !seen {
		return nil, errors.New(errors.CodeNotFound, "cannot read %s", clean)
	}
	return io.NopCloser(strings.NewReader(content)), nil
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

// message returns the user-facing text of an error, which is where the line
// and column of a parse failure have to appear.
func message(t *testing.T, err error) string {
	t.Helper()
	var typed *errors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("error is not a DevNest error: %v", err)
	}
	return typed.Message + " | " + typed.Hint
}

// file names a document in the fake filesystem.
func file(name string) Request { return Request{Path: path(name)} }

// piped is a document arriving on standard input.
func piped(content string) Request { return Request{Input: strings.NewReader(content)} }
