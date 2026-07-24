// Package clean is DevNest's artifact removal module: finding build output and
// dependency directories, reporting what they cost, and deleting them when the
// user says so.
//
// This is the only module in DevNest that destroys data, and every decision in
// it is shaped by that.
//
// # Nothing is removed by a guess
//
// A directory is a candidate only when its name is in the built-in rule table
// or in the user's configuration, and only when the generic names are backed by
// a marker file beside them: "build" counts next to a package.json, and not in
// a photographs directory that happens to have one. Size, age, and emptiness
// are not evidence and are never used as such.
//
// # Finding and removing are different functions with different interfaces
//
// Scan takes an Inspector, which has no method that deletes. Apply takes a
// Remover. A caller cannot accidentally remove something by calling the wrong
// function, because the wrong function cannot remove anything.
//
// # Enumerate, then act
//
// Apply resolves and re-checks every candidate immediately before removing it,
// rather than trusting a plan built earlier. The tree can change between a scan
// and an apply, and the guards are cheap.
//
// # A failure does not stop the run
//
// A directory that cannot be removed, usually because a process is holding a
// file open, is recorded and the rest continue. Stopping halfway leaves a tree
// in a state nobody can reason about, and the user has to run the command again
// anyway.
package clean

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// alwaysSkipped are directories the walk never descends into, whatever the
// rules say. A version control directory is not build output, and a tool that
// deletes one has destroyed the only copy of the work.
var alwaysSkipped = []string{".git", ".hg", ".svn"}

// Candidate is one removable directory.
type Candidate struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
	// Relative is the path from the scan root, which is what a listing shows:
	// twenty absolute paths sharing a prefix are unreadable.
	Relative    string `json:"relative"`
	Bytes       int64  `json:"bytes"`
	Files       int    `json:"files"`
	Regenerable string `json:"regenerable"`
}

// Skip records something that matched a rule and was left alone anyway, with
// the reason. A guard that silently drops a candidate is indistinguishable
// from a bug.
type Skip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// ScanRequest describes what to look for and where.
type ScanRequest struct {
	// Root is the directory to search. It is resolved before anything else
	// happens and every candidate is checked to be inside it.
	Root string
	// Patterns narrows the run to named rules. Empty means every rule.
	Patterns []string
	// Configured are extra directory names from the user's configuration.
	Configured []string
	// Protect are paths that must never be touched, from configuration or
	// from --protect.
	Protect []string
	// Force lifts the refusal to run in a protected directory such as a home
	// directory or a filesystem root. It never comes from configuration.
	Force bool
	// IncludeHidden is not a flag: several rules name hidden directories
	// (.next, .tox), so the walk always includes them. The field exists to
	// say so where somebody would otherwise go looking for the option.
	IncludeHidden bool
}

// ScanResult is what was found.
type ScanResult struct {
	Root       string      `json:"root"`
	Candidates []Candidate `json:"candidates"`
	Skipped    []Skip      `json:"skipped"`
	Count      int         `json:"count"`
	TotalBytes int64       `json:"totalBytes"`
	TotalFiles int         `json:"totalFiles"`
}

// Scan finds removable directories under a root. It changes nothing.
func Scan(ctx context.Context, inspector Inspector, request ScanRequest) (ScanResult, error) {
	root, set, err := prepare(inspector, request)
	if err != nil {
		return ScanResult{}, err
	}

	protected, err := resolveProtected(inspector, request.Protect)
	if err != nil {
		return ScanResult{}, err
	}

	rootDevice, deviceKnown := inspector.DeviceID(root)

	result := ScanResult{Root: root, Candidates: []Candidate{}, Skipped: []Skip{}}
	matched := make(map[string]bool, 16)

	walk := fs.WalkOptions{
		Root:          root,
		IncludeDirs:   true,
		IncludeHidden: true,
		// Symlinks are never followed. A link inside a project pointing at a
		// shared cache elsewhere is exactly how a cleanup escapes the tree it
		// was told to clean.
		FollowSymlinks: false,
		Skip: func(path string, isDir bool) bool {
			if !isDir {
				return false
			}
			// Nothing below a matched directory is considered: the
			// node_modules inside a node_modules goes with its parent, and
			// counting it separately would double the reported total and
			// list a directory that is about to disappear anyway.
			if belowMatch(matched, path) {
				return true
			}
			return skippedName(filepath.Base(path))
		},
	}

	err = inspector.Walk(ctx, walk, func(entry fs.Entry) error {
		if !entry.IsDir || entry.Path == root {
			return nil
		}

		rule, found := set.match(entry.Name)
		if !found {
			return nil
		}
		matched[fs.PathIdentity(entry.Path)] = true

		if entry.IsSymlink {
			result.Skipped = append(result.Skipped, Skip{
				Path:   entry.Path,
				Reason: "it is a symbolic link, and removing one is not what the user meant",
			})
			return nil
		}

		siblings, err := namesBeside(ctx, inspector, filepath.Dir(entry.Path))
		if err != nil {
			return err
		}
		if !satisfied(rule, siblings) {
			result.Skipped = append(result.Skipped, Skip{
				Path: entry.Path,
				Reason: "nothing beside it says a project lives here, so the name " +
					"alone is not evidence",
			})
			return nil
		}

		if reason := guard(inspector, root, entry.Path, protected, rootDevice, deviceKnown); reason != "" {
			result.Skipped = append(result.Skipped, Skip{Path: entry.Path, Reason: reason})
			return nil
		}

		candidate, err := measure(ctx, inspector, root, entry.Path, rule)
		if err != nil {
			return err
		}
		result.Candidates = append(result.Candidates, candidate)
		return nil
	})
	if err != nil {
		return ScanResult{}, err
	}

	// Largest first: the point of the command is reclaiming space, and the
	// first line should be the one worth acting on.
	sort.Slice(result.Candidates, func(first, second int) bool {
		left, right := result.Candidates[first], result.Candidates[second]
		if left.Bytes != right.Bytes {
			return left.Bytes > right.Bytes
		}
		return left.Path < right.Path
	})

	for _, candidate := range result.Candidates {
		result.TotalBytes += candidate.Bytes
		result.TotalFiles += candidate.Files
	}
	result.Count = len(result.Candidates)

	return result, nil
}

// prepare resolves the root, applies the protected-path guard, and builds the
// rules. Everything that can refuse the whole run happens here, before a single
// directory is looked at.
func prepare(inspector Inspector, request ScanRequest) (string, *ruleSet, error) {
	if strings.TrimSpace(request.Root) == "" {
		request.Root = "."
	}

	root, err := inspector.Resolve(request.Root)
	if err != nil {
		return "", nil, err
	}

	entry, err := inspector.Stat(root)
	if err != nil {
		return "", nil, err
	}
	if !entry.IsDir {
		return "", nil, errors.New(errors.CodeInvalidInput, "%s is not a directory", root).
			WithHint("point this at a project directory")
	}

	if reason := inspector.ProtectedReason(root); reason != "" && !request.Force {
		return "", nil, errors.New(errors.CodeInvalidInput,
			"refusing to search %s because %s", root, reason).
			WithHint("this guard exists because a mistyped path here is an incident; " +
				"pass --force if you meant it")
	}

	set := newRuleSet(request.Patterns, request.Configured)
	if missing := set.unknown(request.Patterns); len(missing) > 0 {
		return "", nil, errors.New(errors.CodeInvalidInput,
			"no rule named %s", strings.Join(missing, ", ")).
			WithHint("run \"devnest clean rules\" to see the names")
	}
	if len(set.byName) == 0 {
		return "", nil, errors.New(errors.CodeInvalidInput, "no rules are in effect")
	}

	return root, set, nil
}

// guard applies the per-candidate refusals. It returns the reason to skip, or
// an empty string when the candidate may be removed.
func guard(inspector Inspector, root, path string, protected []string, rootDevice uint64, deviceKnown bool) string {
	inside, err := inspector.Contains(root, path)
	if err != nil || !inside {
		return "it resolved to somewhere outside the directory being cleaned"
	}

	for _, safe := range protected {
		if fs.PathIdentity(safe) == fs.PathIdentity(path) {
			return "it is protected"
		}
		if within, err := inspector.Contains(safe, path); err == nil && within {
			return "it is inside a protected path"
		}
	}

	if reason := inspector.ProtectedReason(path); reason != "" {
		return reason
	}

	if deviceKnown {
		if device, known := inspector.DeviceID(path); known && device != rootDevice {
			return "it is on a different filesystem, which a cleanup should not cross"
		}
	}

	return ""
}

// resolveProtected turns the configured and flagged protections into resolved
// paths, so a comparison is between two things of the same kind.
func resolveProtected(inspector Inspector, paths []string) ([]string, error) {
	resolved := make([]string, 0, len(paths))

	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		full, err := inspector.Resolve(path)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, full)
	}
	return resolved, nil
}

// namesBeside lists the file names in a directory, for the marker check.
func namesBeside(ctx context.Context, inspector Inspector, directory string) ([]string, error) {
	names := make([]string, 0, 16)

	err := inspector.Walk(ctx, fs.WalkOptions{
		Root:          directory,
		MaxDepth:      1,
		IncludeHidden: true,
	}, func(entry fs.Entry) error {
		if !entry.IsDir {
			names = append(names, entry.Name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return names, nil
}

// measure walks a candidate to find out what removing it would reclaim.
func measure(ctx context.Context, inspector Inspector, root, path string, rule Rule) (Candidate, error) {
	candidate := Candidate{
		Path:        path,
		Name:        filepath.Base(path),
		Ecosystem:   rule.Ecosystem,
		Regenerable: rule.Regenerable,
	}

	if relative, err := filepath.Rel(root, path); err == nil {
		candidate.Relative = filepath.ToSlash(relative)
	}

	err := inspector.Walk(ctx, fs.WalkOptions{
		Root:          path,
		IncludeHidden: true,
	}, func(entry fs.Entry) error {
		if entry.IsDir {
			return nil
		}
		candidate.Files++
		candidate.Bytes += entry.Bytes
		return nil
	})
	if err != nil {
		return Candidate{}, err
	}

	return candidate, nil
}

// belowMatch reports whether any directory above this one already matched a
// rule.
//
// The walk consults this before descending, so a matched directory is entered
// once, its immediate children are rejected, and nothing deeper is read at
// all. That is the difference between listing a node_modules and reading every
// file in one.
func belowMatch(matched map[string]bool, path string) bool {
	for parent := filepath.Dir(path); parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		if matched[fs.PathIdentity(parent)] {
			return true
		}
	}
	return false
}

func skippedName(name string) bool {
	for _, skipped := range alwaysSkipped {
		if strings.EqualFold(name, skipped) {
			return true
		}
	}
	return false
}
