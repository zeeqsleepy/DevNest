package file

import (
	"context"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

func duplicateRequest() DuplicateRequest {
	return DuplicateRequest{
		Selection: Selection{Root: root(), Recursive: true},
		Algorithm: fs.SHA256,
	}
}

// Identical content under different names is the case the whole command
// exists for.
func TestDuplicatesMatchContentNotNames(t *testing.T) {
	system := newFakeFS().
		addFile(root("invoice.pdf"), "same content").
		addFile(root("archive", "copy-of-invoice.pdf"), "same content").
		addFile(root("other.pdf"), "different content")

	result, err := Duplicates(context.Background(), system, duplicateRequest())
	if err != nil {
		t.Fatalf("Duplicates: %v", err)
	}

	if len(result.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(result.Groups))
	}
	if len(result.Groups[0].Duplicates) != 1 {
		t.Fatalf("duplicates = %d, want 1", len(result.Groups[0].Duplicates))
	}
	if result.Groups[0].Hash == "" {
		t.Error("the group carries no hash")
	}
}

// OnProgress reports each file as it is hashed, so a long search can be
// shown moving rather than waited on in silence.
func TestDuplicatesReportsProgressPerFile(t *testing.T) {
	system := newFakeFS().
		addFile(root("a.bin"), "same").
		addFile(root("b.bin"), "same").
		addFile(root("c.bin"), "different")

	var progress []int
	request := duplicateRequest()
	request.OnProgress = func(hashed, total int) { progress = append(progress, hashed) }

	if _, err := Duplicates(context.Background(), system, request); err != nil {
		t.Fatalf("Duplicates: %v", err)
	}

	if len(progress) != 2 {
		t.Errorf("OnProgress called %d times, want 2 candidates hashed", len(progress))
	}
}

// Files of the same size but different content must not be reported.
func TestDuplicatesDoNotMatchOnSizeAlone(t *testing.T) {
	system := newFakeFS().
		addFile(root("a.bin"), "aaaa").
		addFile(root("b.bin"), "bbbb")

	result, err := Duplicates(context.Background(), system, duplicateRequest())
	if err != nil {
		t.Fatalf("Duplicates: %v", err)
	}
	if len(result.Groups) != 0 {
		t.Errorf("groups = %v, want none", result.Groups)
	}
	if result.FilesHashed != 2 {
		t.Errorf("FilesHashed = %d, want both candidates hashed", result.FilesHashed)
	}
}

// Only files sharing a size are read. Everything with a unique size is
// eliminated for free, which is the optimisation that makes this usable on a
// large tree.
func TestDuplicatesOnlyHashSizeCandidates(t *testing.T) {
	system := newFakeFS().
		addFile(root("one.txt"), "a").
		addFile(root("two.txt"), "bb").
		addFile(root("three.txt"), "ccc").
		addFile(root("four.txt"), "dd")

	result, err := Duplicates(context.Background(), system, duplicateRequest())
	if err != nil {
		t.Fatalf("Duplicates: %v", err)
	}

	if result.FilesScanned != 4 {
		t.Errorf("FilesScanned = %d, want 4", result.FilesScanned)
	}
	if result.FilesHashed != 2 {
		t.Errorf("FilesHashed = %d, want only the two files sharing a size", result.FilesHashed)
	}
}

func TestDuplicatesChooseTheOldestAsOriginal(t *testing.T) {
	older := time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	system := newFakeFS().
		addFileAt(root("newer.txt"), "shared", newer).
		addFileAt(root("older.txt"), "shared", older)

	result, err := Duplicates(context.Background(), system, duplicateRequest())
	if err != nil {
		t.Fatalf("Duplicates: %v", err)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(result.Groups))
	}
	if result.Groups[0].Original.Name != "older.txt" {
		t.Errorf("original = %q, want the oldest file", result.Groups[0].Original.Name)
	}
}

func TestDuplicatesReportWastedSpace(t *testing.T) {
	system := newFakeFS().
		addFile(root("a.bin"), "0123456789").
		addFile(root("b.bin"), "0123456789").
		addFile(root("c.bin"), "0123456789")

	result, err := Duplicates(context.Background(), system, duplicateRequest())
	if err != nil {
		t.Fatalf("Duplicates: %v", err)
	}

	// Three copies of a ten-byte file waste the two extra copies.
	if result.Wasted != 20 {
		t.Errorf("Wasted = %d, want 20", result.Wasted)
	}
	if result.Duplicates != 2 {
		t.Errorf("Duplicates = %d, want 2", result.Duplicates)
	}
}

// Empty files are all identical to each other, which is true and useless.
func TestDuplicatesIgnoreFilesBelowMinimumSize(t *testing.T) {
	system := newFakeFS().
		addFile(root("empty-one.txt"), "").
		addFile(root("empty-two.txt"), "")

	result, err := Duplicates(context.Background(), system, duplicateRequest())
	if err != nil {
		t.Fatalf("Duplicates: %v", err)
	}
	if len(result.Groups) != 0 {
		t.Errorf("groups = %v, want none", result.Groups)
	}
}

func TestDuplicatesRespectMinimumSize(t *testing.T) {
	system := newFakeFS().
		addFile(root("small-a.txt"), "ab").
		addFile(root("small-b.txt"), "ab").
		addFile(root("large-a.txt"), "0123456789").
		addFile(root("large-b.txt"), "0123456789")

	request := duplicateRequest()
	request.MinBytes = 5

	result, err := Duplicates(context.Background(), system, request)
	if err != nil {
		t.Fatalf("Duplicates: %v", err)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("groups = %d, want only the large pair", len(result.Groups))
	}
	if result.Groups[0].Bytes != 10 {
		t.Errorf("group size = %d, want 10", result.Groups[0].Bytes)
	}
}

func TestDuplicatesRecordUnreadableFilesAsProblems(t *testing.T) {
	system := newFakeFS().
		addFile(root("a.bin"), "shared").
		addFile(root("b.bin"), "shared")
	system.failRead[root("a.bin")] = errors.New(errors.CodePermissionDenied, "access is denied")

	result, err := Duplicates(context.Background(), system, duplicateRequest())
	if err != nil {
		t.Fatalf("Duplicates: %v", err)
	}
	if len(result.Problems) == 0 {
		t.Error("the unreadable file was not reported")
	}
	if len(result.Groups) != 0 {
		t.Error("a group was reported from a single readable file")
	}
}

func TestDuplicatesHonourTheChosenAlgorithm(t *testing.T) {
	system := newFakeFS().
		addFile(root("a.bin"), "shared").
		addFile(root("b.bin"), "shared")

	request := duplicateRequest()
	request.Algorithm = fs.MD5

	result, err := Duplicates(context.Background(), system, request)
	if err != nil {
		t.Fatalf("Duplicates: %v", err)
	}
	if result.Algorithm != string(fs.MD5) {
		t.Errorf("Algorithm = %q, want md5", result.Algorithm)
	}
	if len(result.Groups[0].Hash) != 32 {
		t.Errorf("hash length = %d, want an md5 digest", len(result.Groups[0].Hash))
	}
}
