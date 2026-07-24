package security

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"io"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// fakeHasher stands in for the platform layer. Real files belong in the
// platform layer's own tests; here the question is whether the module asks for
// the right thing and reports what comes back.
type fakeHasher struct {
	files     map[string]string
	statErr   error
	digestErr error
	digested  []string
	readers   int
}

func newFakeHasher() *fakeHasher {
	return &fakeHasher{files: make(map[string]string)}
}

func (f *fakeHasher) add(path, content string) *fakeHasher {
	f.files[path] = content
	return f
}

func (f *fakeHasher) Resolve(path string) (string, error) {
	return path, nil
}

func (f *fakeHasher) Stat(path string) (fs.Entry, error) {
	if f.statErr != nil {
		return fs.Entry{}, f.statErr
	}
	content, known := f.files[path]
	if !known {
		if path == "/a-directory" {
			return fs.Entry{Path: path, IsDir: true}, nil
		}
		return fs.Entry{}, errors.New(errors.CodeNotFound, "cannot read %s", path)
	}
	return fs.Entry{Path: path, Bytes: int64(len(content))}, nil
}

func (f *fakeHasher) Digest(
	ctx context.Context,
	path string,
	algorithms []fs.Algorithm,
) ([]fs.Checksum, error) {
	if f.digestErr != nil {
		return nil, f.digestErr
	}
	content, known := f.files[path]
	if !known {
		return nil, errors.New(errors.CodeNotFound, "cannot read %s", path)
	}
	f.digested = append(f.digested, path)
	return f.DigestReader(ctx, strings.NewReader(content), algorithms)
}

func (f *fakeHasher) DigestReader(
	_ context.Context,
	reader io.Reader,
	algorithms []fs.Algorithm,
) ([]fs.Checksum, error) {
	if f.digestErr != nil {
		return nil, f.digestErr
	}
	f.readers++

	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	checksums := make([]fs.Checksum, 0, len(algorithms))
	for _, algorithm := range algorithms {
		var digest hash.Hash
		switch algorithm {
		case fs.MD5:
			digest = md5.New()
		case fs.SHA512:
			digest = sha512.New()
		default:
			digest = sha256.New()
		}
		digest.Write(content)
		checksums = append(checksums, fs.Checksum{
			Algorithm: string(algorithm),
			Value:     hex.EncodeToString(digest.Sum(nil)),
		})
	}
	return checksums, nil
}

// Published SHA-256 of "abc", so the result is checked against something
// outside this codebase.
const abcSHA256 = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"

func TestHashText(t *testing.T) {
	result, err := Hash(context.Background(), newFakeHasher(), HashRequest{
		Source: SourceText,
		Text:   "abc",
	})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if result.Source != SourceText {
		t.Errorf("Source = %q", result.Source)
	}
	if result.Bytes != 3 {
		t.Errorf("Bytes = %d, want 3", result.Bytes)
	}
	if result.Checksums[0].Value != abcSHA256 {
		t.Errorf("digest = %q, want %q", result.Checksums[0].Value, abcSHA256)
	}
}

func TestHashDefaultsToSHA256(t *testing.T) {
	result, err := Hash(context.Background(), newFakeHasher(), HashRequest{
		Source: SourceText, Text: "abc",
	})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if result.Checksums[0].Algorithm != string(fs.SHA256) {
		t.Errorf("algorithm = %q, want sha256", result.Checksums[0].Algorithm)
	}
}

// Hashing an empty string is a legitimate thing to want and has a well-defined
// answer.
func TestHashEmptyText(t *testing.T) {
	result, err := Hash(context.Background(), newFakeHasher(), HashRequest{
		Source: SourceText, Text: "",
	})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if result.Checksums[0].Value != emptySHA256 {
		t.Errorf("digest = %q, want %q", result.Checksums[0].Value, emptySHA256)
	}
}

func TestHashSeveralAlgorithmsInOnePass(t *testing.T) {
	hasher := newFakeHasher()

	result, err := Hash(context.Background(), hasher, HashRequest{
		Source:     SourceText,
		Text:       "abc",
		Algorithms: fs.Algorithms(),
	})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if len(result.Checksums) != 3 {
		t.Fatalf("checksums = %d, want 3", len(result.Checksums))
	}
	if hasher.readers != 1 {
		t.Errorf("read the input %d times, want once", hasher.readers)
	}

	lengths := map[string]int{"md5": 32, "sha256": 64, "sha512": 128}
	for _, checksum := range result.Checksums {
		if want := lengths[checksum.Algorithm]; len(checksum.Value) != want {
			t.Errorf("%s digest is %d characters, want %d",
				checksum.Algorithm, len(checksum.Value), want)
		}
	}
}

// A file and a string of the same content must agree, or the two commands
// would be answering different questions with the same name.
func TestHashFileAndTextAgree(t *testing.T) {
	hasher := newFakeHasher().add("/payload.txt", "abc")

	fromFile, err := Hash(context.Background(), hasher, HashRequest{
		Source: SourceFile, Path: "/payload.txt",
	})
	if err != nil {
		t.Fatalf("Hash(file): %v", err)
	}
	fromText, err := Hash(context.Background(), hasher, HashRequest{
		Source: SourceText, Text: "abc",
	})
	if err != nil {
		t.Fatalf("Hash(text): %v", err)
	}

	if fromFile.Checksums[0].Value != fromText.Checksums[0].Value {
		t.Errorf("file digest %q differs from text digest %q",
			fromFile.Checksums[0].Value, fromText.Checksums[0].Value)
	}
	if fromFile.Source != SourceFile || fromFile.Path != "/payload.txt" {
		t.Errorf("result = %+v", fromFile)
	}
}

func TestHashMissingFile(t *testing.T) {
	_, err := Hash(context.Background(), newFakeHasher(), HashRequest{
		Source: SourceFile, Path: "/absent.txt",
	})
	assertCode(t, err, errors.CodeNotFound)
}

func TestHashRejectsADirectory(t *testing.T) {
	_, err := Hash(context.Background(), newFakeHasher(), HashRequest{
		Source: SourceFile, Path: "/a-directory",
	})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestHashRequiresASource(t *testing.T) {
	_, err := Hash(context.Background(), newFakeHasher(), HashRequest{})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestHashRejectsAnEmptyPath(t *testing.T) {
	_, err := Hash(context.Background(), newFakeHasher(), HashRequest{
		Source: SourceFile, Path: "   ",
	})
	assertCode(t, err, errors.CodeInvalidInput)
}
