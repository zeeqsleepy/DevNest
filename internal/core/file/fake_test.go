package file

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// fakeFS is an in-memory filesystem implementing Mover.
//
// The module's unit tests run against this rather than a real disk: they need
// no cleanup, they can produce a permission error on demand, and they behave
// identically on every platform. The real filesystem is exercised separately
// by the integration tests in tests/, which is where disk semantics belong.
type fakeFS struct {
	files     map[string]*fakeFile
	protected map[string]string
	failMove  map[string]error
	failRead  map[string]error
	moved     []string
	created   []string
}

type fakeFile struct {
	content  []byte
	modified time.Time
	isDir    bool
}

func newFakeFS() *fakeFS {
	return &fakeFS{
		files:     make(map[string]*fakeFile),
		protected: make(map[string]string),
		failMove:  make(map[string]error),
		failRead:  make(map[string]error),
	}
}

// root is a rooted path that is valid on every supported platform.
func root(parts ...string) string {
	return filepath.Join(append([]string{filepath.FromSlash("/work")}, parts...)...)
}

// addFile places a file, creating the directories above it.
func (f *fakeFS) addFile(path, content string) *fakeFS {
	return f.addFileAt(path, content, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
}

func (f *fakeFS) addFileAt(path, content string, modified time.Time) *fakeFS {
	clean := filepath.Clean(path)
	f.addDir(filepath.Dir(clean))
	f.files[clean] = &fakeFile{content: []byte(content), modified: modified}
	return f
}

func (f *fakeFS) addDir(path string) *fakeFS {
	clean := filepath.Clean(path)
	for clean != "" && clean != "." {
		if existing, seen := f.files[clean]; seen && !existing.isDir {
			break
		}
		f.files[clean] = &fakeFile{isDir: true}
		parent := filepath.Dir(clean)
		if parent == clean {
			break
		}
		clean = parent
	}
	return f
}

func (f *fakeFS) has(path string) bool {
	entry, seen := f.files[filepath.Clean(path)]
	return seen && !entry.isDir
}

func (f *fakeFS) contentOf(path string) string {
	entry, seen := f.files[filepath.Clean(path)]
	if !seen {
		return ""
	}
	return string(entry.content)
}

func (f *fakeFS) paths() []string {
	paths := make([]string, 0, len(f.files))
	for path, entry := range f.files {
		if !entry.isDir {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

// --- Inspector ---

func (f *fakeFS) Resolve(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New(errors.CodeInvalidInput, "empty path")
	}
	return filepath.Clean(path), nil
}

func (f *fakeFS) Contains(base, target string) (bool, error) {
	relative, err := filepath.Rel(fs.PathIdentity(base), fs.PathIdentity(target))
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

func (f *fakeFS) Stat(path string) (fs.Entry, error) {
	clean := filepath.Clean(path)
	entry, seen := f.files[clean]
	if !seen {
		return fs.Entry{}, errors.New(errors.CodeNotFound, "cannot read %s", clean)
	}
	return fs.Entry{
		Path:    clean,
		Name:    filepath.Base(clean),
		Bytes:   int64(len(entry.content)),
		ModTime: entry.modified,
		IsDir:   entry.isDir,
	}, nil
}

func (f *fakeFS) Walk(ctx context.Context, options fs.WalkOptions, visit func(fs.Entry) error) error {
	base := filepath.Clean(options.Root)

	paths := make([]string, 0, len(f.files))
	for path := range f.files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry := f.files[path]
		if entry.isDir || path == base {
			continue
		}

		relative, err := filepath.Rel(base, path)
		if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
			continue
		}
		segments := strings.Split(filepath.ToSlash(relative), "/")

		if options.MaxDepth > 0 && len(segments) > options.MaxDepth {
			continue
		}
		if skipSegments(segments, options) {
			continue
		}
		if err, failing := f.failRead[path]; failing {
			if options.OnProblem == nil {
				return err
			}
			options.OnProblem(path, err)
			continue
		}

		info, err := f.Stat(path)
		if err != nil {
			return err
		}
		if err := visit(info); err != nil {
			return err
		}
	}

	return nil
}

// skipSegments applies the hidden and exclusion rules to every component, so
// that excluding a directory excludes what is inside it.
func skipSegments(segments []string, options fs.WalkOptions) bool {
	for _, segment := range segments {
		if !options.IncludeHidden && strings.HasPrefix(segment, ".") {
			return true
		}
		for _, pattern := range options.Exclude {
			if pattern == segment {
				return true
			}
			if matched, err := filepath.Match(pattern, segment); err == nil && matched {
				return true
			}
		}
	}
	return false
}

func (f *fakeFS) Digest(_ context.Context, path string, algorithms []fs.Algorithm) ([]fs.Checksum, error) {
	clean := filepath.Clean(path)
	if err, failing := f.failRead[clean]; failing {
		return nil, err
	}
	entry, seen := f.files[clean]
	if !seen || entry.isDir {
		return nil, errors.New(errors.CodeNotFound, "cannot read %s", clean)
	}

	checksums := make([]fs.Checksum, 0, len(algorithms))
	for _, algorithm := range algorithms {
		var digest hash.Hash
		switch algorithm {
		case fs.MD5:
			digest = md5.New()
		case fs.SHA512:
			digest = sha512.New()
		default:
			digest = sha256.New()
		}
		digest.Write(entry.content)
		checksums = append(checksums, fs.Checksum{
			Algorithm: string(algorithm),
			Value:     hex.EncodeToString(digest.Sum(nil)),
		})
	}
	return checksums, nil
}

// --- Mover ---

func (f *fakeFS) Exists(path string) (bool, error) {
	_, seen := f.files[filepath.Clean(path)]
	return seen, nil
}

func (f *fakeFS) EnsureDir(path string) error {
	clean := filepath.Clean(path)
	if _, seen := f.files[clean]; !seen {
		f.created = append(f.created, clean)
	}
	f.addDir(clean)
	return nil
}

func (f *fakeFS) Move(source, destination string) error {
	from := filepath.Clean(source)
	to := filepath.Clean(destination)

	if err, failing := f.failMove[from]; failing {
		return err
	}
	if _, taken := f.files[to]; taken {
		return errors.New(errors.CodeConflict, "destination already exists: %s", to)
	}
	entry, seen := f.files[from]
	if !seen {
		return errors.New(errors.CodeNotFound, "cannot move %s", from)
	}

	f.files[to] = entry
	delete(f.files, from)
	f.moved = append(f.moved, from+" -> "+to)
	return nil
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
