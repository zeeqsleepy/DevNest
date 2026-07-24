// Package file is DevNest's file management module: organising a directory,
// finding duplicates, batch renaming, filtering by extension, analysing
// directory size, and hashing.
//
// Every operation takes a request and returns a result. Nothing here prints,
// exits, reads configuration, or knows that a command line exists. Operations
// that can change the disk take a Mover; everything else takes an Inspector
// and is incapable of modifying anything.
//
// Two rules run through all of it. Nothing is ever deleted: the destructive
// end of this module is a rename, and even that refuses to replace an existing
// file. And nothing is ever changed without the caller setting Apply, so the
// default outcome of every command here is a description of what would happen.
package file

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// Selection is the set of options shared by every operation that walks a tree.
type Selection struct {
	// Root is the directory to operate on, as the user gave it.
	Root string
	// Recursive descends into subdirectories.
	Recursive bool
	// MaxDepth limits the descent. Zero means unlimited.
	MaxDepth int
	// IncludeHidden includes dotfiles and, on Windows, hidden entries.
	IncludeHidden bool
	// FollowSymlinks descends into symlinked directories, with cycle
	// detection. Off by default, because a link pointing somewhere large
	// turns a quick scan into a very slow one.
	FollowSymlinks bool
	// Exclude holds glob patterns matched against base names.
	Exclude []string
}

// Info describes one file in a result.
type Info struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Relative   string    `json:"relativePath"`
	Bytes      int64     `json:"bytes"`
	Extension  string    `json:"extension"`
	Category   string    `json:"category"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// Problem is a non-fatal failure encountered while walking. Reporting these
// rather than aborting is what lets a scan finish across a tree containing a
// directory the current user cannot read.
type Problem struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// walkContext holds the resolved root and the problems collected during a
// walk. Every operation starts by building one.
type walkContext struct {
	root     string
	options  fs.WalkOptions
	problems []Problem
}

// prepare resolves the root, confirms it is a directory, and builds the walk
// options. Resolution happens before anything else so that every later
// decision (containment, protection, destination paths) is made against the
// real path rather than what the user typed.
func prepare(inspector Inspector, selection Selection) (*walkContext, error) {
	if strings.TrimSpace(selection.Root) == "" {
		return nil, errors.New(errors.CodeInvalidInput, "no path was given").
			WithHint("pass a directory, or \".\" for the current one")
	}

	root, err := inspector.Resolve(selection.Root)
	if err != nil {
		return nil, err
	}

	entry, err := inspector.Stat(root)
	if err != nil {
		return nil, err
	}
	if !entry.IsDir {
		return nil, errors.New(errors.CodeInvalidInput, "%s is a file, not a directory", root).
			WithHint("pass the directory that contains it")
	}

	depth := selection.MaxDepth
	if !selection.Recursive {
		depth = 1
	}

	context := &walkContext{root: root}
	context.options = fs.WalkOptions{
		Root:           root,
		MaxDepth:       depth,
		FollowSymlinks: selection.FollowSymlinks,
		IncludeHidden:  selection.IncludeHidden,
		Exclude:        selection.Exclude,
		OnProblem:      context.record,
	}
	return context, nil
}

func (w *walkContext) record(path string, err error) {
	report := errors.Classify(err)
	w.problems = append(w.problems, Problem{
		Path:    path,
		Code:    string(report.Code),
		Message: report.Message,
	})
}

// describe converts a walked entry into the result shape used across the
// module.
func (w *walkContext) describe(entry fs.Entry) Info {
	extension := normalizeExtension(filepath.Ext(entry.Name))

	relative, err := filepath.Rel(w.root, entry.Path)
	if err != nil {
		relative = entry.Name
	}

	return Info{
		Name:       entry.Name,
		Path:       entry.Path,
		Relative:   filepath.ToSlash(relative),
		Bytes:      entry.Bytes,
		Extension:  extension,
		Category:   CategoryOf(extension),
		ModifiedAt: entry.ModTime,
	}
}

// collect walks the tree and returns every file that passes keep.
func (w *walkContext) collect(
	ctx context.Context,
	inspector Inspector,
	keep func(Info) bool,
) ([]Info, error) {
	var files []Info

	err := inspector.Walk(ctx, w.options, func(entry fs.Entry) error {
		info := w.describe(entry)
		if keep == nil || keep(info) {
			files = append(files, info)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// guard refuses a bulk change at a path where a typo would be an incident.
//
// The check cannot be turned off from a configuration file. A safety guard
// that can be disabled once and then forgotten is not a safety guard, so the
// only way past it is --force on the command line, visible at the call site
// every time.
func guard(inspector Inspector, root string, force bool) error {
	reason := inspector.ProtectedReason(root)
	if reason == "" {
		return nil
	}
	if force {
		return nil
	}
	return errors.New(errors.CodeInvalidInput,
		"refusing to operate on %s because %s", root, reason).
		WithHint("run this inside the specific directory you mean, or pass --force")
}

// contained confirms a destination stays inside the root.
//
// Every destination this module builds is derived from a file's own name, so
// escaping should be impossible. The check exists because "should be
// impossible" is how paths get out.
func contained(inspector Inspector, root, target string) error {
	inside, err := inspector.Contains(root, target)
	if err != nil {
		return err
	}
	if !inside {
		return errors.New(errors.CodePermissionDenied,
			"%s is outside %s", target, root)
	}
	return nil
}

// pathIdentity keys a path for comparison, so that two spellings of the same
// destination collide on a case-insensitive filesystem as well as a
// case-sensitive one.
func pathIdentity(path string) string {
	return fs.PathIdentity(path)
}

// normalizeExtension lowercases an extension and keeps the leading dot. Files
// with no extension return an empty string.
func normalizeExtension(extension string) string {
	extension = strings.ToLower(strings.TrimSpace(extension))
	if extension == "." {
		return ""
	}
	return extension
}

// NormalizeExtensionArgument accepts an extension with or without its dot, so
// both "--extension pdf" and "--extension .pdf" work.
func NormalizeExtensionArgument(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, ".") {
		value = "." + value
	}
	return normalizeExtension(value)
}
