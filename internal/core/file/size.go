package file

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/devnest/devnest/internal/platform/fs"
)

// SizeRequest describes one directory size analysis.
type SizeRequest struct {
	Selection
	// Depth limits how deep the reported directory list goes. One reports
	// only the immediate children of the root, which is what answers "what is
	// taking up the space" most of the time.
	Depth int
	// TopDirectories and TopFiles cap the two listings.
	TopDirectories int
	TopFiles       int
}

// DirectoryUsage is one directory's share of the total.
type DirectoryUsage struct {
	Path     string  `json:"path"`
	Relative string  `json:"relativePath"`
	Bytes    int64   `json:"bytes"`
	Files    int     `json:"files"`
	Percent  float64 `json:"percent"`
}

// SizeResult reports where the space went.
type SizeResult struct {
	Root             string           `json:"root"`
	TotalBytes       int64            `json:"totalBytes"`
	TotalFiles       int              `json:"totalFiles"`
	TotalDirectories int              `json:"totalDirectories"`
	Directories      []DirectoryUsage `json:"directories"`
	LargestFiles     []Info           `json:"largestFiles"`
	Problems         []Problem        `json:"problems"`
}

// Size analyses where the space in a directory tree has gone.
//
// The walk is always recursive, whatever the selection says, because a size
// report that stops at the first level would attribute nothing to the
// directories that actually hold the data. Depth controls how much of the
// result is reported, not how much is measured.
//
// Sizes accumulate as the walk proceeds and only the totals are kept, so
// memory use tracks the number of directories rather than the number of files.
// A tree with a million files in a few hundred directories costs a few hundred
// counters.
func Size(ctx context.Context, inspector Inspector, request SizeRequest) (SizeResult, error) {
	if request.Depth < 1 {
		request.Depth = 1
	}
	if request.TopDirectories < 1 {
		request.TopDirectories = 10
	}
	if request.TopFiles < 1 {
		request.TopFiles = 10
	}

	selection := request.Selection
	selection.Recursive = true
	selection.MaxDepth = 0

	walk, err := prepare(inspector, selection)
	if err != nil {
		return SizeResult{}, err
	}

	totals := make(map[string]*DirectoryUsage)
	largest := newLargestFiles(request.TopFiles)
	result := SizeResult{Root: walk.root}

	err = inspector.Walk(ctx, walk.options, func(entry fs.Entry) error {
		info := walk.describe(entry)

		result.TotalFiles++
		result.TotalBytes += info.Bytes
		largest.offer(info)

		// A file's bytes count towards every directory between it and the
		// root, which is what makes the reported figures nest correctly.
		for _, owner := range ancestors(walk.root, filepath.Dir(info.Path), request.Depth) {
			usage, seen := totals[owner]
			if !seen {
				usage = &DirectoryUsage{Path: owner}
				totals[owner] = usage
			}
			usage.Bytes += info.Bytes
			usage.Files++
		}
		return nil
	})
	if err != nil {
		return SizeResult{}, err
	}

	result.TotalDirectories = len(totals)
	result.Directories = rankDirectories(totals, walk.root, result.TotalBytes, request.TopDirectories)
	result.LargestFiles = largest.sorted()
	result.Problems = walk.problems
	if result.Problems == nil {
		result.Problems = []Problem{}
	}

	return result, nil
}

// ancestors lists the directories, up to the reporting depth, that a file
// belongs to. The root itself is not included; its total is the grand total.
func ancestors(root, directory string, depth int) []string {
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == "." {
		return nil
	}

	parts := splitPath(relative)
	if len(parts) > depth {
		parts = parts[:depth]
	}

	owners := make([]string, 0, len(parts))
	current := root
	for _, part := range parts {
		current = filepath.Join(current, part)
		owners = append(owners, current)
	}
	return owners
}

func splitPath(relative string) []string {
	var parts []string
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return parts
}

func rankDirectories(totals map[string]*DirectoryUsage, root string, grandTotal int64, limit int) []DirectoryUsage {
	usages := make([]DirectoryUsage, 0, len(totals))
	for _, usage := range totals {
		relative, err := filepath.Rel(root, usage.Path)
		if err != nil {
			relative = usage.Path
		}
		usage.Relative = filepath.ToSlash(relative)
		if grandTotal > 0 {
			usage.Percent = round1(float64(usage.Bytes) * 100 / float64(grandTotal))
		}
		usages = append(usages, *usage)
	}

	sort.Slice(usages, func(i, j int) bool {
		if usages[i].Bytes != usages[j].Bytes {
			return usages[i].Bytes > usages[j].Bytes
		}
		return usages[i].Relative < usages[j].Relative
	})

	if len(usages) > limit {
		usages = usages[:limit]
	}
	return usages
}

func round1(value float64) float64 {
	return float64(int64(value*10+0.5)) / 10
}

// largestFiles keeps only the top N files rather than sorting every file at
// the end. On a tree with a million files that is the difference between a few
// kilobytes and a few hundred megabytes.
type largestFiles struct {
	limit int
	files []Info
}

func newLargestFiles(limit int) *largestFiles {
	return &largestFiles{limit: limit, files: make([]Info, 0, limit+1)}
}

func (l *largestFiles) offer(file Info) {
	if len(l.files) == l.limit && file.Bytes <= l.files[len(l.files)-1].Bytes {
		return
	}

	position := sort.Search(len(l.files), func(i int) bool {
		return l.files[i].Bytes < file.Bytes
	})

	l.files = append(l.files, Info{})
	copy(l.files[position+1:], l.files[position:])
	l.files[position] = file

	if len(l.files) > l.limit {
		l.files = l.files[:l.limit]
	}
}

func (l *largestFiles) sorted() []Info {
	files := append([]Info(nil), l.files...)
	sort.Slice(files, func(i, j int) bool {
		if files[i].Bytes != files[j].Bytes {
			return files[i].Bytes > files[j].Bytes
		}
		return files[i].Path < files[j].Path
	})
	if files == nil {
		return []Info{}
	}
	return files
}
