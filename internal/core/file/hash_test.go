package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

func TestHashProducesTheExpectedDigest(t *testing.T) {
	const content = "devnest"
	system := newFakeFS().addFile(root("payload.bin"), content)

	result, err := Hash(context.Background(), system, HashRequest{
		Paths:      []string{root("payload.bin")},
		Algorithms: []fs.Algorithm{fs.SHA256},
	})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	expected := sha256.Sum256([]byte(content))
	if len(result.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(result.Files))
	}
	if got := result.Files[0].Checksums[0].Value; got != hex.EncodeToString(expected[:]) {
		t.Errorf("digest = %q, want %q", got, hex.EncodeToString(expected[:]))
	}
	if result.Files[0].Bytes != int64(len(content)) {
		t.Errorf("Bytes = %d, want %d", result.Files[0].Bytes, len(content))
	}
}

func TestHashDefaultsToSHA256(t *testing.T) {
	system := newFakeFS().addFile(root("payload.bin"), "x")

	result, err := Hash(context.Background(), system, HashRequest{
		Paths: []string{root("payload.bin")},
	})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if result.Files[0].Checksums[0].Algorithm != string(fs.SHA256) {
		t.Errorf("algorithm = %q, want sha256", result.Files[0].Checksums[0].Algorithm)
	}
}

func TestHashComputesSeveralAlgorithms(t *testing.T) {
	system := newFakeFS().addFile(root("payload.bin"), "x")

	result, err := Hash(context.Background(), system, HashRequest{
		Paths:      []string{root("payload.bin")},
		Algorithms: fs.Algorithms(),
	})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	checksums := result.Files[0].Checksums
	if len(checksums) != len(fs.Algorithms()) {
		t.Fatalf("checksums = %d, want %d", len(checksums), len(fs.Algorithms()))
	}

	lengths := map[string]int{"md5": 32, "sha256": 64, "sha512": 128}
	for _, checksum := range checksums {
		if want := lengths[checksum.Algorithm]; len(checksum.Value) != want {
			t.Errorf("%s digest is %d characters, want %d",
				checksum.Algorithm, len(checksum.Value), want)
		}
	}
}

func TestHashHandlesSeveralFiles(t *testing.T) {
	system := newFakeFS().
		addFile(root("a.bin"), "a").
		addFile(root("b.bin"), "b")

	result, err := Hash(context.Background(), system, HashRequest{
		Paths: []string{root("a.bin"), root("b.bin")},
	})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if len(result.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(result.Files))
	}
	if result.Files[0].Checksums[0].Value == result.Files[1].Checksums[0].Value {
		t.Error("different content produced the same digest")
	}
}

// One bad path given on its own is worth failing on; in a batch it becomes a
// per-file problem so the rest still run.
func TestHashFailsOnASingleMissingFile(t *testing.T) {
	_, err := Hash(context.Background(), newFakeFS(), HashRequest{
		Paths: []string{root("missing.bin")},
	})
	assertCode(t, err, errors.CodeNotFound)
}

func TestHashReportsPerFileProblemsInABatch(t *testing.T) {
	system := newFakeFS().addFile(root("a.bin"), "a")

	result, err := Hash(context.Background(), system, HashRequest{
		Paths: []string{root("a.bin"), root("missing.bin")},
	})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if len(result.Files) != 1 {
		t.Errorf("files = %d, want the readable one", len(result.Files))
	}
	if len(result.Problems) != 1 {
		t.Fatalf("problems = %v, want one", result.Problems)
	}
	if result.Problems[0].Code != string(errors.CodeNotFound) {
		t.Errorf("problem code = %q", result.Problems[0].Code)
	}
}

// Hashing a directory means something different from hashing a file, so the
// command refuses rather than quietly doing something else.
func TestHashRejectsADirectory(t *testing.T) {
	system := newFakeFS().addDir(root("folder"))

	_, err := Hash(context.Background(), system, HashRequest{Paths: []string{root("folder")}})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestHashRequiresAPath(t *testing.T) {
	_, err := Hash(context.Background(), newFakeFS(), HashRequest{})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestHashResultCollectionsAreNeverNull(t *testing.T) {
	system := newFakeFS().addFile(root("a.bin"), "a")

	result, err := Hash(context.Background(), system, HashRequest{Paths: []string{root("a.bin")}})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if result.Problems == nil {
		t.Error("Problems is null; it must always be an array")
	}
}
