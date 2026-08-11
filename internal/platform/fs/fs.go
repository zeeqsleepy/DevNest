// Package fs is the filesystem half of the platform layer.
//
// Everything that touches a real disk lives here, behind methods on System.
// Modules declare the narrow interfaces they need and receive a System in
// production and a fake in tests, so no module has to know how a directory is
// read or how paths differ between operating systems.
package fs

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// Entry describes one filesystem entry. It is plain data so a caller can
// collect entries without holding an open handle.
type Entry struct {
	Path      string      `json:"path"`
	Name      string      `json:"name"`
	Bytes     int64       `json:"bytes"`
	Mode      os.FileMode `json:"-"`
	ModTime   time.Time   `json:"modifiedAt"`
	IsDir     bool        `json:"isDirectory"`
	IsSymlink bool        `json:"isSymlink"`
}

// WalkOptions controls a tree walk.
type WalkOptions struct {
	// Root is the absolute, already-resolved directory to walk.
	Root string
	// MaxDepth limits how deep the walk goes. Zero means unlimited; one
	// means the direct children of Root and nothing further.
	MaxDepth int
	// FollowSymlinks makes the walk descend into symlinked directories.
	// Cycles are detected by resolved path, so a link pointing at its own
	// ancestor terminates instead of looping.
	FollowSymlinks bool
	// IncludeHidden includes dotfiles, and on Windows entries carrying the
	// hidden attribute.
	IncludeHidden bool
	// IncludeDirs calls visit for directories as well as files.
	IncludeDirs bool
	// Exclude holds glob patterns matched against an entry's base name. A
	// matching directory is not descended into, which is where most of the
	// time is saved on a tree with a large dependency directory in it.
	Exclude []string
	// Skip decides, per entry, whether to leave it out. A directory it
	// rejects is not descended into.
	//
	// Exclude answers "does this name match a pattern", which is all most
	// callers need. Skip exists for the callers whose rule depends on the
	// whole path rather than the name: ignore files are written relative to
	// the root they sit in, and a rule that can only see a base name cannot
	// express "build/ at the top level but not lib/build/".
	//
	// The path passed is the entry's full path. It is called before a
	// directory is read, so the saving is the subtree, not one entry.
	Skip func(path string, isDir bool) bool
	// OnProblem receives entries that could not be read. When it is nil, such
	// a problem aborts the walk; when it is set, the walk continues. A scan
	// over forty thousand files that gives up on the first unreadable one is
	// not useful.
	OnProblem func(path string, err error)
}

// System is the real filesystem. Its zero value is ready to use.
type System struct{}

// Walk visits every entry under options.Root in a deterministic order.
func (s System) Walk(ctx context.Context, options WalkOptions, visit func(Entry) error) error {
	visited := make(map[string]bool)
	return s.walk(ctx, options, options.Root, 1, visited, visit)
}

func (s System) walk(
	ctx context.Context,
	options WalkOptions,
	directory string,
	depth int,
	visited map[string]bool,
	visit func(Entry) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	names, err := os.ReadDir(directory)
	if err != nil {
		return s.report(options, directory, wrapPath("read directory", directory, err))
	}

	// os.ReadDir already sorts by filename. Sorting again is cheap insurance
	// against that guarantee changing, and determinism is what makes two runs
	// over an unchanged tree produce identical output.
	sort.Slice(names, func(i, j int) bool { return names[i].Name() < names[j].Name() })

	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}

		entry, skip, err := s.describe(options, directory, name)
		if err != nil {
			if err := s.report(options, entry.Path, err); err != nil {
				return err
			}
			continue
		}
		if skip {
			continue
		}

		if !entry.IsDir {
			if err := visit(entry); err != nil {
				return err
			}
			continue
		}

		if options.IncludeDirs {
			if err := visit(entry); err != nil {
				return err
			}
		}
		if options.MaxDepth > 0 && depth >= options.MaxDepth {
			continue
		}
		if err := s.descend(ctx, options, entry, depth, visited, visit); err != nil {
			return err
		}
	}

	return nil
}

// describe turns a directory entry into an Entry, applying the exclusion and
// hidden-file rules. It reports skip=true for anything the options filter out.
func (s System) describe(options WalkOptions, directory string, name os.DirEntry) (Entry, bool, error) {
	path := filepath.Join(directory, name.Name())

	if matchesAny(name.Name(), options.Exclude) {
		return Entry{Path: path}, true, nil
	}
	if !options.IncludeHidden && isHidden(path, name.Name()) {
		return Entry{Path: path}, true, nil
	}

	info, err := name.Info()
	if err != nil {
		return Entry{Path: path}, false, wrapPath("read file information for", path, err)
	}

	entry := Entry{
		Path:      path,
		Name:      name.Name(),
		Bytes:     info.Size(),
		Mode:      info.Mode(),
		ModTime:   info.ModTime(),
		IsDir:     info.IsDir(),
		IsSymlink: info.Mode()&os.ModeSymlink != 0,
	}

	if options.Skip != nil && options.Skip(path, entry.IsDir) {
		return Entry{Path: path}, true, nil
	}

	// A symlink's own metadata says nothing about what it points at, so a
	// link to a directory has to be resolved before the walk can decide
	// whether to descend.
	if entry.IsSymlink && options.FollowSymlinks {
		target, err := os.Stat(path)
		if err != nil {
			return entry, false, wrapPath("resolve symlink", path, err)
		}
		entry.IsDir = target.IsDir()
		entry.Bytes = target.Size()
	}

	return entry, false, nil
}

func (s System) descend(
	ctx context.Context,
	options WalkOptions,
	entry Entry,
	depth int,
	visited map[string]bool,
	visit func(Entry) error,
) error {
	if entry.IsSymlink {
		if !options.FollowSymlinks {
			return nil
		}
		resolved, err := filepath.EvalSymlinks(entry.Path)
		if err != nil {
			return s.report(options, entry.Path, wrapPath("resolve symlink", entry.Path, err))
		}
		key := pathKey(resolved)
		if visited[key] {
			return nil
		}
		visited[key] = true
	}
	return s.walk(ctx, options, entry.Path, depth+1, visited, visit)
}

// report routes a non-fatal problem to OnProblem, or makes it fatal when the
// caller did not supply one.
func (s System) report(options WalkOptions, path string, err error) error {
	if options.OnProblem == nil {
		return err
	}
	options.OnProblem(path, err)
	return nil
}

// Stat describes a single path, following symlinks.
func (s System) Stat(path string) (Entry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Entry{}, wrapPath("read file information for", path, err)
	}

	link, linkErr := os.Lstat(path)
	isSymlink := linkErr == nil && link.Mode()&os.ModeSymlink != 0

	return Entry{
		Path:      path,
		Name:      filepath.Base(path),
		Bytes:     info.Size(),
		Mode:      info.Mode(),
		ModTime:   info.ModTime(),
		IsDir:     info.IsDir(),
		IsSymlink: isSymlink,
	}, nil
}

// Open returns a reader over a file's contents.
//
// It exists so that a module can stream a file it is too large to hold: the
// caller reads through a fixed buffer and closes the handle, and no layer above
// this one has to know how a file is opened. The handle is read-only, so a
// failed Close cannot lose data.
func (s System) Open(path string) (io.ReadCloser, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, wrapPath("read", path, err)
	}
	return file, nil
}

// ReadFile returns a whole file. It is for the small ones a caller genuinely
// holds in memory, such as a configuration file; anything that streams uses
// Open instead.
func (s System) ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, wrapPath("read", path, err)
	}
	return data, nil
}

// Exists reports whether a path exists, without following a final symlink.
func (s System) Exists(path string) (bool, error) {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, wrapPath("check", path, err)
	}
}

// Resolve turns any path into an absolute one with symlinks resolved.
//
// A path that does not exist yet still resolves: the cleaned absolute form is
// returned. Destinations are checked for containment before they are created,
// and refusing to reason about a path until it exists would make that
// impossible.
func (s System) Resolve(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", wrapPath("resolve", path, err)
	}

	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return filepath.Clean(absolute), nil
		}
		return "", wrapPath("resolve", path, err)
	}
	return resolved, nil
}

// Contains reports whether target lies inside root.
//
// Both paths must already be resolved. Deciding containment on an unresolved
// path and then acting on the resolved one is the classic hole that lets "../"
// escape a supposedly contained operation.
func (s System) Contains(root, target string) (bool, error) {
	relative, err := filepath.Rel(pathKey(root), pathKey(target))
	if err != nil {
		return false, nil
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false, nil
	}
	return true, nil
}

// EnsureDir creates a directory and any missing parents.
func (s System) EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return wrapPath("create directory", path, err)
	}
	return nil
}

// WriteAtomic writes a file by rendering it beside the target and renaming it
// into place, creating the directory if it is missing.
//
// The temporary file lives in the destination directory so the rename stays
// within one filesystem and therefore stays atomic. An interrupted write leaves
// the previous file or the new one, never a truncated file that still looks
// valid, which is what matters when the file is a report somebody decides to
// trust.
func (s System) WriteAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := s.EnsureDir(directory); err != nil {
		return err
	}

	file, err := os.CreateTemp(directory, ".devnest-write-*")
	if err != nil {
		return wrapPath("write", path, err)
	}
	temporary := file.Name()

	if err := writeAndSync(file, data); err != nil {
		os.Remove(temporary)
		return wrapPath("write", path, err)
	}
	if err := os.Rename(temporary, path); err != nil {
		os.Remove(temporary)
		return wrapPath("write", path, err)
	}
	return nil
}

// writeAndSync flushes to the disk rather than to the operating system's cache,
// so a report that exists after a crash is a complete one.
func writeAndSync(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// Writable reports whether a directory can be written to, by writing to it and
// removing what it wrote.
//
// Permission bits do not answer this question. On Windows they describe almost
// nothing about what the current user may do, and everywhere else an access
// control list, a read-only mount, or a full disk can refuse a write that the
// mode says is allowed. The only honest test is the attempt.
//
// A nil error means the write and the removal both succeeded. The temporary
// file is created with a recognisable name so that one left behind by a process
// killed mid-check can be identified.
func (s System) Writable(directory string) error {
	file, err := os.CreateTemp(directory, ".devnest-writable-*")
	if err != nil {
		return wrapPath("write to", directory, err)
	}

	name := file.Name()
	if err := file.Close(); err != nil {
		os.Remove(name)
		return wrapPath("write to", directory, err)
	}
	if err := os.Remove(name); err != nil {
		return wrapPath("remove", name, err)
	}
	return nil
}

// Move renames source to destination, refusing to replace anything that is
// already there.
//
// The existence check and the rename are two operations, so a file appearing
// in between would still be overwritten on platforms where rename replaces.
// Closing that window needs a platform-specific atomic operation; for a
// workstation tool acting inside a directory the user chose, the check is the
// right trade. It is recorded here so nobody assumes otherwise.
//
// A rename across filesystems fails rather than falling back to copy-then-
// delete. That fallback deletes the source, and an interruption partway
// through loses data, which is not a trade this tool makes.
func (s System) Move(source, destination string) error {
	// On a case-insensitive filesystem a destination differing only in case
	// resolves to the source file itself, so the existence check below would
	// refuse a legitimate case-only rename. The rename is what changes the
	// case; let it through. On a case-sensitive filesystem the two paths can
	// only name the same file when they are identical, and renaming a file
	// onto itself is the no-op the caller is asking for.
	if !sameFile(source, destination) {
		exists, err := s.Exists(destination)
		if err != nil {
			return err
		}
		if exists {
			return errors.New(errors.CodeConflict,
				"destination already exists: %s", destination)
		}
	}

	if err := os.Rename(source, destination); err != nil {
		return wrapPath("move", source, err)
	}
	return nil
}

// ProtectedReason names why a path must not be operated on in bulk, or returns
// an empty string when the path is ordinary.
func (s System) ProtectedReason(path string) string {
	return protectedReason(path)
}

// RemoveAll deletes a directory and everything inside it.
//
// This is the only method in the platform layer that destroys data, and it is
// deliberately dumb: it checks nothing beyond the operation succeeding. Every
// guard that decides whether a path may be deleted at all lives in the module
// above, where the rules can be read in one place and tested against the
// refusal rather than the success. A platform method that also enforced policy
// would be a second, quieter copy of those rules.
//
// A path that does not exist is not an error. Two runs of a cleanup where the
// first succeeded should not differ.
func (s System) RemoveAll(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return wrapPath("remove", path, err)
	}
	return nil
}

// DeviceID identifies the filesystem a path lives on, when the platform can
// say.
//
// The second return value is false where that question has no cheap answer,
// which is currently Windows. A caller uses this to avoid walking off the
// filesystem it started on; where the answer is unavailable it has to fall
// back to containment, which is the check that matters most anyway.
func (s System) DeviceID(path string) (uint64, bool) {
	return deviceID(path)
}

// PathIdentity normalises a path so that two spellings of the same file
// compare equal. It is a pure function rather than a method because it
// performs no I/O, but it still belongs here: whether case matters is a
// property of the platform, and nothing above this layer should have to know
// the answer.
func PathIdentity(path string) string {
	return pathKey(path)
}

func matchesAny(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		if pattern == name {
			return true
		}
		if matched, err := filepath.Match(pattern, name); err == nil && matched {
			return true
		}
	}
	return false
}

// wrapPath classifies a filesystem error and records what was attempted.
func wrapPath(operation, path string, err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return errors.Wrap(err, errors.CodeNotFound, "cannot %s %s", operation, path)
	case errors.Is(err, os.ErrPermission):
		return errors.Wrap(err, errors.CodePermissionDenied, "cannot %s %s", operation, path)
	case errors.Is(err, os.ErrExist):
		return errors.Wrap(err, errors.CodeConflict, "cannot %s %s", operation, path)
	default:
		return errors.Wrap(err, errors.CodeIO, "cannot %s %s", operation, path)
	}
}
