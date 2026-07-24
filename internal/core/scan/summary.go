package scan

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/classify"
	"github.com/devnest/devnest/internal/platform/fs"
)

// SummaryRequest describes one project summary.
type SummaryRequest struct {
	Selection
	// Top caps each ranked listing. Zero means the default.
	Top int
}

// Count is one label and how much of the tree it accounts for.
type Count struct {
	Name    string  `json:"name"`
	Files   int     `json:"files"`
	Bytes   int64   `json:"bytes"`
	Percent float64 `json:"percent"`
}

// SummaryResult is the shape of a project.
type SummaryResult struct {
	Root        string `json:"root"`
	Files       int    `json:"files"`
	Directories int    `json:"directories"`
	Bytes       int64  `json:"bytes"`
	// Depth is how many levels the deepest file sits below the root.
	Depth int `json:"depth"`

	// Categories covers every file, so the numbers add up to the total.
	Categories []Count `json:"categories"`
	// Languages and Extensions are ranked and capped, so they do not.
	Languages  []Count `json:"languages"`
	Extensions []Count `json:"extensions"`

	// Authored is the part of the tree somebody wrote: source, tests, docs,
	// and configuration, with vendored and generated files left out. It is
	// the number that answers "how big is this project really".
	Authored      int   `json:"authoredFiles"`
	AuthoredBytes int64 `json:"authoredBytes"`

	Ignored    bool      `json:"ignoreRulesApplied"`
	Problems   []Problem `json:"problems"`
	DurationMs int64     `json:"durationMs"`
}

// defaultTop is how many entries a ranked listing reports when the caller does
// not say.
const defaultTop = 10

// Summarize reports what a directory tree is made of.
//
// One walk, and the counters are maps keyed by category, language, and
// extension, so memory tracks the variety of the tree rather than its size. A
// repository with a million files in forty languages costs forty counters.
func Summarize(ctx context.Context, inspector Inspector, request SummaryRequest) (SummaryResult, error) {
	started := time.Now()

	walk, err := prepare(ctx, inspector, request.Selection)
	if err != nil {
		return SummaryResult{}, err
	}

	top := request.Top
	if top < 1 {
		top = defaultTop
	}

	categories := newTally()
	languages := newTally()
	extensions := newTally()
	result := SummaryResult{Root: walk.root, Ignored: walk.ignore != nil}

	options := walk.options
	options.IncludeDirs = true

	err = inspector.Walk(ctx, options, func(entry fs.Entry) error {
		if entry.IsDir {
			result.Directories++
			return nil
		}

		relative := walk.relative(entry.Path)
		result.Files++
		result.Bytes += entry.Bytes
		if depth := strings.Count(relative, "/") + 1; depth > result.Depth {
			result.Depth = depth
		}

		category := classify.Of(relative)
		categories.add(string(category), entry.Bytes)
		extensions.add(extensionOf(entry.Name), entry.Bytes)

		if language, known := classify.LanguageOf(entry.Name); known {
			languages.add(language.Name, entry.Bytes)
		}
		if isAuthored(category) {
			result.Authored++
			result.AuthoredBytes += entry.Bytes
		}
		return nil
	})
	if err != nil {
		return SummaryResult{}, err
	}

	result.Categories = categories.inOrder(categoryNames(), result.Files, result.Bytes)
	result.Languages = languages.ranked(top, result.Files, result.Bytes)
	result.Extensions = extensions.ranked(top, result.Files, result.Bytes)
	result.Problems = walk.collected()
	result.DurationMs = time.Since(started).Milliseconds()
	return result, nil
}

// isAuthored reports whether a category counts as work somebody did on this
// project, as opposed to code copied in or produced by a build.
func isAuthored(category classify.Category) bool {
	switch category {
	case classify.CategorySource, classify.CategoryTest,
		classify.CategoryDocs, classify.CategoryConfig:
		return true
	default:
		return false
	}
}

func categoryNames() []string {
	categories := classify.Categories()
	names := make([]string, 0, len(categories))
	for _, category := range categories {
		names = append(names, string(category))
	}
	return names
}

// extensionOf returns the extension a file is counted under, lower-cased. A
// file without one is counted as "(none)" rather than dropped: a tree full of
// extensionless files is worth seeing.
func extensionOf(name string) string {
	extension := strings.ToLower(filepath.Ext(name))
	if extension == "" {
		return "(none)"
	}
	return extension
}

// tally counts files and bytes against a label.
type tally struct {
	files map[string]int
	bytes map[string]int64
}

func newTally() *tally {
	return &tally{files: make(map[string]int), bytes: make(map[string]int64)}
}

func (t *tally) add(name string, size int64) {
	t.files[name]++
	t.bytes[name] += size
}

// ranked returns the busiest labels, most files first.
//
// Ties break on the name, so two runs over an unchanged tree produce identical
// output. A report nobody can diff is a report nobody trusts.
func (t *tally) ranked(limit, totalFiles int, totalBytes int64) []Count {
	counts := make([]Count, 0, len(t.files))
	for name, files := range t.files {
		counts = append(counts, Count{
			Name:    name,
			Files:   files,
			Bytes:   t.bytes[name],
			Percent: share(files, totalFiles),
		})
	}

	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Files != counts[j].Files {
			return counts[i].Files > counts[j].Files
		}
		if counts[i].Bytes != counts[j].Bytes {
			return counts[i].Bytes > counts[j].Bytes
		}
		return counts[i].Name < counts[j].Name
	})

	if limit > 0 && len(counts) > limit {
		counts = counts[:limit]
	}
	return counts
}

// inOrder returns every named label in the order given, including the ones
// with nothing in them.
//
// Categories want this: a summary that silently omits "generated" leaves the
// reader unable to tell "none" from "not measured".
func (t *tally) inOrder(names []string, totalFiles int, totalBytes int64) []Count {
	counts := make([]Count, 0, len(names))
	for _, name := range names {
		counts = append(counts, Count{
			Name:    name,
			Files:   t.files[name],
			Bytes:   t.bytes[name],
			Percent: share(t.files[name], totalFiles),
		})
	}
	return counts
}

// share is a percentage rounded to one decimal place, so that output stays
// byte-identical between runs.
func share(value, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(int64(float64(value)*1000/float64(total)+0.5)) / 10
}
