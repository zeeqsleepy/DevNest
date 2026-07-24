package log

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// fakeReader is an in-memory filesystem implementing Reader.
//
// The module's unit tests run against this rather than a real disk: they need
// no cleanup, they can produce a read error on demand, and they behave
// identically on every platform. The sample logs in testdata are loaded
// through it, so the fixtures are realistic while the tests stay in memory.
type fakeReader struct {
	files    map[string]string
	dirs     map[string]bool
	failOpen map[string]error
}

func newFakeReader() *fakeReader {
	return &fakeReader{
		files:    make(map[string]string),
		dirs:     make(map[string]bool),
		failOpen: make(map[string]error),
	}
}

// with places a file with the given contents.
func (f *fakeReader) with(path, content string) *fakeReader {
	f.files[filepath.Clean(path)] = content
	return f
}

// withDir places a directory, so the "that is a directory" path can be tested.
func (f *fakeReader) withDir(path string) *fakeReader {
	f.dirs[filepath.Clean(path)] = true
	return f
}

func (f *fakeReader) Resolve(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New(errors.CodeInvalidInput, "empty path")
	}
	return filepath.Clean(path), nil
}

func (f *fakeReader) Stat(path string) (fs.Entry, error) {
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

func (f *fakeReader) Open(path string) (io.ReadCloser, error) {
	clean := filepath.Clean(path)
	if err, failing := f.failOpen[clean]; failing {
		return nil, err
	}
	content, seen := f.files[clean]
	if !seen {
		return nil, errors.New(errors.CodeNotFound, "cannot read %s", clean)
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

// fixture loads one of the sample logs from testdata into a fake reader.
//
// The samples are real log output rather than minimal strings, because the
// failures worth catching here are the ones a hand-written two-line fixture
// never contains: a request path with a space in it, a "-" where a byte count
// should be, a rotation notice in the middle of an access log.
func fixture(t *testing.T, name string) (*fakeReader, string) {
	t.Helper()

	content, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}

	path := filepath.Join("logs", name)
	return newFakeReader().with(path, string(content)), path
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

// find returns the count recorded for a value, and whether it was there.
func find(counts []Count, value string) (Count, bool) {
	for _, count := range counts {
		if count.Value == value {
			return count, true
		}
	}
	return Count{}, false
}

// requireCount fails unless a listing holds the value with the expected count.
func requireCount(t *testing.T, counts []Count, value string, want int) {
	t.Helper()
	got, found := find(counts, value)
	if !found {
		t.Fatalf("%q is missing from the listing %v", value, counts)
	}
	if got.Count != want {
		t.Errorf("count for %q = %d, want %d", value, got.Count, want)
	}
}
