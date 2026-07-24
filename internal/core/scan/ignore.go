package scan

import (
	"bufio"
	"context"
	"io"
	"path/filepath"
	"strings"
)

// ignoreSet is the .gitignore rules for one tree.
//
// # What is supported, and what is not
//
// This reads the root .gitignore and applies the parts of the format that
// decide what a scan sees: comments and blank lines, negation with "!",
// directory-only rules ending in "/", anchoring with a leading "/", the "**"
// wildcard, and plain glob matching on either the base name or the whole
// relative path.
//
// It does not read nested .gitignore files, .git/info/exclude, or the global
// excludes file, and it does not know what is already tracked. Those are the
// parts of the format that need a repository rather than a directory, and this
// module works on any directory including one that is not a repository.
//
// The gap is stated here rather than hidden because "why is this file in the
// count" has to have an answer. A scan of a repository with per-directory
// ignore files will include a little more than git would, and `--exclude`
// covers the difference.
type ignoreSet struct {
	rules []ignoreRule
}

type ignoreRule struct {
	// pattern is the rule with its markers stripped.
	pattern string
	// negate re-includes a path an earlier rule excluded.
	negate bool
	// directoryOnly means the rule matches directories only.
	directoryOnly bool
	// anchored means the rule is relative to the root rather than matching
	// at any depth.
	anchored bool
}

// ignoreFileLimit caps how much of a .gitignore is read. A rules file larger
// than this is not a rules file.
const ignoreFileLimit = 256 * 1024

// loadIgnore reads the root .gitignore, if there is one.
//
// A missing or unreadable file means no rules rather than an error. Scanning a
// directory that is not a repository is an ordinary thing to do.
func loadIgnore(ctx context.Context, inspector Inspector, root string) *ignoreSet {
	if err := ctx.Err(); err != nil {
		return &ignoreSet{}
	}

	path := filepath.Join(root, ".gitignore")
	if entry, err := inspector.Stat(path); err != nil || entry.IsDir {
		return &ignoreSet{}
	}

	file, err := inspector.Open(path)
	if err != nil {
		return &ignoreSet{}
	}
	defer func() {
		// Opened for reading only, so a failed close cannot lose anything.
		_ = file.Close()
	}()

	return parseIgnore(io.LimitReader(file, ignoreFileLimit))
}

// parseIgnore turns the contents of a .gitignore into rules.
func parseIgnore(reader io.Reader) *ignoreSet {
	set := &ignoreSet{}
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		rule := ignoreRule{pattern: line}

		if strings.HasPrefix(rule.pattern, "!") {
			rule.negate = true
			rule.pattern = rule.pattern[1:]
		}
		if strings.HasSuffix(rule.pattern, "/") {
			rule.directoryOnly = true
			rule.pattern = strings.TrimSuffix(rule.pattern, "/")
		}
		if strings.HasPrefix(rule.pattern, "/") {
			rule.anchored = true
			rule.pattern = strings.TrimPrefix(rule.pattern, "/")
		} else if strings.Contains(rule.pattern, "/") {
			// A rule with a separator anywhere but the end is relative to
			// the file it appears in, which for a root .gitignore is the
			// root. "docs/build" means that path, not any "build" inside
			// any "docs".
			rule.anchored = true
		}

		if rule.pattern != "" {
			set.rules = append(set.rules, rule)
		}
	}
	return set
}

// matches reports whether a path is ignored.
//
// Later rules win, which is what makes negation work: the last rule to match
// decides, so "build/" followed by "!build/keep.txt" keeps that one file.
func (i *ignoreSet) matches(relative string, isDir bool) bool {
	if i == nil || len(i.rules) == 0 || relative == "" || relative == "." {
		return false
	}

	ignored := false
	for _, rule := range i.rules {
		if rule.directoryOnly && !isDir {
			continue
		}
		if rule.matches(relative) {
			ignored = !rule.negate
		}
	}
	return ignored
}

// matches reports whether one rule covers a path.
//
// Anchoring is the whole of the difference, and getting it wrong is not
// subtle: "/devnest" means the one at the root, and a matcher that also
// accepted cmd/devnest would quietly drop a source directory out of every
// report.
func (r ignoreRule) matches(relative string) bool {
	if r.anchored {
		return matchPath(r.pattern, relative)
	}

	// An unanchored rule with no separator in it matches a name at any
	// depth, so it is tried against every component: "build" covers build/
	// at the root and lib/build/ below it, which is what git does.
	for _, segment := range strings.Split(relative, "/") {
		if matchPath(r.pattern, segment) {
			return true
		}
	}
	return false
}

// matchPath applies one pattern to one path, as a whole.
//
// "**" is handled here rather than delegated, because filepath.Match has no
// concept of it: its "*" never crosses a separator, which is the whole point
// of "**" existing.
func matchPath(pattern, path string) bool {
	if !strings.Contains(pattern, "**") {
		matched, err := filepath.Match(pattern, path)
		return err == nil && matched
	}

	prefix, suffix, _ := strings.Cut(pattern, "**")
	prefix = strings.TrimSuffix(prefix, "/")
	suffix = strings.TrimPrefix(suffix, "/")

	if prefix != "" {
		if !strings.HasPrefix(path, prefix) {
			return false
		}
		path = strings.TrimPrefix(strings.TrimPrefix(path, prefix), "/")
	}
	if suffix == "" {
		return true
	}

	if matchPath(suffix, path) {
		return true
	}
	for index, character := range path {
		if character == '/' && matchPath(suffix, path[index+1:]) {
			return true
		}
	}
	return false
}
