package fs

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

// Published digests of "abc", so the implementation is checked against
// something outside itself rather than against its own output.
const (
	abcSHA256 = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	abcSHA512 = "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a" +
		"2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f"
	abcMD5 = "900150983cd24fb0d6963f7d28e17f72"
)

func TestDigestMatchesPublishedValues(t *testing.T) {
	path := write(t, filepath.Join(t.TempDir(), "payload.bin"), "abc")

	checksums, err := System{}.Digest(context.Background(), path, Algorithms())
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}

	want := map[string]string{"sha256": abcSHA256, "sha512": abcSHA512, "md5": abcMD5}
	if len(checksums) != len(want) {
		t.Fatalf("checksums = %d, want %d", len(checksums), len(want))
	}
	for _, checksum := range checksums {
		if checksum.Value != want[checksum.Algorithm] {
			t.Errorf("%s = %q, want %q", checksum.Algorithm, checksum.Value, want[checksum.Algorithm])
		}
	}
}

// Every requested digest comes from one pass over the file, so three checksums
// of a large file cost one read rather than three.
func TestDigestReturnsAlgorithmsInTheOrderRequested(t *testing.T) {
	path := write(t, filepath.Join(t.TempDir(), "payload.bin"), "abc")

	checksums, err := System{}.Digest(context.Background(), path,
		[]Algorithm{MD5, SHA512, SHA256})
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}

	order := []string{"md5", "sha512", "sha256"}
	for index, checksum := range checksums {
		if checksum.Algorithm != order[index] {
			t.Errorf("position %d = %q, want %q", index, checksum.Algorithm, order[index])
		}
	}
}

// A file larger than the read buffer exercises the streaming loop rather than
// a single read, which is the path that keeps memory use flat.
func TestDigestStreamsAFileLargerThanTheBuffer(t *testing.T) {
	content := strings.Repeat("devnest", digestBuffer/2)
	path := write(t, filepath.Join(t.TempDir(), "large.bin"), content)

	checksums, err := System{}.Digest(context.Background(), path, []Algorithm{SHA256})
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if len(checksums[0].Value) != 64 {
		t.Errorf("digest = %q", checksums[0].Value)
	}
}

func TestDigestOfAnEmptyFile(t *testing.T) {
	path := write(t, filepath.Join(t.TempDir(), "empty.bin"), "")

	checksums, err := System{}.Digest(context.Background(), path, []Algorithm{SHA256})
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if checksums[0].Value != emptySHA256 {
		t.Errorf("digest = %q, want %q", checksums[0].Value, emptySHA256)
	}
}

func TestDigestMissingFileIsNotFound(t *testing.T) {
	_, err := System{}.Digest(context.Background(),
		filepath.Join(t.TempDir(), "absent"), []Algorithm{SHA256})

	if errors.CodeOf(err) != errors.CodeNotFound {
		t.Errorf("code = %q, want %q", errors.CodeOf(err), errors.CodeNotFound)
	}
}

func TestDigestRequiresAnAlgorithm(t *testing.T) {
	path := write(t, filepath.Join(t.TempDir(), "payload.bin"), "abc")

	_, err := System{}.Digest(context.Background(), path, nil)
	if errors.CodeOf(err) != errors.CodeInvalidInput {
		t.Errorf("code = %q, want %q", errors.CodeOf(err), errors.CodeInvalidInput)
	}
}

func TestDigestStopsOnCancellation(t *testing.T) {
	path := write(t, filepath.Join(t.TempDir(), "payload.bin"), strings.Repeat("x", 1024))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := System{}.Digest(ctx, path, []Algorithm{SHA256})
	if errors.CodeOf(err) != errors.CodeCancelled {
		t.Errorf("code = %q, want %q (%v)", errors.CodeOf(err), errors.CodeCancelled, err)
	}
}

// DigestReader is what lets the security module hash a string using exactly
// the same code that hashes a file. If the two disagreed, two commands would
// be answering different questions under one name.
func TestDigestReaderAgreesWithDigest(t *testing.T) {
	path := write(t, filepath.Join(t.TempDir(), "payload.bin"), "abc")
	system := System{}

	fromFile, err := system.Digest(context.Background(), path, Algorithms())
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	fromReader, err := system.DigestReader(context.Background(),
		strings.NewReader("abc"), Algorithms())
	if err != nil {
		t.Fatalf("DigestReader: %v", err)
	}

	if len(fromFile) != len(fromReader) {
		t.Fatalf("got %d and %d checksums", len(fromFile), len(fromReader))
	}
	for index := range fromFile {
		if fromFile[index] != fromReader[index] {
			t.Errorf("%s: file gave %q, reader gave %q",
				fromFile[index].Algorithm, fromFile[index].Value, fromReader[index].Value)
		}
	}
}

func TestDigestReaderMatchesPublishedValues(t *testing.T) {
	checksums, err := System{}.DigestReader(context.Background(),
		strings.NewReader("abc"), []Algorithm{SHA256})
	if err != nil {
		t.Fatalf("DigestReader: %v", err)
	}
	if checksums[0].Value != abcSHA256 {
		t.Errorf("digest = %q, want %q", checksums[0].Value, abcSHA256)
	}
}

func TestDigestReaderOnAnEmptyStream(t *testing.T) {
	checksums, err := System{}.DigestReader(context.Background(),
		strings.NewReader(""), []Algorithm{SHA256})
	if err != nil {
		t.Fatalf("DigestReader: %v", err)
	}
	const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if checksums[0].Value != emptySHA256 {
		t.Errorf("digest = %q, want %q", checksums[0].Value, emptySHA256)
	}
}

func TestDigestReaderRequiresAnAlgorithm(t *testing.T) {
	_, err := System{}.DigestReader(context.Background(), strings.NewReader("abc"), nil)
	if errors.CodeOf(err) != errors.CodeInvalidInput {
		t.Errorf("code = %q, want %q", errors.CodeOf(err), errors.CodeInvalidInput)
	}
}

func TestDigestReaderStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := System{}.DigestReader(ctx,
		strings.NewReader(strings.Repeat("x", 1024)), []Algorithm{SHA256})
	if errors.CodeOf(err) != errors.CodeCancelled {
		t.Errorf("code = %q, want %q (%v)", errors.CodeOf(err), errors.CodeCancelled, err)
	}
}

// Every supported algorithm has a distinct output length, which is what lets a
// checksum be recognised without the user saying which kind it is.
func TestDigestLengthsAreDistinct(t *testing.T) {
	seen := make(map[int]Algorithm)

	for _, algorithm := range Algorithms() {
		length := DigestLength(algorithm)
		if length == 0 {
			t.Errorf("%s reports no digest length", algorithm)
		}
		if previous, clash := seen[length]; clash {
			t.Errorf("%s and %s both produce %d characters", previous, algorithm, length)
		}
		seen[length] = algorithm
	}

	if DigestLength("sha1") != 0 {
		t.Error("an unsupported algorithm reported a length")
	}
}

// The reported length must match what the algorithm actually produces, or the
// checksum command would reject valid digests.
func TestDigestLengthMatchesRealOutput(t *testing.T) {
	for _, algorithm := range Algorithms() {
		checksums, err := System{}.DigestReader(context.Background(),
			strings.NewReader("abc"), []Algorithm{algorithm})
		if err != nil {
			t.Fatalf("DigestReader(%s): %v", algorithm, err)
		}
		if got := len(checksums[0].Value); got != DigestLength(algorithm) {
			t.Errorf("%s produced %d characters, DigestLength says %d",
				algorithm, got, DigestLength(algorithm))
		}
	}
}

func TestAlgorithmForLength(t *testing.T) {
	for _, algorithm := range Algorithms() {
		found, known := AlgorithmForLength(DigestLength(algorithm))
		if !known || found != algorithm {
			t.Errorf("AlgorithmForLength(%d) = %q, %v; want %q",
				DigestLength(algorithm), found, known, algorithm)
		}
	}

	if _, known := AlgorithmForLength(40); known {
		t.Error("a 40-character digest was matched to a supported algorithm")
	}
	if _, known := AlgorithmForLength(0); known {
		t.Error("a zero length was matched to an algorithm")
	}
}

func TestParseAlgorithm(t *testing.T) {
	for _, name := range []string{"sha256", "SHA256", " sha256 ", "md5", "sha512"} {
		if _, err := ParseAlgorithm(name); err != nil {
			t.Errorf("ParseAlgorithm(%q): %v", name, err)
		}
	}

	// SHA-1 is deliberately not offered; the error has to say what is.
	_, err := ParseAlgorithm("sha1")
	if errors.CodeOf(err) != errors.CodeInvalidInput {
		t.Fatalf("code = %q, want %q", errors.CodeOf(err), errors.CodeInvalidInput)
	}
	if hint := errors.Classify(err).Hint; !strings.Contains(hint, "sha256") {
		t.Errorf("hint = %q, want it to list the supported algorithms", hint)
	}
}

func TestAlgorithmsAreAllParseable(t *testing.T) {
	for _, algorithm := range Algorithms() {
		parsed, err := ParseAlgorithm(string(algorithm))
		if err != nil || parsed != algorithm {
			t.Errorf("ParseAlgorithm(%q) = %q, %v", algorithm, parsed, err)
		}
		if _, err := newHash(algorithm); err != nil {
			t.Errorf("newHash(%q): %v", algorithm, err)
		}
	}
}
