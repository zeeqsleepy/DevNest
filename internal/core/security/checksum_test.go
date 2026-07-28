package security

import (
	"context"
	"path/filepath"
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

// The checksum file and the files it lists live in one directory, which is
// where a release download lands.
const sumsPath = "/dist/SHA256SUMS"

// sumsHasher builds a directory holding a checksum file and the files it
// names, keyed the way the module will look them up.
func sumsHasher(contents string, files map[string]string) *fakeHasher {
	hasher := newFakeHasher().add(sumsPath, contents)
	for name, content := range files {
		hasher.add(filepath.Join(filepath.Dir(sumsPath), name), content)
	}
	return hasher
}

func verifySums(t *testing.T, hasher Hasher, only ...string) ChecksumFileResult {
	t.Helper()
	result, err := VerifyChecksumFile(context.Background(), hasher, ChecksumFileRequest{
		Path: sumsPath,
		Only: only,
	})
	if err != nil {
		t.Fatalf("VerifyChecksumFile: %v", err)
	}
	return result
}

func TestVerifyChecksumFileChecksEveryEntry(t *testing.T) {
	hasher := sumsHasher(
		abcSHA256+"  first.zip\n"+
			abcSHA256+" *second.zip\n",
		map[string]string{"first.zip": "abc", "second.zip": "not abc"},
	)

	result := verifySums(t, hasher)

	if result.Matched != 1 || result.Mismatched != 1 || result.Missing != 0 {
		t.Fatalf("result = %+v, want one match and one mismatch", result)
	}
	if result.Entries[0].Status != StatusMatch {
		t.Errorf("first.zip = %q, want %q", result.Entries[0].Status, StatusMatch)
	}
	// A mismatch still reports what was found, so the user can compare.
	if result.Entries[1].Actual == "" || result.Entries[1].Actual == result.Entries[1].Expected {
		t.Errorf("second.zip = %+v, want a differing digest reported", result.Entries[1])
	}
}

// A release publishes a digest for every platform it built and nobody
// downloads all of them, so an absent file is missing rather than failed.
func TestVerifyChecksumFileReportsMissingWithoutFailing(t *testing.T) {
	hasher := sumsHasher(
		abcSHA256+"  here.zip\n"+
			abcSHA256+"  elsewhere.zip\n",
		map[string]string{"here.zip": "abc"},
	)

	result := verifySums(t, hasher)

	if result.Matched != 1 || result.Missing != 1 {
		t.Fatalf("result = %+v, want one match and one missing", result)
	}
	if result.Entries[1].Status != StatusMissing {
		t.Errorf("elsewhere.zip = %q, want %q", result.Entries[1].Status, StatusMissing)
	}
	if result.Entries[1].Actual != "" {
		t.Errorf("a missing file reported a digest: %q", result.Entries[1].Actual)
	}
}

// Each line carries its own digest, so a file mixing algorithms works without
// anybody saying which is which.
func TestVerifyChecksumFileInfersEachLineSeparately(t *testing.T) {
	hasher := sumsHasher(
		"# checksums for the 1.0 release\n\n"+
			abcMD5+"  legacy.msi\n"+
			abcSHA256+"  modern.zip\n",
		map[string]string{"legacy.msi": "abc", "modern.zip": "abc"},
	)

	result := verifySums(t, hasher)

	if result.Matched != 2 {
		t.Fatalf("result = %+v, want both to match", result)
	}
	if result.Entries[0].Algorithm != string(fs.MD5) {
		t.Errorf("legacy.msi = %q, want md5", result.Entries[0].Algorithm)
	}
	if result.Entries[1].Algorithm != string(fs.SHA256) {
		t.Errorf("modern.zip = %q, want sha256", result.Entries[1].Algorithm)
	}
}

func TestVerifyChecksumFileChecksOnlyTheNamedFiles(t *testing.T) {
	hasher := sumsHasher(
		abcSHA256+"  wanted.zip\n"+
			abcSHA256+"  ignored.zip\n",
		map[string]string{"wanted.zip": "abc", "ignored.zip": "not abc"},
	)

	// A path on the command line still finds the bare name in the list.
	result := verifySums(t, hasher, filepath.Join("dist", "wanted.zip"))

	if len(result.Entries) != 1 || result.Entries[0].Name != "wanted.zip" {
		t.Fatalf("entries = %+v, want only wanted.zip", result.Entries)
	}
	if result.Matched != 1 {
		t.Errorf("result = %+v, want one match", result)
	}
}

// Asking about a file the checksum file does not cover must not answer with
// silence, which would read as a pass.
func TestVerifyChecksumFileRejectsANameItDoesNotCover(t *testing.T) {
	hasher := sumsHasher(abcSHA256+"  listed.zip\n", map[string]string{"listed.zip": "abc"})

	_, err := VerifyChecksumFile(context.Background(), hasher, ChecksumFileRequest{
		Path: sumsPath,
		Only: []string{"absent.zip"},
	})
	assertCode(t, err, errors.CodeNotFound)
}

func TestVerifyChecksumFileRejectsUnusableLines(t *testing.T) {
	tests := map[string]string{
		"no file name":  abcSHA256 + "\n",
		"no digest":     "just-a-name.zip is not a digest\n",
		"bad digest":    "zzz816bf  payload.zip\n",
		"absolute path": abcSHA256 + "  /etc/shadow\n",
		"traversal":     abcSHA256 + "  ../../.ssh/id_rsa\n",
		"backslash":     abcSHA256 + `  ..\..\secrets.txt` + "\n",
		"empty file":    "# nothing but a comment\n",
	}

	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := VerifyChecksumFile(context.Background(), sumsHasher(contents, nil),
				ChecksumFileRequest{Path: sumsPath})
			assertCode(t, err, errors.CodeInvalidInput)
		})
	}
}

func TestVerifyChecksumFileRequiresAPath(t *testing.T) {
	_, err := VerifyChecksumFile(context.Background(), newFakeHasher(), ChecksumFileRequest{})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestVerifyChecksumFileHonoursAnExplicitAlgorithm(t *testing.T) {
	hasher := sumsHasher(abcSHA256+"  payload.zip\n", map[string]string{"payload.zip": "abc"})

	_, err := VerifyChecksumFile(context.Background(), hasher, ChecksumFileRequest{
		Path:      sumsPath,
		Algorithm: fs.MD5,
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
