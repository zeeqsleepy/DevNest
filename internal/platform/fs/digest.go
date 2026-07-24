package fs

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"io"
	"os"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// Algorithm names a supported digest. Adding one is a constant, a case in
// newHash, and a line in Algorithms.
type Algorithm string

const (
	MD5    Algorithm = "md5"
	SHA256 Algorithm = "sha256"
	SHA512 Algorithm = "sha512"
)

// Algorithms lists every supported digest, in the order help text shows them.
func Algorithms() []Algorithm {
	return []Algorithm{SHA256, SHA512, MD5}
}

// ParseAlgorithm resolves a name given on the command line.
func ParseAlgorithm(name string) (Algorithm, error) {
	candidate := Algorithm(strings.ToLower(strings.TrimSpace(name)))
	for _, supported := range Algorithms() {
		if candidate == supported {
			return supported, nil
		}
	}

	names := make([]string, 0, len(Algorithms()))
	for _, supported := range Algorithms() {
		names = append(names, string(supported))
	}
	return "", errors.New(errors.CodeInvalidInput, "unknown algorithm %q", name).
		WithHint("expected one of: %s", strings.Join(names, ", "))
}

// Checksum is one digest of one file.
type Checksum struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

// digestBuffer is the read size used while hashing. Large enough that syscall
// overhead disappears, small enough that hashing a 10 GB file costs the same
// memory as hashing a small one.
const digestBuffer = 256 * 1024

// Digest computes every requested digest in a single pass over the file.
//
// Reading once and writing into several hashes matters: computing three
// digests separately would read a large file three times, and the read is the
// expensive part.
func (s System) Digest(ctx context.Context, path string, algorithms []Algorithm) ([]Checksum, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, wrapPath("read", path, err)
	}
	defer func() {
		// The file is open for reading only, so a failed close cannot lose data.
		_ = file.Close()
	}()

	return s.digest(ctx, file, path, algorithms)
}

// DigestReader computes digests over any stream.
//
// This exists so that hashing a string and hashing a file are the same code.
// The security module hashes text, the file module hashes files, and a second
// implementation of "which algorithm is this and how do I feed it" is exactly
// the kind of duplication that lets two commands disagree about what SHA-256
// means.
func (s System) DigestReader(ctx context.Context, reader io.Reader, algorithms []Algorithm) ([]Checksum, error) {
	return s.digest(ctx, reader, "input", algorithms)
}

func (s System) digest(
	ctx context.Context,
	reader io.Reader,
	subject string,
	algorithms []Algorithm,
) ([]Checksum, error) {
	if len(algorithms) == 0 {
		return nil, errors.New(errors.CodeInvalidInput, "no digest algorithm was requested")
	}

	hashes := make([]hash.Hash, 0, len(algorithms))
	writers := make([]io.Writer, 0, len(algorithms))
	for _, algorithm := range algorithms {
		created, err := newHash(algorithm)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, created)
		writers = append(writers, created)
	}

	if err := copyInto(ctx, io.MultiWriter(writers...), reader, subject); err != nil {
		return nil, err
	}

	checksums := make([]Checksum, 0, len(algorithms))
	for index, algorithm := range algorithms {
		checksums = append(checksums, Checksum{
			Algorithm: string(algorithm),
			Value:     hex.EncodeToString(hashes[index].Sum(nil)),
		})
	}
	return checksums, nil
}

// DigestLength reports how many hex characters an algorithm's output has, so a
// checksum can be recognised from its length alone.
func DigestLength(algorithm Algorithm) int {
	switch algorithm {
	case MD5:
		return 32
	case SHA256:
		return 64
	case SHA512:
		return 128
	default:
		return 0
	}
}

// AlgorithmForLength identifies an algorithm from the length of a hex digest.
//
// Every supported algorithm has a different output length, so a user who
// pasted a checksum does not have to know or say which kind it is.
func AlgorithmForLength(length int) (Algorithm, bool) {
	for _, algorithm := range Algorithms() {
		if DigestLength(algorithm) == length {
			return algorithm, true
		}
	}
	return "", false
}

// copyInto streams the file through a fixed buffer, checking for cancellation
// between chunks so a digest of a very large file can be interrupted.
func copyInto(ctx context.Context, destination io.Writer, source io.Reader, path string) error {
	buffer := make([]byte, digestBuffer)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		read, err := source.Read(buffer)
		if read > 0 {
			if _, err := destination.Write(buffer[:read]); err != nil {
				return errors.Wrap(err, errors.CodeIO, "cannot hash %s", path)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return wrapPath("read", path, err)
		}
	}
}

func newHash(algorithm Algorithm) (hash.Hash, error) {
	switch algorithm {
	case MD5:
		return md5.New(), nil
	case SHA256:
		return sha256.New(), nil
	case SHA512:
		return sha512.New(), nil
	default:
		return nil, errors.New(errors.CodeInvalidInput, "unknown algorithm %q", algorithm)
	}
}
