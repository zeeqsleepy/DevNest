//go:build integration

// Integration tests: the file module driven against a real disk.
//
// The module's own tests run against an in-memory fake, which is fast and
// portable but proves nothing about how a real filesystem behaves. The
// platform layer has its own tests beside the code, covering walking, moving,
// hashing, and path comparison directly. What is left for here is the join
// between them: a whole operation, end to end, on a real tree.
//
// Run with: make test-integration
package tests

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/core/file"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

func write(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent of %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func tree(t *testing.T) string {
	t.Helper()
	base := t.TempDir()

	write(t, filepath.Join(base, "photo.jpg"), "image bytes")
	write(t, filepath.Join(base, "manual.pdf"), "document bytes")
	write(t, filepath.Join(base, "copy.pdf"), "document bytes")
	write(t, filepath.Join(base, "notes.txt"), "some notes")
	write(t, filepath.Join(base, "src", "main.go"), "package main")
	write(t, filepath.Join(base, ".hidden"), "hidden")

	return base
}

func TestOrganizeOnARealDirectory(t *testing.T) {
	base := tree(t)

	plan, err := file.Organize(context.Background(), fs.System{}, file.OrganizeRequest{
		Selection: file.Selection{Root: base},
	})
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}
	if plan.Applied {
		t.Error("a dry run reported itself as applied")
	}
	if _, err := os.Stat(filepath.Join(base, "photo.jpg")); err != nil {
		t.Fatal("a dry run moved a file")
	}

	done, err := file.Organize(context.Background(), fs.System{}, file.OrganizeRequest{
		Selection: file.Selection{Root: base},
		Apply:     true,
	})
	if err != nil {
		t.Fatalf("Organize --apply: %v", err)
	}
	if done.Moved == 0 {
		t.Fatal("nothing moved")
	}

	moved := filepath.Join(base, "Images", "jpg", "photo.jpg")
	content, err := os.ReadFile(moved)
	if err != nil {
		t.Fatalf("read moved file: %v", err)
	}
	if string(content) != "image bytes" {
		t.Errorf("content = %q, the move lost data", content)
	}
	if _, err := os.Stat(filepath.Join(base, ".hidden")); err != nil {
		t.Error("a hidden file was moved without --include-hidden")
	}
	if _, err := os.Stat(filepath.Join(base, "src", "main.go")); err != nil {
		t.Error("a file in a subdirectory was moved without --recursive")
	}
}

// Running organize twice must leave the same tree as running it once.
func TestOrganizeIsIdempotentOnARealDirectory(t *testing.T) {
	base := tree(t)
	request := file.OrganizeRequest{Selection: file.Selection{Root: base}, Apply: true}

	if _, err := file.Organize(context.Background(), fs.System{}, request); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	second, err := file.Organize(context.Background(), fs.System{}, request)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if second.Moved != 0 {
		t.Errorf("the second pass moved %d files, want 0", second.Moved)
	}
}

func TestDuplicatesOnARealDirectory(t *testing.T) {
	base := tree(t)

	result, err := file.Duplicates(context.Background(), fs.System{}, file.DuplicateRequest{
		Selection: file.Selection{Root: base, Recursive: true},
	})
	if err != nil {
		t.Fatalf("Duplicates: %v", err)
	}

	if len(result.Groups) != 1 {
		t.Fatalf("groups = %d, want the two identical pdf files", len(result.Groups))
	}
	if len(result.Groups[0].Duplicates) != 1 {
		t.Errorf("duplicates = %d, want 1", len(result.Groups[0].Duplicates))
	}
	if result.Groups[0].Hash == "" {
		t.Error("the group carries no hash")
	}
}

func TestRenameOnARealDirectory(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "IMG_1.jpg"), "one")
	write(t, filepath.Join(base, "IMG_2.jpg"), "two")

	result, err := file.RenameFiles(context.Background(), fs.System{}, file.RenameRequest{
		Selection: file.Selection{Root: base},
		Replace:   []file.Replacement{{From: "IMG_", To: "photo-"}},
		Apply:     true,
	})
	if err != nil {
		t.Fatalf("RenameFiles: %v", err)
	}
	if result.Renamed != 2 {
		t.Fatalf("Renamed = %d, want 2", result.Renamed)
	}

	if _, err := os.Stat(filepath.Join(base, "photo-1.jpg")); err != nil {
		t.Errorf("renamed file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "IMG_1.jpg")); !os.IsNotExist(err) {
		t.Error("the original name still exists")
	}
}

// A conflict must abort the batch with the disk untouched.
func TestRenameConflictLeavesTheDirectoryUnchanged(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "draft.txt"), "a")
	write(t, filepath.Join(base, "final.txt"), "b")

	_, err := file.RenameFiles(context.Background(), fs.System{}, file.RenameRequest{
		Selection: file.Selection{Root: base},
		Match:     "draft.txt",
		Replace:   []file.Replacement{{From: "draft", To: "final"}},
		Apply:     true,
	})
	if errors.CodeOf(err) != errors.CodeConflict {
		t.Fatalf("code = %q, want %q (%v)", errors.CodeOf(err), errors.CodeConflict, err)
	}

	content, readErr := os.ReadFile(filepath.Join(base, "final.txt"))
	if readErr != nil || string(content) != "b" {
		t.Error("the existing file was replaced")
	}
	if _, err := os.Stat(filepath.Join(base, "draft.txt")); err != nil {
		t.Error("the source was renamed despite the conflict")
	}
}

func TestFilterOnARealDirectory(t *testing.T) {
	base := tree(t)

	result, err := file.Filter(context.Background(), fs.System{}, file.FilterRequest{
		Selection:  file.Selection{Root: base, Recursive: true},
		Extensions: []string{"pdf"},
	})
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	if result.Matched != 2 {
		t.Errorf("matched = %d, want the two pdf files", result.Matched)
	}
}

func TestSizeOnARealDirectory(t *testing.T) {
	base := tree(t)

	result, err := file.Size(context.Background(), fs.System{}, file.SizeRequest{
		Selection: file.Selection{Root: base},
	})
	if err != nil {
		t.Fatalf("Size: %v", err)
	}

	if result.TotalFiles != 5 {
		t.Errorf("TotalFiles = %d, want the five visible files", result.TotalFiles)
	}
	if result.TotalBytes == 0 {
		t.Error("TotalBytes = 0")
	}
	if len(result.LargestFiles) == 0 {
		t.Error("no largest files were reported")
	}
}

func TestHashOnARealFile(t *testing.T) {
	path := write(t, filepath.Join(t.TempDir(), "payload.bin"), "abc")

	result, err := file.Hash(context.Background(), fs.System{}, file.HashRequest{
		Paths: []string{path},
	})
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if result.Files[0].Checksums[0].Value != want {
		t.Errorf("digest = %q, want %q", result.Files[0].Checksums[0].Value, want)
	}
}

// The guard is what stands between a mistyped path and an incident.
func TestOrganizeRefusesTheHomeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this system: %v", err)
	}

	_, err = file.Organize(context.Background(), fs.System{}, file.OrganizeRequest{
		Selection: file.Selection{Root: home},
	})
	if err == nil {
		t.Fatal("organize accepted the home directory without --force")
	}
	if errors.CodeOf(err) != errors.CodeInvalidInput {
		t.Errorf("code = %q, want %q", errors.CodeOf(err), errors.CodeInvalidInput)
	}
	if !strings.Contains(err.Error(), "refusing") {
		t.Errorf("error = %q, want it to explain the refusal", err.Error())
	}
}
