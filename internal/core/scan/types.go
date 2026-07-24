package scan

import (
	"context"
	"time"

	"github.com/devnest/devnest/internal/classify"
	"github.com/devnest/devnest/internal/platform/fs"
)

// TypesRequest describes one file type breakdown.
type TypesRequest struct {
	Selection
	// Limit caps each listing. Zero means every entry, because "what is in
	// this tree" is a question people ask expecting the whole answer.
	Limit int
	// ByLanguage groups by detected language instead of by extension.
	ByLanguage bool
}

// TypesResult is the breakdown.
type TypesResult struct {
	Root  string `json:"root"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
	// Subject says what the entries are grouped by, so a reader of the JSON
	// does not have to infer it from the flag that produced it.
	Subject string  `json:"subject"`
	Entries []Count `json:"entries"`
	// Unrecognised counts files whose language could not be identified. It
	// is reported rather than hidden, because a large number here means the
	// language table is missing something this project uses.
	Unrecognised int       `json:"unrecognised,omitempty"`
	Problems     []Problem `json:"problems"`
	DurationMs   int64     `json:"durationMs"`
}

// Types reports what kinds of file a tree holds.
//
// By extension, which is what "what is this project written in" usually means,
// or by language with --by-language, which folds .js, .mjs, and .jsx into one
// row and is the more honest answer.
func Types(ctx context.Context, inspector Inspector, request TypesRequest) (TypesResult, error) {
	started := time.Now()

	walk, err := prepare(ctx, inspector, request.Selection)
	if err != nil {
		return TypesResult{}, err
	}

	counts := newTally()
	result := TypesResult{Root: walk.root, Subject: "extension"}
	if request.ByLanguage {
		result.Subject = "language"
	}

	err = inspector.Walk(ctx, walk.options, func(entry fs.Entry) error {
		result.Files++
		result.Bytes += entry.Bytes

		if !request.ByLanguage {
			counts.add(extensionOf(entry.Name), entry.Bytes)
			return nil
		}

		language, known := classify.LanguageOf(entry.Name)
		if !known {
			result.Unrecognised++
			return nil
		}
		counts.add(language.Name, entry.Bytes)
		return nil
	})
	if err != nil {
		return TypesResult{}, err
	}

	result.Entries = counts.ranked(request.Limit, result.Files, result.Bytes)
	result.Problems = walk.collected()
	result.DurationMs = time.Since(started).Milliseconds()
	return result, nil
}
