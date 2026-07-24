package env

import (
	"context"
	"sort"

	"github.com/devnest/devnest/internal/platform/fs"
	"github.com/devnest/devnest/internal/platform/proc"
)

// PathProblem names something wrong with a PATH entry.
type PathProblem string

const (
	// ProblemDuplicate is the same directory listed more than once.
	ProblemDuplicate PathProblem = "duplicate"
	// ProblemMissing is an entry pointing at nothing.
	ProblemMissing PathProblem = "missing"
	// ProblemNotDirectory is an entry pointing at a file.
	ProblemNotDirectory PathProblem = "not-a-directory"
	// ProblemUnreadable is an entry that cannot be read.
	ProblemUnreadable PathProblem = "unreadable"
)

// PathEntry is one directory on PATH.
type PathEntry struct {
	Position int           `json:"position"`
	Path     string        `json:"path"`
	Problems []PathProblem `json:"problems"`
	// Executables is how many runnable files the directory holds. A PATH
	// entry with none is not broken, but it is usually left over.
	Executables int `json:"executables"`
}

// Shadow is one executable name resolvable from more than one PATH entry.
type Shadow struct {
	Name string `json:"name"`
	// Winner is the location that actually runs.
	Winner string `json:"winner"`
	// Hidden are the copies earlier lookups will never reach.
	Hidden []string `json:"hidden"`
}

// PathRequest describes one PATH inspection.
type PathRequest struct {
	// Shadows turns on the shadowed-executable check, which has to read
	// every directory on PATH and is therefore the expensive half.
	Shadows bool
}

// PathResult is what the inspection found.
type PathResult struct {
	Entries  []PathEntry `json:"entries"`
	Shadowed []Shadow    `json:"shadowed"`
	Problems int         `json:"problems"`
}

// InspectPath reports what is wrong with PATH.
//
// Four kinds of wrong, in order of how often they explain a real complaint:
// a shadowed executable, a duplicate entry, an entry pointing at nothing, and
// an entry pointing at a file. The first is the one that explains most "but I
// installed the new version" reports, and it is the only one that costs
// anything to find.
//
// A problem is a finding, not an error. A PATH with three dead entries works
// perfectly well; the command's job is to say so.
func InspectPath(ctx context.Context, deps Locator, request PathRequest) (PathResult, error) {
	result := PathResult{Entries: []PathEntry{}, Shadowed: []Shadow{}}

	seen := make(map[string]bool)
	locations := make(map[string][]string)

	for position, path := range deps.PathEntries() {
		if err := ctx.Err(); err != nil {
			return PathResult{}, err
		}

		entry := PathEntry{Position: position + 1, Path: path, Problems: []PathProblem{}}

		key := fs.PathIdentity(path)
		if seen[key] {
			entry.Problems = append(entry.Problems, ProblemDuplicate)
		}
		seen[key] = true

		described, err := deps.Stat(path)
		switch {
		case err != nil:
			entry.Problems = append(entry.Problems, ProblemUnreadable)
		case !described.Exists:
			entry.Problems = append(entry.Problems, ProblemMissing)
		case !described.IsDir:
			entry.Problems = append(entry.Problems, ProblemNotDirectory)
		default:
			entry.Executables = record(deps.Executables(path), request.Shadows, locations)
		}

		result.Problems += len(entry.Problems)
		result.Entries = append(result.Entries, entry)
	}

	if request.Shadows {
		result.Shadowed = shadows(locations)
	}
	return result, nil
}

// record counts a directory's executables, and keeps track of where each name
// was found when the shadow check needs it.
//
// The listing is read once and used for both. Reading every directory on PATH
// twice to answer two questions about the same files is the kind of waste that
// turns a fast command into a slow one.
//
// What counts as an executable, and what name a shell would use to run it, are
// decided in the platform layer. Both differ by operating system, and nothing
// above that layer should have to know how.
func record(executables []proc.Executable, shadows bool, locations map[string][]string) int {
	if !shadows {
		return len(executables)
	}
	for _, executable := range executables {
		locations[executable.Name] = append(locations[executable.Name], executable.Path)
	}
	return len(executables)
}

// shadows reports the names resolvable from more than one place.
func shadows(locations map[string][]string) []Shadow {
	found := make([]Shadow, 0)

	for name, paths := range locations {
		if len(paths) < 2 {
			continue
		}
		found = append(found, Shadow{Name: name, Winner: paths[0], Hidden: paths[1:]})
	}

	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })
	return found
}
