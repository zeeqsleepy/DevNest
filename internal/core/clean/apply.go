package clean

import (
	"context"
	"path/filepath"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// ApplyRequest asks for the candidates under a root to be removed.
//
// It is the scan request plus one field, and that is deliberate: the removal
// runs its own scan rather than taking a plan built earlier. A plan is a
// description of a tree that has since had time to change, and re-deriving it
// costs a walk that has just been done anyway on a warm cache.
type ApplyRequest struct {
	ScanRequest
	// Confirmed records that the user was asked and said yes. The interface
	// layer owns the prompt; this field is how it reports the answer, and a
	// false value refuses the run rather than assuming.
	Confirmed bool
}

// Removal is one directory that was deleted.
type Removal struct {
	Path      string `json:"path"`
	Relative  string `json:"relative"`
	Ecosystem string `json:"ecosystem"`
	Bytes     int64  `json:"bytes"`
	Files     int    `json:"files"`
}

// Failure is one directory that could not be deleted, with the reason.
type Failure struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// ApplyResult is what a removal run did.
type ApplyResult struct {
	Root       string    `json:"root"`
	Removed    []Removal `json:"removed"`
	Failed     []Failure `json:"failed"`
	Skipped    []Skip    `json:"skipped"`
	Count      int       `json:"count"`
	BytesFreed int64     `json:"bytesFreed"`
	FilesFreed int       `json:"filesFreed"`
}

// Apply removes the candidates under a root.
//
// The order is the safety design: scan, then re-check each candidate against
// every guard immediately before removing it, then remove. Between the scan and
// the removal a directory can be replaced by a symlink, moved, or become the
// mount point of another filesystem, and the second check is what makes those
// races expensive to exploit rather than free.
//
// A failure on one directory is recorded and the run continues. The alternative
// is stopping halfway through, which leaves a tree in a state that is harder to
// reason about than either extreme.
func Apply(ctx context.Context, remover Remover, request ApplyRequest) (ApplyResult, error) {
	if !request.Confirmed {
		return ApplyResult{}, errors.New(errors.CodeInvalidInput,
			"this removal was not confirmed").
			WithHint("nothing has been deleted; this is a bug in the caller rather " +
				"than something you did")
	}

	found, err := Scan(ctx, remover, request.ScanRequest)
	if err != nil {
		return ApplyResult{}, err
	}

	protected, err := resolveProtected(remover, request.Protect)
	if err != nil {
		return ApplyResult{}, err
	}

	rootDevice, deviceKnown := remover.DeviceID(found.Root)

	result := ApplyResult{
		Root:    found.Root,
		Removed: []Removal{},
		Failed:  []Failure{},
		Skipped: found.Skipped,
	}

	for _, candidate := range found.Candidates {
		if err := ctx.Err(); err != nil {
			return result, errors.Wrap(err, errors.CodeCancelled,
				"cancelled after removing %d of %d", len(result.Removed), len(found.Candidates))
		}

		if reason := recheck(remover, found.Root, candidate, protected, rootDevice, deviceKnown); reason != "" {
			result.Skipped = append(result.Skipped, Skip{Path: candidate.Path, Reason: reason})
			continue
		}

		if err := remover.RemoveAll(candidate.Path); err != nil {
			result.Failed = append(result.Failed, Failure{
				Path:   candidate.Path,
				Reason: errors.Classify(err).Message,
			})
			continue
		}

		result.Removed = append(result.Removed, Removal{
			Path:      candidate.Path,
			Relative:  candidate.Relative,
			Ecosystem: candidate.Ecosystem,
			Bytes:     candidate.Bytes,
			Files:     candidate.Files,
		})
		result.BytesFreed += candidate.Bytes
		result.FilesFreed += candidate.Files
	}

	result.Count = len(result.Removed)
	return result, nil
}

// recheck applies every guard again to one candidate, in the moment before it
// is removed.
//
// It repeats work Scan already did, on purpose. The scan proves a tree was safe
// to clean at the time it ran; this proves this directory is safe to delete
// now, which is the only claim that matters when the next statement deletes it.
func recheck(remover Remover, root string, candidate Candidate, protected []string, rootDevice uint64, deviceKnown bool) string {
	resolved, err := remover.Resolve(candidate.Path)
	if err != nil {
		return "it could not be resolved a second time"
	}
	if fs.PathIdentity(resolved) != fs.PathIdentity(candidate.Path) {
		return "it now resolves somewhere else than when it was found"
	}

	entry, err := remover.Stat(resolved)
	if err != nil {
		return "it is no longer there"
	}
	if !entry.IsDir {
		return "it is no longer a directory"
	}
	if entry.IsSymlink {
		return "it is now a symbolic link"
	}
	if !sameName(filepath.Base(resolved), candidate.Name) {
		return "its name changed after it was found"
	}

	return guard(remover, root, resolved, protected, rootDevice, deviceKnown)
}

// sameName compares a directory name the way the filesystem does.
func sameName(current, found string) bool {
	return fs.PathIdentity(current) == fs.PathIdentity(found)
}
