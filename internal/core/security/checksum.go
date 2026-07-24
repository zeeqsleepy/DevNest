package security

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"strings"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// ChecksumRequest describes one integrity check.
type ChecksumRequest struct {
	Path     string
	Expected string
	// Algorithm may be left empty, in which case it is worked out from the
	// length of the expected digest.
	Algorithm fs.Algorithm
}

// ChecksumResult reports what was expected, what was found, and whether they
// agree.
//
// Both digests are reported even on a match. A verification tool that prints
// only "ok" gives the user nothing to check when they suspect the tool itself.
type ChecksumResult struct {
	Path      string `json:"path"`
	Algorithm string `json:"algorithm"`
	Expected  string `json:"expected"`
	Actual    string `json:"actual"`
	Match     bool   `json:"match"`
	Bytes     int64  `json:"bytes"`
}

// VerifyChecksum checks a file against an expected digest.
//
// A mismatch is a result, not an error: finding out that a download does not
// match its published checksum is the entire purpose of the command, and it
// succeeded in telling you. The caller turns that into a non-zero exit code.
//
// Only a failure to perform the check (a missing file, an unreadable one, a
// digest that is not a digest) comes back as an error.
func VerifyChecksum(ctx context.Context, hasher Hasher, request ChecksumRequest) (ChecksumResult, error) {
	expected, algorithm, err := parseExpected(request.Expected, request.Algorithm)
	if err != nil {
		return ChecksumResult{}, err
	}

	if strings.TrimSpace(request.Path) == "" {
		return ChecksumResult{}, errors.New(errors.CodeInvalidInput, "no file was given").
			WithHint("pass the file to verify and the digest to check it against")
	}

	resolved, err := hasher.Resolve(request.Path)
	if err != nil {
		return ChecksumResult{}, err
	}

	entry, err := hasher.Stat(resolved)
	if err != nil {
		return ChecksumResult{}, err
	}
	if entry.IsDir {
		return ChecksumResult{}, errors.New(errors.CodeInvalidInput,
			"%s is a directory", resolved).
			WithHint("pass the file whose checksum you want to verify")
	}

	checksums, err := hasher.Digest(ctx, resolved, []fs.Algorithm{algorithm})
	if err != nil {
		return ChecksumResult{}, err
	}

	actual := checksums[0].Value

	// Constant-time comparison. The threat it defends against does not really
	// apply to a local file check, but the cost is nothing and getting into
	// the habit of comparing digests this way is worth more than the
	// microseconds.
	match := subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1

	return ChecksumResult{
		Path:      resolved,
		Algorithm: string(algorithm),
		Expected:  expected,
		Actual:    actual,
		Match:     match,
		Bytes:     entry.Bytes,
	}, nil
}

// parseExpected validates the digest and works out which algorithm produced
// it.
//
// Every supported algorithm has a distinct output length, so a user who pasted
// a checksum from a release page does not have to know or say which kind it
// is. When they do say, and it disagrees with the length, that is a mistake
// worth reporting rather than quietly overriding.
func parseExpected(expected string, algorithm fs.Algorithm) (string, fs.Algorithm, error) {
	normalised := strings.ToLower(strings.TrimSpace(expected))
	if normalised == "" {
		return "", "", errors.New(errors.CodeInvalidInput, "no expected digest was given").
			WithHint("pass the digest published alongside the file")
	}

	// Checksum files often carry a leading marker for binary or text mode.
	normalised = strings.TrimPrefix(normalised, "*")

	if _, err := hex.DecodeString(normalised); err != nil {
		return "", "", errors.New(errors.CodeInvalidInput,
			"%q is not a hexadecimal digest", expected).
			WithHint("a digest is hex characters only, with no spaces or prefix")
	}

	detected, known := fs.AlgorithmForLength(len(normalised))
	if !known {
		return "", "", errors.New(errors.CodeInvalidInput,
			"a digest of %d characters does not match any supported algorithm",
			len(normalised)).
			WithHint("expected %d characters for md5, %d for sha256, or %d for sha512",
				fs.DigestLength(fs.MD5), fs.DigestLength(fs.SHA256), fs.DigestLength(fs.SHA512))
	}

	if algorithm != "" && algorithm != detected {
		return "", "", errors.New(errors.CodeInvalidInput,
			"the digest is %d characters, which is %s, but --algorithm says %s",
			len(normalised), detected, algorithm).
			WithHint("leave --algorithm off and it will be worked out from the digest")
	}

	return normalised, detected, nil
}
