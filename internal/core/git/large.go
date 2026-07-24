package git

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

// defaultLargest is how many objects the report lists when the caller does not
// say. Ten is what fits on a screen and what answers the question.
const defaultLargest = 10

// LargeRequest asks what is making a repository big.
type LargeRequest struct {
	Path string
	// Limit caps how many objects are reported. Zero means defaultLargest.
	Limit int
}

// Object is one blob in the history, with the path it was last seen at.
//
// Path is the name the object had in the object listing, which is where it
// lives now or lived most recently. A file that was deleted years ago is still
// in the history and still costs the same to clone, which is the entire point
// of this report.
type Object struct {
	Hash  string `json:"hash"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// LargeResult is the report.
type LargeResult struct {
	Root    string   `json:"root"`
	Objects []Object `json:"objects"`
	Count   int      `json:"count"`
	// TotalBytes is the size of the listed objects, not of the repository. A
	// reader comparing it against the size of .git would otherwise be
	// comparing two different things.
	TotalBytes int64 `json:"totalBytes"`
	// Scanned is how many objects were examined, so a small result from a
	// large repository is visibly a ranking rather than a whole story.
	Scanned int `json:"scanned"`
	// Truncated says the object listing itself was cut short by the output
	// limit, which happens on a genuinely enormous repository.
	Truncated bool `json:"truncated"`
}

// Large reports the biggest objects in a repository's history.
//
// Two commands, because git has no single one that answers this: rev-list
// names every object reachable from every ref, and cat-file --batch-check
// reports the type and size of each. The list is piped from the first into the
// second by this code rather than by a shell, which is why the object names are
// carried across in a map instead of on a pipeline.
//
// This is the slowest command in the module and the timeout reflects that. On a
// repository with a long history it walks every object once.
func Large(ctx context.Context, runner Runner, locator Locator, request LargeRequest) (LargeResult, error) {
	repository, err := open(ctx, runner, locator, request.Path)
	if err != nil {
		return LargeResult{}, err
	}

	limit := request.Limit
	if limit <= 0 {
		limit = defaultLargest
	}

	paths, err := objectPaths(ctx, repository)
	if err != nil {
		return LargeResult{}, err
	}

	result := LargeResult{Root: repository.Root, Objects: []Object{}, Scanned: len(paths)}
	if len(paths) == 0 {
		return result, nil
	}

	sizes, err := objectSizes(ctx, repository)
	if err != nil {
		return LargeResult{}, err
	}

	found := make([]Object, 0, len(paths))
	for hash, size := range sizes {
		// An object with no path is unreachable from any ref: a leftover from
		// a rebase or a fetch that is waiting to be garbage collected. It
		// still occupies space, but it is not what somebody asking "what is
		// making this repository large" is looking for, and naming it without
		// a path would be a row nobody can act on.
		path, reachable := paths[hash]
		if !reachable {
			continue
		}
		found = append(found, Object{Hash: hash, Path: path, Bytes: size})
	}

	// Largest first, then by hash so that two objects of the same size always
	// come out in the same order.
	sort.Slice(found, func(first, second int) bool {
		left, right := found[first], found[second]
		if left.Bytes != right.Bytes {
			return left.Bytes > right.Bytes
		}
		return left.Hash < right.Hash
	})

	if len(found) > limit {
		found = found[:limit]
	}

	result.Objects = found
	result.Count = len(found)
	for _, object := range found {
		result.TotalBytes += object.Bytes
	}

	return result, nil
}

// objectPaths lists every reachable object and the path it is known by.
//
// An object appearing under several paths over its life keeps the first one
// seen, which is the most recent: rev-list walks newest first. Reporting one
// path is the honest simplification, and the hash is in the result for anyone
// who needs to go deeper.
func objectPaths(ctx context.Context, repository *repository) (map[string]string, error) {
	lines, err := repository.lines(ctx, walkTimeout, "rev-list", "--objects", "--all")
	if err != nil {
		return nil, err
	}

	paths := make(map[string]string, len(lines))

	for _, line := range lines {
		hash, path, _ := strings.Cut(strings.TrimSpace(line), " ")
		if len(hash) < 7 {
			continue
		}
		if _, seen := paths[hash]; seen {
			continue
		}
		paths[hash] = strings.TrimSpace(path)
	}

	return paths, nil
}

// objectSizes asks git for the size of every object, in one invocation.
//
// cat-file --batch-check normally reads object names from standard input, and
// nothing in the platform layer writes to a process's input: a command here is
// an argument vector and its output, deliberately, because that is the shape
// with no shell in it. --batch-all-objects sidesteps the question by reporting
// on every object in the repository, which is a superset of what is wanted.
// The unreachable extras are filtered out by the caller, which knows which
// objects have a path.
func objectSizes(ctx context.Context, repository *repository) (map[string]int64, error) {
	lines, err := repository.lines(ctx, walkTimeout,
		"cat-file", "--batch-all-objects",
		"--batch-check=%(objectname) %(objecttype) %(objectsize)")
	if err != nil {
		return nil, err
	}

	sizes := make(map[string]int64, len(lines))
	for _, line := range lines {
		if hash, size, ok := parseBatchLine(line); ok {
			sizes[hash] = size
		}
	}

	return sizes, nil
}

// parseBatchLine reads one "<hash> <type> <size>" record, keeping blobs only.
//
// Trees and commits are objects too, and they are small and uninteresting: a
// report of what is making a repository large is a report about file content.
func parseBatchLine(line string) (string, int64, bool) {
	parts := strings.Fields(line)
	if len(parts) != 3 || parts[1] != "blob" {
		return "", 0, false
	}

	size, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return parts[0], size, true
}
