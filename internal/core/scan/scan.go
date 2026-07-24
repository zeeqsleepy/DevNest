// Package scan is DevNest's project analysis module: what a directory tree is
// made of.
//
// Read-only throughout. Nothing here writes, moves, or removes anything.
//
// # Ignoring what a project ignores
//
// A scan that counts node_modules answers a question nobody asked. Four
// hundred thousand files, of which four hundred are the project, and every
// figure in the report is about somebody else's code.
//
// So the walk skips what the project already ignores: the rules in
// .gitignore, plus the vendor and build directories that every ecosystem has
// whether or not they are written down. The skipping happens before a
// directory is read, not after, which is the difference between a scan that
// takes a second and one that takes a minute. `--no-ignore` turns it off for
// the times you want the whole truth.
//
// # Classification is somebody else's table
//
// Whether a path is source, a test, generated, or vendored is decided in
// internal/classify, below the module layer, because `clean` and `secret` will
// need the same answers and modules may not import each other.
//
// # Size reporting lives elsewhere
//
// "Where did the disk space go" is `devnest file size`, and this module does
// not duplicate it. What it reports is shape: how many files, of what kinds,
// in what languages, and how much of the tree is code somebody wrote.
package scan

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/devnest/devnest/internal/classify"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// Selection is the walk every scan command shares.
type Selection struct {
	// Root is the directory to scan. Empty means the current one.
	Root string
	// MaxDepth limits how deep the walk goes. Zero means unlimited.
	MaxDepth int
	// IncludeHidden includes dotfiles and, on Windows, hidden entries.
	IncludeHidden bool
	// FollowSymlinks descends into symlinked directories, with cycle
	// detection.
	FollowSymlinks bool
	// Exclude holds glob patterns matched against an entry's name.
	Exclude []string
	// NoIgnore disregards .gitignore and the built-in vendor and build
	// directory rules, reporting the tree exactly as it is on disk.
	NoIgnore bool
}

// Problem is an entry the walk could not read.
//
// A tree with one unreadable directory in it is still worth reporting on, so
// these are collected and returned rather than ending the scan.
type Problem struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// walkContext is the resolved root and the options derived from a Selection,
// shared by every command in the module so they all skip the same things.
type walkContext struct {
	root     string
	options  fs.WalkOptions
	problems []Problem
	ignore   *ignoreSet
}

// prepare resolves the root and builds the walk options.
//
// Every path decision is made against the resolved root rather than what the
// user typed, so a relative path, a symlink, and a trailing separator all
// describe the same scan.
func prepare(ctx context.Context, inspector Inspector, selection Selection) (*walkContext, error) {
	root := selection.Root
	if strings.TrimSpace(root) == "" {
		root = "."
	}

	resolved, err := inspector.Resolve(root)
	if err != nil {
		return nil, err
	}

	entry, err := inspector.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !entry.IsDir {
		return nil, errors.New(errors.CodeInvalidInput, "%s is not a directory", resolved).
			WithHint("pass the directory to scan; for one file, try \"devnest file hash\"")
	}

	walk := &walkContext{root: resolved}

	if !selection.NoIgnore {
		walk.ignore = loadIgnore(ctx, inspector, resolved)
	}

	walk.options = fs.WalkOptions{
		Root:           resolved,
		MaxDepth:       selection.MaxDepth,
		IncludeHidden:  selection.IncludeHidden,
		FollowSymlinks: selection.FollowSymlinks,
		Exclude:        selection.Exclude,
		Skip:           walk.skip,
		OnProblem: func(path string, err error) {
			walk.problems = append(walk.problems, Problem{
				Path:   walk.relative(path),
				Reason: errors.Classify(err).Message,
			})
		},
	}
	return walk, nil
}

// skip decides whether an entry is left out of the walk.
//
// The .git directory is skipped whatever the settings say. It is not a
// scanning target: it holds thousands of objects with no extensions, and
// counting them tells you nothing about the project except that it is a
// repository, which the presence of .git already said.
func (w *walkContext) skip(path string, isDir bool) bool {
	name := filepath.Base(path)

	if isDir && name == ".git" {
		return true
	}
	if w.ignore == nil {
		return false
	}
	if isDir && (classify.IsVendoredDirectory(name) || classify.IsBuildDirectory(name)) {
		return true
	}
	return w.ignore.matches(w.relative(path), isDir)
}

// relative returns a path relative to the scan root, in slash form, which is
// what every rule and every report uses.
func (w *walkContext) relative(path string) string {
	relative, err := filepath.Rel(w.root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

// collected returns the problems in a form safe to serialise: never nil, so a
// consumer does not have to null-check before iterating.
func (w *walkContext) collected() []Problem {
	if w.problems == nil {
		return []Problem{}
	}
	return w.problems
}
