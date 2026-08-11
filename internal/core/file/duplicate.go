package file

import (
	"context"
	"sort"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// DuplicateRequest describes one duplicate search.
type DuplicateRequest struct {
	Selection
	// Algorithm is the digest used to compare content.
	Algorithm fs.Algorithm
	// MinBytes ignores files smaller than this. Empty files are all
	// identical to each other, which is true and useless.
	MinBytes int64
	// OnProgress is called after each file is hashed, with how many have been
	// hashed and how many size-candidates there are in total, so a caller can
	// show a long search moving. It may be nil and must not block for long.
	OnProgress func(hashed, total int)
}

// DuplicateGroup is one set of files with identical content.
type DuplicateGroup struct {
	Hash       string `json:"hash"`
	Algorithm  string `json:"algorithm"`
	Bytes      int64  `json:"bytes"`
	Original   Info   `json:"original"`
	Duplicates []Info `json:"duplicates"`
	Wasted     int64  `json:"wastedBytes"`
}

// DuplicateResult reports every group found.
type DuplicateResult struct {
	Root         string           `json:"root"`
	Algorithm    string           `json:"algorithm"`
	Groups       []DuplicateGroup `json:"groups"`
	FilesScanned int              `json:"filesScanned"`
	FilesHashed  int              `json:"filesHashed"`
	Duplicates   int              `json:"duplicateFiles"`
	Wasted       int64            `json:"wastedBytes"`
	Problems     []Problem        `json:"problems"`
}

// Duplicates finds files with identical content.
//
// Two passes, because hashing is the expensive part and most files do not need
// it. The first pass groups by size, which is free: two files of different
// sizes cannot have the same content. Only groups holding more than one file
// go on to be hashed. On a typical tree that eliminates the great majority of
// the work before any file is read.
//
// If this ever proves too slow on a large media library (many files sharing a
// size but differing in content) the next step is a partial digest of the
// first block as a second filter before the full read. That is deliberately
// not here yet: it is complexity that needs a measurement to justify it.
//
// Nothing is deleted. The command reports what it found and the user decides.
func Duplicates(ctx context.Context, inspector Inspector, request DuplicateRequest) (DuplicateResult, error) {
	if request.Algorithm == "" {
		request.Algorithm = fs.SHA256
	}
	if request.MinBytes < 1 {
		request.MinBytes = 1
	}

	walk, err := prepare(inspector, request.Selection)
	if err != nil {
		return DuplicateResult{}, err
	}

	files, err := walk.collect(ctx, inspector, func(file Info) bool {
		return file.Bytes >= request.MinBytes
	})
	if err != nil {
		return DuplicateResult{}, err
	}

	result := DuplicateResult{
		Root:         walk.root,
		Algorithm:    string(request.Algorithm),
		Groups:       []DuplicateGroup{},
		FilesScanned: len(files),
		Problems:     walk.problems,
	}

	candidates := groupBySize(files)
	byHash := make(map[string][]Info)
	total := 0
	for _, sized := range candidates {
		total += len(sized)
	}

	for _, sized := range candidates {
		for _, file := range sized {
			if err := ctx.Err(); err != nil {
				return DuplicateResult{}, err
			}

			checksums, err := inspector.Digest(ctx, file.Path, []fs.Algorithm{request.Algorithm})
			if err != nil {
				report := errors.Classify(err)
				if report.Code == errors.CodeCancelled {
					return DuplicateResult{}, err
				}
				result.Problems = append(result.Problems, Problem{
					Path: file.Path, Code: string(report.Code), Message: report.Message,
				})
				continue
			}

			result.FilesHashed++
			byHash[checksums[0].Value] = append(byHash[checksums[0].Value], file)

			if request.OnProgress != nil {
				request.OnProgress(result.FilesHashed, total)
			}
		}
	}

	result.Groups = buildGroups(byHash, string(request.Algorithm))
	for _, group := range result.Groups {
		result.Duplicates += len(group.Duplicates)
		result.Wasted += group.Wasted
	}
	if result.Problems == nil {
		result.Problems = []Problem{}
	}

	return result, nil
}

// groupBySize returns only the size groups that could possibly hold
// duplicates. Everything with a unique size is discarded without being read.
func groupBySize(files []Info) [][]Info {
	bySize := make(map[int64][]Info)
	for _, file := range files {
		bySize[file.Bytes] = append(bySize[file.Bytes], file)
	}

	sizes := make([]int64, 0, len(bySize))
	for size, group := range bySize {
		if len(group) > 1 {
			sizes = append(sizes, size)
		}
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i] > sizes[j] })

	candidates := make([][]Info, 0, len(sizes))
	for _, size := range sizes {
		candidates = append(candidates, bySize[size])
	}
	return candidates
}

// buildGroups turns the hash index into results, choosing the oldest file in
// each group as the original.
func buildGroups(byHash map[string][]Info, algorithm string) []DuplicateGroup {
	groups := make([]DuplicateGroup, 0)

	for hash, files := range byHash {
		if len(files) < 2 {
			continue
		}

		// Oldest first, then by path. The oldest copy is the one most likely
		// to be the original, and the path tie-break keeps the choice stable
		// across runs.
		sort.Slice(files, func(i, j int) bool {
			if !files[i].ModifiedAt.Equal(files[j].ModifiedAt) {
				return files[i].ModifiedAt.Before(files[j].ModifiedAt)
			}
			return files[i].Path < files[j].Path
		})

		group := DuplicateGroup{
			Hash:       hash,
			Algorithm:  algorithm,
			Bytes:      files[0].Bytes,
			Original:   files[0],
			Duplicates: files[1:],
			Wasted:     files[0].Bytes * int64(len(files)-1),
		}
		groups = append(groups, group)
	}

	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Wasted != groups[j].Wasted {
			return groups[i].Wasted > groups[j].Wasted
		}
		return groups[i].Original.Path < groups[j].Original.Path
	})
	return groups
}
