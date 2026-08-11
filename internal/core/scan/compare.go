package scan

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// CompareRequest compares a saved snapshot of a project with the same tree now.
type CompareRequest struct {
	// Selection is the walk both the snapshot and the current scan use, so a
	// snapshot taken one way and compared the same way show the change rather
	// than a difference between two methods of counting.
	Selection Selection
	// Before is the earlier scan, loaded from a file the user saved.
	Before SummaryResult
	// Now locates the tree being compared, overriding Selection.Root when set.
	// Most comparisons run the saved snapshot against the current directory.
	Now string
}

// CountDelta is one label and how it grew between two scans.
type CountDelta struct {
	Name string `json:"name"`
	// FilesBefore and FilesAfter are the two counts, and FilesDelta is the
	// difference, so a reader has the change and the two numbers it came from.
	FilesBefore int   `json:"filesBefore"`
	FilesAfter  int   `json:"filesAfter"`
	FilesDelta  int   `json:"filesDelta"`
	BytesBefore int64 `json:"bytesBefore"`
	BytesAfter  int64 `json:"bytesAfter"`
	BytesDelta  int64 `json:"bytesDelta"`
}

// CompareResult is the growth between two scans.
//
// Delta is always after minus before, so a growing project is a positive
// number and a shrinking one is negative.
type CompareResult struct {
	Root string `json:"root"`

	FilesBefore     int `json:"filesBefore"`
	FilesAfter      int `json:"filesAfter"`
	FilesDelta      int `json:"filesDelta"`
	DirectoriesBefore int `json:"directoriesBefore"`
	DirectoriesAfter  int `json:"directoriesAfter"`
	DirectoriesDelta  int `json:"directoriesDelta"`
	BytesBefore     int64 `json:"bytesBefore"`
	BytesAfter      int64 `json:"bytesAfter"`
	BytesDelta      int64 `json:"bytesDelta"`

	// Authored is the part of the tree somebody wrote, and how that grew.
	AuthoredBefore      int   `json:"authoredFilesBefore"`
	AuthoredAfter       int   `json:"authoredFilesAfter"`
	AuthoredDelta       int   `json:"authoredFilesDelta"`
	AuthoredBytesBefore int64 `json:"authoredBytesBefore"`
	AuthoredBytesAfter  int64 `json:"authoredBytesAfter"`
	AuthoredBytesDelta  int64 `json:"authoredBytesDelta"`

	// Categories is every category in the canonical order, with deltas, so a
	// reader can tell "none" from "not measured". Languages is restricted to
	// the languages that changed, because a list of everything would be long
	// and most of it would be zeroes.
	Categories []CountDelta `json:"categories"`
	Languages  []CountDelta `json:"languages"`

	Problems   []Problem `json:"problems"`
	DurationMs int64     `json:"durationMs"`
}

// Load reads a saved scan out of a JSON file.
//
// A scan saved by --export or --output json is wrapped in the result envelope;
// a bare SummaryResult is accepted too. The envelope is the form this module's
// own commands write, so that is what is tried first and expected.
func Load(data []byte) (SummaryResult, error) {
	var envelope struct {
		Data SummaryResult `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Data.Root != "" {
		return envelope.Data, nil
	}

	var bare SummaryResult
	if err := json.Unmarshal(data, &bare); err != nil {
		return SummaryResult{}, errors.New(errors.CodeParse,
			"the snapshot is not a saved scan: %v", err)
	}
	if bare.Files == 0 && bare.Bytes == 0 && len(bare.Categories) == 0 {
		return SummaryResult{}, errors.New(errors.CodeParse,
			"the snapshot holds no scan data").
			WithHint("save one with \"devnest scan --output json > snapshot.json\" " +
				"or \"devnest scan --export snapshot.json\"")
	}
	return bare, nil
}

// Compare reports how a project grew between an earlier scan and now.
//
// The "before" comes from a file the user saved (with --export or --output
// json); the "after" is a fresh scan with the same settings. The comparison is
// of aggregation, not of individual files: reports how many more files, how
// much larger, and which categories grew, not a list of renamed paths.
func Compare(ctx context.Context, inspector Inspector, request CompareRequest) (CompareResult, error) {
	started := time.Now()

	selection := request.Selection
	if request.Now != "" {
		selection.Root = request.Now
	}

	current, err := Summarize(ctx, inspector, SummaryRequest{Selection: selection})
	if err != nil {
		return CompareResult{}, err
	}

	before := request.Before
	after := current

	return CompareResult{
		Root: current.Root,

		FilesBefore:     before.Files,
		FilesAfter:      after.Files,
		FilesDelta:      after.Files - before.Files,
		DirectoriesBefore: before.Directories,
		DirectoriesAfter:  after.Directories,
		DirectoriesDelta:  after.Directories - before.Directories,
		BytesBefore:     before.Bytes,
		BytesAfter:      after.Bytes,
		BytesDelta:      after.Bytes - before.Bytes,

		AuthoredBefore:      before.Authored,
		AuthoredAfter:       after.Authored,
		AuthoredDelta:       after.Authored - before.Authored,
		AuthoredBytesBefore: before.AuthoredBytes,
		AuthoredBytesAfter:  after.AuthoredBytes,
		AuthoredBytesDelta:  after.AuthoredBytes - before.AuthoredBytes,

		Categories: compareCounts(before.Categories, after.Categories, true),
		Languages:  compareCounts(before.Languages, after.Languages, false),
		Problems:   current.Problems,

		DurationMs: time.Since(started).Milliseconds(),
	}, nil
}

// compareCounts builds the deltas between two sets of counts.
//
// When includeAll is true every key in the "after" list is reported, in the
// canonical order, whether or not it changed, plus any key that was in
// "before" but has since disappeared. When false only the keys that differ (or
// are in one list but not the other) are kept, and they are sorted by name so
// two runs are comparable.
func compareCounts(before, after []Count, includeAll bool) []CountDelta {
	byName := make(map[string]CountDelta, len(after)+len(before))

	seed := func(counts []Count) {
		for _, count := range counts {
			delta := byName[count.Name]
			delta.Name = count.Name
			delta.FilesBefore = count.Files
			delta.BytesBefore = count.Bytes
			byName[count.Name] = delta
		}
	}
	seed(before)
	for _, count := range after {
		delta := byName[count.Name]
		delta.Name = count.Name
		delta.FilesAfter = count.Files
		delta.BytesAfter = count.Bytes
		byName[count.Name] = delta
	}

	deltas := make([]CountDelta, 0, len(byName))
	for _, delta := range byName {
		// The delta is computed here, not in the loop over "after", so a label
		// that was in "before" but is gone now still reports its disappearance
		// rather than being taken as zero change.
		delta.FilesDelta = delta.FilesAfter - delta.FilesBefore
		delta.BytesDelta = delta.BytesAfter - delta.BytesBefore
		if includeAll || delta.FilesDelta != 0 || delta.BytesDelta != 0 {
			deltas = append(deltas, delta)
		}
	}

	if includeAll {
		// The canonical order belongs to the after scan. A category present in
		// "before" but absent now is reported after everything current, so the
		// reader sees it shrank to nothing rather than that it is unmentioned.
		afterIndex := make(map[string]int, len(after))
		for index, count := range after {
			afterIndex[count.Name] = index
		}
		sort.SliceStable(deltas, func(i, j int) bool {
			left, leftSeen := afterIndex[deltas[i].Name]
			right, rightSeen := afterIndex[deltas[j].Name]
			if leftSeen && rightSeen {
				return left < right
			}
			return leftSeen && !rightSeen
		})
	} else {
		sort.Slice(deltas, func(i, j int) bool { return deltas[i].Name < deltas[j].Name })
	}

	return deltas
}
