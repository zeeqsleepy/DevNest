package file

import (
	"context"
	"sort"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// Sort orders for a filter result.
const (
	SortByPath     = "path"
	SortBySize     = "size"
	SortByModified = "modified"
	SortByName     = "name"
)

// FilterRequest describes one search.
type FilterRequest struct {
	Selection
	// Extensions selects by extension, normalised with a leading dot.
	Extensions []string
	// Category selects a whole group, such as Images or Code.
	Category string
	// Match is a glob applied to base names.
	Match string
	// MinBytes and MaxBytes bound the file size. Zero means no bound.
	MinBytes int64
	MaxBytes int64
	// SortBy orders the result.
	SortBy string
	// Limit truncates the list. Zero means everything.
	Limit int
}

// FilterResult lists what was found.
type FilterResult struct {
	Root       string    `json:"root"`
	Files      []Info    `json:"files"`
	Matched    int       `json:"matched"`
	Scanned    int       `json:"scanned"`
	TotalBytes int64     `json:"totalBytes"`
	Truncated  bool      `json:"truncated"`
	Problems   []Problem `json:"problems"`
}

// ParseSort resolves the --sort flag.
func ParseSort(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", SortByPath:
		return SortByPath, nil
	case SortBySize:
		return SortBySize, nil
	case SortByModified:
		return SortByModified, nil
	case SortByName:
		return SortByName, nil
	}
	return "", errors.New(errors.CodeInvalidInput, "unknown sort order %q", name).
		WithHint("expected one of: path, name, size, modified")
}

// Filter searches a tree for files matching an extension, a category, a name
// pattern, a size range, or any combination of them.
//
// Every condition given must hold. Giving none lists everything, which is a
// reasonable way to see what is in a directory.
func Filter(ctx context.Context, inspector Inspector, request FilterRequest) (FilterResult, error) {
	if request.SortBy == "" {
		request.SortBy = SortByPath
	}
	if request.Category != "" {
		matched, known := MatchCategory(request.Category)
		if !known {
			return FilterResult{}, errors.New(errors.CodeInvalidInput,
				"unknown category %q", request.Category).
				WithHint("expected one of: %s", strings.Join(CategoryNames(), ", "))
		}
		request.Category = matched
	}
	if request.MaxBytes > 0 && request.MinBytes > request.MaxBytes {
		return FilterResult{}, errors.New(errors.CodeInvalidInput,
			"--min-size is larger than --max-size")
	}

	wanted := make(map[string]bool, len(request.Extensions))
	for _, extension := range request.Extensions {
		if wantedExtension := NormalizeExtensionArgument(extension); wantedExtension != "" {
			wanted[wantedExtension] = true
		}
	}

	walk, err := prepare(inspector, request.Selection)
	if err != nil {
		return FilterResult{}, err
	}

	scanned := 0
	files, err := walk.collect(ctx, inspector, func(file Info) bool {
		scanned++
		return keeps(request, wanted, file)
	})
	if err != nil {
		return FilterResult{}, err
	}

	sortFiles(files, request.SortBy)

	result := FilterResult{
		Root:     walk.root,
		Files:    files,
		Matched:  len(files),
		Scanned:  scanned,
		Problems: walk.problems,
	}
	for _, file := range files {
		result.TotalBytes += file.Bytes
	}

	if request.Limit > 0 && len(result.Files) > request.Limit {
		result.Files = result.Files[:request.Limit]
		result.Truncated = true
	}
	if result.Files == nil {
		result.Files = []Info{}
	}
	if result.Problems == nil {
		result.Problems = []Problem{}
	}

	return result, nil
}

func keeps(request FilterRequest, wanted map[string]bool, file Info) bool {
	if len(wanted) > 0 && !wanted[file.Extension] {
		return false
	}
	if request.Category != "" && file.Category != request.Category {
		return false
	}
	if !matchesGlob(file.Name, request.Match) {
		return false
	}
	if request.MinBytes > 0 && file.Bytes < request.MinBytes {
		return false
	}
	if request.MaxBytes > 0 && file.Bytes > request.MaxBytes {
		return false
	}
	return true
}

// sortFiles orders the result. Every comparison falls back to the path so that
// two runs over an unchanged tree produce identical output.
func sortFiles(files []Info, order string) {
	switch order {
	case SortBySize:
		sort.Slice(files, func(i, j int) bool {
			if files[i].Bytes != files[j].Bytes {
				return files[i].Bytes > files[j].Bytes
			}
			return files[i].Path < files[j].Path
		})
	case SortByModified:
		sort.Slice(files, func(i, j int) bool {
			if !files[i].ModifiedAt.Equal(files[j].ModifiedAt) {
				return files[i].ModifiedAt.After(files[j].ModifiedAt)
			}
			return files[i].Path < files[j].Path
		})
	case SortByName:
		sort.Slice(files, func(i, j int) bool {
			left, right := strings.ToLower(files[i].Name), strings.ToLower(files[j].Name)
			if left != right {
				return left < right
			}
			return files[i].Path < files[j].Path
		})
	default:
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	}
}
