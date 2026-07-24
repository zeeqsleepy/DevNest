package file

import (
	"context"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// Grouping selects the folder layout organize produces.
type Grouping string

const (
	// GroupByCategory produces Images/jpg/photo.jpg: a category folder with
	// an extension folder inside it.
	GroupByCategory Grouping = "category"
	// GroupByExtension produces jpg/photo.jpg: one flat folder per
	// extension.
	GroupByExtension Grouping = "extension"
)

// Conflict decides what happens when a destination is already taken.
type Conflict string

const (
	// ConflictSkip leaves the file where it is and records why. This is the
	// default: it never invents a name and never loses anything.
	ConflictSkip Conflict = "skip"
	// ConflictRename appends a counter, producing "photo (2).jpg".
	ConflictRename Conflict = "rename"
	// ConflictFail aborts the whole operation before anything moves.
	ConflictFail Conflict = "fail"
)

// ParseGrouping resolves the --by flag.
func ParseGrouping(name string) (Grouping, error) {
	switch Grouping(strings.ToLower(strings.TrimSpace(name))) {
	case GroupByCategory:
		return GroupByCategory, nil
	case GroupByExtension:
		return GroupByExtension, nil
	}
	return "", errors.New(errors.CodeInvalidInput, "unknown grouping %q", name).
		WithHint("expected one of: category, extension")
}

// ParseConflict resolves the --on-conflict flag.
func ParseConflict(name string) (Conflict, error) {
	switch Conflict(strings.ToLower(strings.TrimSpace(name))) {
	case ConflictSkip:
		return ConflictSkip, nil
	case ConflictRename:
		return ConflictRename, nil
	case ConflictFail:
		return ConflictFail, nil
	}
	return "", errors.New(errors.CodeInvalidInput, "unknown conflict policy %q", name).
		WithHint("expected one of: skip, rename, fail")
}

// OrganizeRequest describes one organise operation.
type OrganizeRequest struct {
	Selection
	// Grouping selects the folder layout.
	Grouping Grouping
	// OnConflict decides what to do about a taken destination.
	OnConflict Conflict
	// Apply performs the moves. Without it the result is a plan and the disk
	// is untouched.
	Apply bool
	// Force permits running at a protected path.
	Force bool
}

// Move statuses.
const (
	MovePlanned = "planned"
	MoveDone    = "moved"
	MoveSkipped = "skipped"
	MoveFailed  = "failed"
)

// Move is one file's journey, planned or performed.
type Move struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Category    string `json:"category"`
	Extension   string `json:"extension"`
	Bytes       int64  `json:"bytes"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
}

// FolderSummary counts what lands in one destination folder.
type FolderSummary struct {
	Folder string `json:"folder"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
}

// OrganizeResult is the plan, or the record of what was done.
type OrganizeResult struct {
	Root     string          `json:"root"`
	Applied  bool            `json:"applied"`
	Grouping string          `json:"grouping"`
	Moves    []Move          `json:"moves"`
	Folders  []FolderSummary `json:"folders"`
	Planned  int             `json:"planned"`
	Moved    int             `json:"moved"`
	Skipped  int             `json:"skipped"`
	Failed   int             `json:"failed"`
	Bytes    int64           `json:"bytes"`
	Problems []Problem       `json:"problems"`
}

// Organize groups the files under a directory into folders by category or by
// extension.
//
// It runs in two passes. The first only reads: it walks the tree, works out
// every destination, and resolves conflicts. The second performs the moves,
// and only when Apply is set. Enumerating while mutating the same tree
// produces behaviour nobody can reason about afterwards, least of all during a
// bug report.
func Organize(ctx context.Context, mover Mover, request OrganizeRequest) (OrganizeResult, error) {
	if request.Grouping == "" {
		request.Grouping = GroupByCategory
	}
	if request.OnConflict == "" {
		request.OnConflict = ConflictSkip
	}

	walk, err := prepare(mover, request.Selection)
	if err != nil {
		return OrganizeResult{}, err
	}
	if err := guard(mover, walk.root, request.Force); err != nil {
		return OrganizeResult{}, err
	}

	files, err := walk.collect(ctx, mover, nil)
	if err != nil {
		return OrganizeResult{}, err
	}

	result := OrganizeResult{
		Root:     walk.root,
		Applied:  request.Apply,
		Grouping: string(request.Grouping),
		Moves:    []Move{},
		Folders:  []FolderSummary{},
		Problems: walk.problems,
	}

	moves, err := planMoves(mover, walk.root, request, files)
	if err != nil {
		return OrganizeResult{}, err
	}
	result.Moves = moves

	if request.Apply {
		applyMoves(ctx, mover, result.Moves)
	}

	summarise(&result)
	return result, nil
}

// planMoves computes every destination without touching the disk.
func planMoves(mover Mover, root string, request OrganizeRequest, files []Info) ([]Move, error) {
	// taken holds destinations claimed earlier in this plan, so two files
	// heading for the same name collide during planning rather than during
	// execution.
	taken := make(map[string]bool, len(files))
	moves := make([]Move, 0, len(files))

	for _, file := range files {
		move := Move{
			Source:    file.Path,
			Category:  file.Category,
			Extension: file.Extension,
			Bytes:     file.Bytes,
			Status:    MovePlanned,
		}

		folder := destinationFolder(root, request.Grouping, file)

		// A file already sitting in its own destination folder is left alone.
		// Running organize twice must be the same as running it once.
		if filepath.Dir(file.Path) == folder {
			move.Status = MoveSkipped
			move.Reason = "already organised"
			move.Destination = file.Path
			moves = append(moves, move)
			continue
		}

		destination, err := resolveDestination(mover, folder, file.Name, taken, request.OnConflict)
		if err != nil {
			return nil, err
		}
		if destination == "" {
			move.Status = MoveSkipped
			move.Reason = "a file with that name is already there"
			moves = append(moves, move)
			continue
		}

		if err := contained(mover, root, destination); err != nil {
			return nil, err
		}

		taken[pathIdentity(destination)] = true
		move.Destination = destination
		moves = append(moves, move)
	}

	return moves, nil
}

// destinationFolder builds the folder a file belongs in.
func destinationFolder(root string, grouping Grouping, file Info) string {
	if grouping == GroupByExtension {
		return filepath.Join(root, folderFor(file.Extension))
	}
	return filepath.Join(root, file.Category, folderFor(file.Extension))
}

// resolveDestination applies the conflict policy. An empty return means the
// file should be skipped.
func resolveDestination(
	mover Mover,
	folder, name string,
	taken map[string]bool,
	policy Conflict,
) (string, error) {
	candidate := filepath.Join(folder, name)

	free, err := available(mover, candidate, taken)
	if err != nil {
		return "", err
	}
	if free {
		return candidate, nil
	}

	switch policy {
	case ConflictFail:
		return "", errors.New(errors.CodeConflict,
			"%s already exists", candidate).
			WithHint("pass --on-conflict skip to leave those files alone, " +
				"or --on-conflict rename to number them")
	case ConflictRename:
		return numberedDestination(mover, folder, name, taken)
	default:
		return "", nil
	}
}

// numberedDestination finds "photo (2).jpg", then "photo (3).jpg", and so on.
func numberedDestination(mover Mover, folder, name string, taken map[string]bool) (string, error) {
	extension := filepath.Ext(name)
	stem := strings.TrimSuffix(name, extension)

	// A file that needs more than a thousand numbered variants is a sign that
	// something is wrong with the request, not that the loop should run longer.
	for counter := 2; counter < 1000; counter++ {
		candidate := filepath.Join(folder, stem+" ("+strconv.Itoa(counter)+")"+extension)
		free, err := available(mover, candidate, taken)
		if err != nil {
			return "", err
		}
		if free {
			return candidate, nil
		}
	}

	return "", errors.New(errors.CodeConflict,
		"cannot find a free name for %s in %s", name, folder)
}

func available(mover Mover, candidate string, taken map[string]bool) (bool, error) {
	if taken[pathIdentity(candidate)] {
		return false, nil
	}
	exists, err := mover.Exists(candidate)
	if err != nil {
		return false, err
	}
	return !exists, nil
}

// applyMoves performs the plan, one file at a time.
//
// A failure on one file does not abort the rest. Stopping halfway through
// leaves the directory in a state that is harder to reason about than
// finishing and reporting exactly which files did not make it.
func applyMoves(ctx context.Context, mover Mover, moves []Move) {
	for index := range moves {
		move := &moves[index]
		if move.Status != MovePlanned {
			continue
		}

		// Cancellation is observed between files, never inside one, so an
		// interrupt cannot leave a half-moved file.
		if err := ctx.Err(); err != nil {
			move.Status = MoveSkipped
			move.Reason = "cancelled"
			continue
		}

		if err := mover.EnsureDir(filepath.Dir(move.Destination)); err != nil {
			move.Status = MoveFailed
			move.Reason = errors.Classify(err).Message
			continue
		}
		if err := mover.Move(move.Source, move.Destination); err != nil {
			move.Status = MoveFailed
			move.Reason = errors.Classify(err).Message
			continue
		}
		move.Status = MoveDone
	}
}

// summarise counts the outcome and groups it by destination folder.
func summarise(result *OrganizeResult) {
	folders := make(map[string]*FolderSummary)

	for _, move := range result.Moves {
		switch move.Status {
		case MovePlanned:
			result.Planned++
		case MoveDone:
			result.Moved++
		case MoveSkipped:
			result.Skipped++
			continue
		case MoveFailed:
			result.Failed++
			continue
		}

		result.Bytes += move.Bytes

		name, err := filepath.Rel(result.Root, filepath.Dir(move.Destination))
		if err != nil {
			name = filepath.Dir(move.Destination)
		}
		name = filepath.ToSlash(name)

		summary, seen := folders[name]
		if !seen {
			summary = &FolderSummary{Folder: name}
			folders[name] = summary
		}
		summary.Files++
		summary.Bytes += move.Bytes
	}

	result.Folders = make([]FolderSummary, 0, len(folders))
	for _, summary := range folders {
		result.Folders = append(result.Folders, *summary)
	}
	sort.Slice(result.Folders, func(i, j int) bool {
		if result.Folders[i].Bytes != result.Folders[j].Bytes {
			return result.Folders[i].Bytes > result.Folders[j].Bytes
		}
		return result.Folders[i].Folder < result.Folders[j].Folder
	})

	if result.Problems == nil {
		result.Problems = []Problem{}
	}
}
