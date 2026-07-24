package benchmarks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/devnest/devnest/internal/core/clean"
	"github.com/devnest/devnest/internal/core/scan"
	"github.com/devnest/devnest/internal/core/secret"
	"github.com/devnest/devnest/internal/platform/fs"
)

// benchFiles is the size of the generated project tree. Ten thousand is the
// number the targets in docs/performance.md are written against, and it is
// generated once per benchmark rather than per iteration: creating files is
// slower than walking them, and including that in the measurement would be
// measuring the operating system's file creation instead.
const benchFiles = 10_000

// writeProjectTree generates a tree shaped like a real project: source files
// in nested directories, a dependency directory the walk is meant to skip, and
// build output for the cleanup rules to find.
func writeProjectTree(b *testing.B, files int) string {
	b.Helper()

	root := b.TempDir()
	source := "package main\n\n// a comment\nfunc main() {\n\tprintln(\"hello\")\n}\n"

	// A marker, so the cleanup rules recognise the build directories as build
	// output rather than as somebody's work.
	write(b, filepath.Join(root, "go.mod"), "module bench\n\ngo 1.25\n")

	for index := range files {
		var directory string
		switch {
		case index%10 == 0:
			directory = filepath.Join(root, "node_modules", fmt.Sprintf("pkg%d", index%50))
		case index%17 == 0:
			directory = filepath.Join(root, "dist", fmt.Sprintf("chunk%d", index%20))
		default:
			directory = filepath.Join(root, "src", fmt.Sprintf("area%d", index%40))
		}
		write(b, filepath.Join(directory, fmt.Sprintf("file%d.go", index)), source)
	}

	return root
}

func write(b *testing.B, path, contents string) {
	b.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		b.Fatalf("write: %v", err)
	}
}

func BenchmarkScanSummary(b *testing.B) {
	root := writeProjectTree(b, benchFiles)

	b.ResetTimer()
	for range b.N {
		if _, err := scan.Summarize(context.Background(), fs.System{},
			scan.SummaryRequest{Selection: scan.Selection{Root: root}}); err != nil {
			b.Fatalf("Summarize: %v", err)
		}
	}
}

// The type breakdown walks the same tree and reads nothing, so the gap between
// this and the line count is the cost of opening every file.
func BenchmarkScanTypes(b *testing.B) {
	root := writeProjectTree(b, benchFiles)

	b.ResetTimer()
	for range b.N {
		if _, err := scan.Types(context.Background(), fs.System{},
			scan.TypesRequest{Selection: scan.Selection{Root: root}}); err != nil {
			b.Fatalf("Types: %v", err)
		}
	}
}

func BenchmarkScanLines(b *testing.B) {
	root := writeProjectTree(b, benchFiles)

	b.ResetTimer()
	for range b.N {
		if _, err := scan.Lines(context.Background(), fs.System{},
			scan.LinesRequest{Selection: scan.Selection{Root: root}}); err != nil {
			b.Fatalf("Lines: %v", err)
		}
	}
}

func BenchmarkCleanScan(b *testing.B) {
	root := writeProjectTree(b, benchFiles)

	b.ResetTimer()
	for range b.N {
		if _, err := clean.Scan(context.Background(), fs.System{},
			clean.ScanRequest{Root: root}); err != nil {
			b.Fatalf("Scan: %v", err)
		}
	}
}

// Credential scanning reads every file it does not skip, so this is the
// heaviest of the tree operations and the one whose target has the most room
// in it.
func BenchmarkSecretScan(b *testing.B) {
	root := writeProjectTree(b, benchFiles)

	b.ResetTimer()
	for range b.N {
		if _, err := secret.Scan(context.Background(), fs.System{},
			secret.ScanRequest{Root: root}); err != nil {
			b.Fatalf("Scan: %v", err)
		}
	}
}

// BenchmarkTreeReadBaseline is the floor the two reading benchmarks above are
// measured against: opening and reading every file in the same tree with the
// standard library and nothing else.
//
// It exists because a per-file cost of a few hundred microseconds looks like a
// defect in DevNest until this number shows the same cost without DevNest in
// the picture. On Windows with real-time virus scanning enabled, opening a file
// is most of what a tree-reading command spends its time on.
func BenchmarkTreeReadBaseline(b *testing.B) {
	root := writeProjectTree(b, benchFiles)

	b.ResetTimer()
	for range b.N {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(io.Discard, file)
			return errors.Join(err, file.Close())
		})
		if err != nil {
			b.Fatalf("walk: %v", err)
		}
	}
}
