package file

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// HashRequest describes one hashing operation.
type HashRequest struct {
	// Paths are the files to hash.
	Paths []string
	// Algorithms are the digests to compute. Every one is produced in a
	// single pass over each file.
	Algorithms []fs.Algorithm
}

// Digest is one file and its checksums.
type Digest struct {
	Name      string        `json:"name"`
	Path      string        `json:"path"`
	Bytes     int64         `json:"bytes"`
	Checksums []fs.Checksum `json:"checksums"`
}

// HashResult reports the digests of every file requested.
type HashResult struct {
	Files    []Digest  `json:"files"`
	Problems []Problem `json:"problems"`
}

// Hash computes checksums for one or more files.
//
// A directory is rejected rather than walked. Hashing a tree is a different
// operation with a different meaning (the digest of a directory has to fold
// in names as well as contents, or a rename would go unnoticed) and quietly
// doing something else because the path happened to be a directory is not the
// kind of surprise this tool hands out.
//
// A failure on one file is recorded and the rest still run, so hashing a list
// of files does not stop at the first one that has been moved away.
func Hash(ctx context.Context, inspector Inspector, request HashRequest) (HashResult, error) {
	if len(request.Paths) == 0 {
		return HashResult{}, errors.New(errors.CodeInvalidInput, "no file was given").
			WithHint("pass one or more files to hash")
	}
	if len(request.Algorithms) == 0 {
		request.Algorithms = []fs.Algorithm{fs.SHA256}
	}

	result := HashResult{Files: []Digest{}, Problems: []Problem{}}

	for _, path := range request.Paths {
		if err := ctx.Err(); err != nil {
			return HashResult{}, err
		}

		digest, err := hashOne(ctx, inspector, path, request.Algorithms)
		if err != nil {
			report := errors.Classify(err)
			if report.Code == errors.CodeCancelled {
				return HashResult{}, err
			}
			// A single bad path given explicitly is worth failing on; several
			// paths behave like a batch and report per-file problems.
			if len(request.Paths) == 1 {
				return HashResult{}, err
			}
			result.Problems = append(result.Problems, Problem{
				Path: path, Code: string(report.Code), Message: report.Message,
			})
			continue
		}
		result.Files = append(result.Files, digest)
	}

	return result, nil
}

func hashOne(
	ctx context.Context,
	inspector Inspector,
	path string,
	algorithms []fs.Algorithm,
) (Digest, error) {
	if strings.TrimSpace(path) == "" {
		return Digest{}, errors.New(errors.CodeInvalidInput, "an empty path was given")
	}

	resolved, err := inspector.Resolve(path)
	if err != nil {
		return Digest{}, err
	}

	entry, err := inspector.Stat(resolved)
	if err != nil {
		return Digest{}, err
	}
	if entry.IsDir {
		return Digest{}, errors.New(errors.CodeInvalidInput,
			"%s is a directory", resolved).
			WithHint("pass a file, or use \"devnest file duplicate\" to compare a whole tree")
	}

	checksums, err := inspector.Digest(ctx, resolved, algorithms)
	if err != nil {
		return Digest{}, err
	}

	return Digest{
		Name:      filepath.Base(resolved),
		Path:      resolved,
		Bytes:     entry.Bytes,
		Checksums: checksums,
	}, nil
}
