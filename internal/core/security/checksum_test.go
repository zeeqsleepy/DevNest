package security

import (
	"context"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// Published digests of "abc".
const (
	abcMD5    = "900150983cd24fb0d6963f7d28e17f72"
	abcSHA512 = "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a" +
		"2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f"
)

func TestVerifyChecksumMatches(t *testing.T) {
	hasher := newFakeHasher().add("/payload.txt", "abc")

	result, err := VerifyChecksum(context.Background(), hasher, ChecksumRequest{
		Path:     "/payload.txt",
		Expected: abcSHA256,
	})
	if err != nil {
		t.Fatalf("VerifyChecksum: %v", err)
	}

	if !result.Match {
		t.Error("Match = false for a correct digest")
	}
	if result.Algorithm != string(fs.SHA256) {
		t.Errorf("Algorithm = %q, want sha256", result.Algorithm)
	}
	// Both digests are reported even on a match, so the user has something to
	// check when they start doubting the tool.
	if result.Expected == "" || result.Actual == "" {
		t.Errorf("result = %+v, want both digests reported", result)
	}
}

// A mismatch is a result, not an error: finding out that a download is wrong
// is the whole point.
func TestVerifyChecksumMismatchIsAResult(t *testing.T) {
	hasher := newFakeHasher().add("/payload.txt", "different content")

	result, err := VerifyChecksum(context.Background(), hasher, ChecksumRequest{
		Path:     "/payload.txt",
		Expected: abcSHA256,
	})
	if err != nil {
		t.Fatalf("VerifyChecksum returned an error for a mismatch: %v", err)
	}

	if result.Match {
		t.Error("Match = true for a wrong digest")
	}
	if result.Actual == result.Expected {
		t.Error("the reported digests are identical although they did not match")
	}
}

// Every supported algorithm has a distinct output length, so someone pasting a
// digest from a release page does not have to say which kind it is.
func TestVerifyChecksumInfersTheAlgorithm(t *testing.T) {
	hasher := newFakeHasher().add("/payload.txt", "abc")

	tests := map[string]fs.Algorithm{
		abcMD5:    fs.MD5,
		abcSHA256: fs.SHA256,
		abcSHA512: fs.SHA512,
	}

	for digest, want := range tests {
		result, err := VerifyChecksum(context.Background(), hasher, ChecksumRequest{
			Path:     "/payload.txt",
			Expected: digest,
		})
		if err != nil {
			t.Fatalf("VerifyChecksum(%s): %v", want, err)
		}
		if result.Algorithm != string(want) {
			t.Errorf("Algorithm = %q, want %q", result.Algorithm, want)
		}
		if !result.Match {
			t.Errorf("%s did not match", want)
		}
	}
}

func TestVerifyChecksumAcceptsUppercaseAndWhitespace(t *testing.T) {
	hasher := newFakeHasher().add("/payload.txt", "abc")

	for _, expected := range []string{
		strings.ToUpper(abcSHA256),
		"  " + abcSHA256 + "  ",
		"*" + abcSHA256,
	} {
		result, err := VerifyChecksum(context.Background(), hasher, ChecksumRequest{
			Path:     "/payload.txt",
			Expected: expected,
		})
		if err != nil {
			t.Fatalf("VerifyChecksum(%q): %v", expected, err)
		}
		if !result.Match {
			t.Errorf("%q did not match", expected)
		}
	}
}

func TestVerifyChecksumHonoursAnExplicitAlgorithm(t *testing.T) {
	hasher := newFakeHasher().add("/payload.txt", "abc")

	result, err := VerifyChecksum(context.Background(), hasher, ChecksumRequest{
		Path:      "/payload.txt",
		Expected:  abcMD5,
		Algorithm: fs.MD5,
	})
	if err != nil {
		t.Fatalf("VerifyChecksum: %v", err)
	}
	if !result.Match {
		t.Error("an explicit md5 did not match")
	}
}

// A stated algorithm that disagrees with the digest length is a mistake worth
// reporting rather than quietly overriding.
func TestVerifyChecksumRejectsAContradictoryAlgorithm(t *testing.T) {
	hasher := newFakeHasher().add("/payload.txt", "abc")

	_, err := VerifyChecksum(context.Background(), hasher, ChecksumRequest{
		Path:      "/payload.txt",
		Expected:  abcSHA256,
		Algorithm: fs.MD5,
	})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestVerifyChecksumRejectsMalformedDigests(t *testing.T) {
	hasher := newFakeHasher().add("/payload.txt", "abc")

	tests := map[string]string{
		"empty":         "",
		"not hex":       "zzzz816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		"wrong length":  "abcdef",
		"has spaces":    "ba78 16bf 8f01 cfea",
		"almost sha256": abcSHA256[:63],
		"too long":      abcSHA512 + "00",
	}

	for name, expected := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := VerifyChecksum(context.Background(), hasher, ChecksumRequest{
				Path:     "/payload.txt",
				Expected: expected,
			})
			assertCode(t, err, errors.CodeInvalidInput)
		})
	}
}

func TestVerifyChecksumMissingFile(t *testing.T) {
	_, err := VerifyChecksum(context.Background(), newFakeHasher(), ChecksumRequest{
		Path:     "/absent.txt",
		Expected: abcSHA256,
	})
	assertCode(t, err, errors.CodeNotFound)
}

func TestVerifyChecksumRejectsADirectory(t *testing.T) {
	_, err := VerifyChecksum(context.Background(), newFakeHasher(), ChecksumRequest{
		Path:     "/a-directory",
		Expected: abcSHA256,
	})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestVerifyChecksumRequiresAPath(t *testing.T) {
	_, err := VerifyChecksum(context.Background(), newFakeHasher(), ChecksumRequest{
		Path:     "  ",
		Expected: abcSHA256,
	})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestParseExpectedDetectsEachAlgorithm(t *testing.T) {
	tests := map[int]fs.Algorithm{
		32:  fs.MD5,
		64:  fs.SHA256,
		128: fs.SHA512,
	}

	for length, want := range tests {
		digest := strings.Repeat("a", length)
		_, algorithm, err := parseExpected(digest, "")
		if err != nil {
			t.Fatalf("parseExpected(%d characters): %v", length, err)
		}
		if algorithm != want {
			t.Errorf("length %d gave %q, want %q", length, algorithm, want)
		}
	}
}
