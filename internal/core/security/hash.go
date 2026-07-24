package security

import (
	"context"
	"strings"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// Input sources.
const (
	SourceText = "text"
	SourceFile = "file"
)

// HashRequest describes one hashing operation.
//
// Exactly one of Text and Path is used. Which one is a decision the caller has
// already made; this module does not guess whether a string is a filename,
// because guessing wrong means either hashing the name of a file or trying to
// open a sentence.
type HashRequest struct {
	Text       string
	Path       string
	Source     string
	Algorithms []fs.Algorithm
}

// HashResult is the digest and where it came from.
type HashResult struct {
	Source    string        `json:"source"`
	Path      string        `json:"path,omitempty"`
	Bytes     int64         `json:"bytes"`
	Checksums []fs.Checksum `json:"checksums"`
}

// Hash computes digests over text or a file.
//
// Both paths go through the same streaming implementation in the platform
// layer, so a file and a string of the same content produce the same digest by
// construction rather than by coincidence. Memory use does not depend on file
// size.
func Hash(ctx context.Context, hasher Hasher, request HashRequest) (HashResult, error) {
	algorithms := request.Algorithms
	if len(algorithms) == 0 {
		algorithms = []fs.Algorithm{fs.SHA256}
	}

	switch request.Source {
	case SourceFile:
		return hashFile(ctx, hasher, request.Path, algorithms)
	case SourceText:
		return hashText(ctx, hasher, request.Text, algorithms)
	default:
		return HashResult{}, errors.New(errors.CodeInvalidInput,
			"no input was given").
			WithHint("pass text to hash, or --file to hash a file")
	}
}

func hashText(
	ctx context.Context,
	hasher Hasher,
	text string,
	algorithms []fs.Algorithm,
) (HashResult, error) {
	// An empty string has a well-defined digest and hashing it is a legitimate
	// thing to want, so this is not rejected.
	checksums, err := hasher.DigestReader(ctx, strings.NewReader(text), algorithms)
	if err != nil {
		return HashResult{}, err
	}

	return HashResult{
		Source:    SourceText,
		Bytes:     int64(len(text)),
		Checksums: checksums,
	}, nil
}

func hashFile(
	ctx context.Context,
	hasher Hasher,
	path string,
	algorithms []fs.Algorithm,
) (HashResult, error) {
	if strings.TrimSpace(path) == "" {
		return HashResult{}, errors.New(errors.CodeInvalidInput, "no file was given")
	}

	resolved, err := hasher.Resolve(path)
	if err != nil {
		return HashResult{}, err
	}

	entry, err := hasher.Stat(resolved)
	if err != nil {
		return HashResult{}, err
	}
	if entry.IsDir {
		return HashResult{}, errors.New(errors.CodeInvalidInput,
			"%s is a directory", resolved).
			WithHint("pass a file; a directory digest has to fold in names as well as content")
	}

	checksums, err := hasher.Digest(ctx, resolved, algorithms)
	if err != nil {
		return HashResult{}, err
	}

	return HashResult{
		Source:    SourceFile,
		Path:      resolved,
		Bytes:     entry.Bytes,
		Checksums: checksums,
	}, nil
}
