package file

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// Replacement is one literal text substitution applied to a file name.
type Replacement struct {
	From string
	To   string
}

// Sequence adds a running number to each name.
type Sequence struct {
	// Enabled turns numbering on.
	Enabled bool
	// Start is the first number.
	Start int
	// Padding is the minimum width, zero-filled. Four gives 0001.
	Padding int
	// Separator sits between the number and the rest of the name.
	Separator string
	// Position places the number before or after the name.
	Position string
}

// Sequence positions.
const (
	SequenceBefore = "prefix"
	SequenceAfter  = "suffix"
)

// RenameRequest describes one batch rename.
//
// Transforms are applied to the name without its extension, in a fixed order:
// replacements, then the sequence number, then the prefix and suffix. The
// order is fixed rather than configurable so that the same flags always
// produce the same names.
type RenameRequest struct {
	Selection
	// Match is a glob applied to base names, selecting what to rename.
	Match string
	// Prefix is prepended to every name.
	Prefix string
	// Suffix is appended before the extension.
	Suffix string
	// Replace holds literal substitutions, applied in order.
	Replace []Replacement
	// Sequence adds a running number.
	Sequence Sequence
	// Lowercase and Uppercase change the case of the name.
	Lowercase bool
	Uppercase bool
	// Apply performs the renames. Without it the result is a preview.
	Apply bool
	// Force permits running at a protected path.
	Force bool
}

// Rename statuses.
const (
	RenamePlanned   = "planned"
	RenameDone      = "renamed"
	RenameUnchanged = "unchanged"
	RenameFailed    = "failed"
)

// Rename is one file's old and new name.
type Rename struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	OldName     string `json:"oldName"`
	NewName     string `json:"newName"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
}

// NameConflict records two files that would end up with the same name, or a
// name already taken on disk.
type NameConflict struct {
	Destination string   `json:"destination"`
	Sources     []string `json:"sources"`
	Reason      string   `json:"reason"`
}

// RenameResult is the preview, or the record of what was renamed.
//
// Renames carries every old and new name, whether or not the operation was
// applied. That is the rollback record: run with --output json and keep the
// file, and undoing the batch is a matter of reading it back.
type RenameResult struct {
	Root      string    `json:"root"`
	Applied   bool      `json:"applied"`
	Renames   []Rename  `json:"renames"`
	Planned   int       `json:"planned"`
	Renamed   int       `json:"renamed"`
	Unchanged int       `json:"unchanged"`
	Failed    int       `json:"failed"`
	Problems  []Problem `json:"problems"`
}

// RenameFiles applies a batch rename.
//
// The whole plan is computed and checked for conflicts before anything moves.
// A conflict anywhere aborts the entire operation with nothing changed: a
// half-renamed directory where the failure happened in the middle is far worse
// than a refusal that names the problem.
func RenameFiles(ctx context.Context, mover Mover, request RenameRequest) (RenameResult, error) {
	if request.Lowercase && request.Uppercase {
		return RenameResult{}, errors.New(errors.CodeInvalidInput,
			"--lowercase and --uppercase cannot both be used")
	}
	if !hasTransform(request) {
		return RenameResult{}, errors.New(errors.CodeInvalidInput,
			"no rename rule was given").
			WithHint("pass at least one of --prefix, --suffix, --replace, " +
				"--sequence, --lowercase, or --uppercase")
	}

	walk, err := prepare(mover, request.Selection)
	if err != nil {
		return RenameResult{}, err
	}
	if err := guard(mover, walk.root, request.Force); err != nil {
		return RenameResult{}, err
	}

	files, err := walk.collect(ctx, mover, func(file Info) bool {
		return matchesGlob(file.Name, request.Match)
	})
	if err != nil {
		return RenameResult{}, err
	}

	// Names are assigned in path order so that a sequence number is stable
	// between a preview and the run that applies it.
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	renames := planRenames(request, files)
	if err := checkConflicts(mover, renames); err != nil {
		return RenameResult{}, err
	}

	result := RenameResult{
		Root:     walk.root,
		Applied:  request.Apply,
		Renames:  renames,
		Problems: walk.problems,
	}

	if request.Apply {
		applyRenames(ctx, mover, result.Renames)
	}

	for _, rename := range result.Renames {
		switch rename.Status {
		case RenamePlanned:
			result.Planned++
		case RenameDone:
			result.Renamed++
		case RenameUnchanged:
			result.Unchanged++
		case RenameFailed:
			result.Failed++
		}
	}
	if result.Problems == nil {
		result.Problems = []Problem{}
	}

	return result, nil
}

func hasTransform(request RenameRequest) bool {
	return request.Prefix != "" ||
		request.Suffix != "" ||
		len(request.Replace) > 0 ||
		request.Sequence.Enabled ||
		request.Lowercase ||
		request.Uppercase
}

func planRenames(request RenameRequest, files []Info) []Rename {
	renames := make([]Rename, 0, len(files))
	number := request.Sequence.Start

	for _, file := range files {
		extension := filepath.Ext(file.Name)
		stem := strings.TrimSuffix(file.Name, extension)

		newStem := transform(request, stem, number)
		if request.Sequence.Enabled {
			number++
		}

		newName := newStem + extension
		rename := Rename{
			Source:  file.Path,
			OldName: file.Name,
			NewName: newName,
			Status:  RenamePlanned,
		}

		if newName == file.Name {
			rename.Destination = file.Path
			rename.Status = RenameUnchanged
			rename.Reason = "the name is already what was asked for"
		} else {
			rename.Destination = filepath.Join(filepath.Dir(file.Path), newName)
		}

		renames = append(renames, rename)
	}

	return renames
}

// transform applies every rule in a fixed order.
func transform(request RenameRequest, stem string, number int) string {
	for _, replacement := range request.Replace {
		if replacement.From != "" {
			stem = strings.ReplaceAll(stem, replacement.From, replacement.To)
		}
	}

	switch {
	case request.Lowercase:
		stem = strings.ToLower(stem)
	case request.Uppercase:
		stem = strings.ToUpper(stem)
	}

	if request.Sequence.Enabled {
		formatted := fmt.Sprintf("%0*d", request.Sequence.Padding, number)
		if request.Sequence.Position == SequenceBefore {
			stem = formatted + request.Sequence.Separator + stem
		} else {
			stem += request.Sequence.Separator + formatted
		}
	}

	return request.Prefix + stem + request.Suffix
}

// checkConflicts refuses the whole batch if any two files would collide, or if
// a destination is already taken by a file that is not being renamed away.
func checkConflicts(mover Mover, renames []Rename) error {
	// sources holds every path taking part, so a file renamed out of the way
	// does not count as an obstacle to the file taking its name.
	sources := make(map[string]bool, len(renames))
	for _, rename := range renames {
		if rename.Status == RenamePlanned {
			sources[pathIdentity(rename.Source)] = true
		}
	}

	planned := make(map[string][]string)
	var conflicts []NameConflict

	for _, rename := range renames {
		if rename.Status != RenamePlanned {
			continue
		}
		key := pathIdentity(rename.Destination)
		planned[key] = append(planned[key], rename.Source)

		if sources[key] {
			continue
		}
		exists, err := mover.Exists(rename.Destination)
		if err != nil {
			return err
		}
		if exists {
			conflicts = append(conflicts, NameConflict{
				Destination: rename.Destination,
				Sources:     []string{rename.Source},
				Reason:      "a file with that name already exists",
			})
		}
	}

	for key, owners := range planned {
		if len(owners) > 1 {
			sort.Strings(owners)
			conflicts = append(conflicts, NameConflict{
				Destination: key,
				Sources:     owners,
				Reason:      "several files would end up with the same name",
			})
		}
	}

	if len(conflicts) == 0 {
		return nil
	}

	sort.Slice(conflicts, func(i, j int) bool {
		return conflicts[i].Destination < conflicts[j].Destination
	})

	return errors.New(errors.CodeConflict,
		"%s", describeConflicts(conflicts)).
		WithHint("nothing was renamed; adjust the rules so every name is unique")
}

func describeConflicts(conflicts []NameConflict) string {
	var message strings.Builder
	fmt.Fprintf(&message, "%d naming conflict", len(conflicts))
	if len(conflicts) != 1 {
		message.WriteString("s")
	}

	for _, conflict := range conflicts {
		fmt.Fprintf(&message, "\n  %s: %s", conflict.Destination, conflict.Reason)
		for _, source := range conflict.Sources {
			fmt.Fprintf(&message, "\n    from %s", source)
		}
	}
	return message.String()
}

// applyRenames performs the plan. Like organize, a failure on one file does
// not abort the rest, and cancellation is observed between files.
func applyRenames(ctx context.Context, mover Mover, renames []Rename) {
	for index := range renames {
		rename := &renames[index]
		if rename.Status != RenamePlanned {
			continue
		}
		if err := ctx.Err(); err != nil {
			rename.Status = RenameFailed
			rename.Reason = "cancelled"
			continue
		}

		if err := mover.Move(rename.Source, rename.Destination); err != nil {
			rename.Status = RenameFailed
			rename.Reason = errors.Classify(err).Message
			continue
		}
		rename.Status = RenameDone
	}
}

// matchesGlob reports whether a name is selected. An empty pattern selects
// everything.
func matchesGlob(name, pattern string) bool {
	if strings.TrimSpace(pattern) == "" {
		return true
	}
	matched, err := filepath.Match(pattern, name)
	return err == nil && matched
}
