package benchmarks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/core/data"
	"github.com/devnest/devnest/internal/core/file"
	"github.com/devnest/devnest/internal/platform/fs"
)

// benchDocumentBytes is the size of the generated JSON document. Ten megabytes
// is the size the target in docs/performance.md is written against, and it is
// well inside the module's 64 MiB limit.
const benchDocumentBytes = 10 << 20

// benchFileBytes is the size of the file the digest benchmarks read. The target
// is written for a gigabyte; a sixteenth of that measures the same throughput
// without asking a developer's disk for a gigabyte of temporary space, and the
// reported MB/s is what carries over.
const benchFileBytes = 64 << 20

// writeJSONDocument generates a document shaped like an API response: an array
// of objects with a few nested levels, rather than one enormous string.
func writeJSONDocument(b *testing.B, size int) string {
	b.Helper()

	var document strings.Builder
	document.Grow(size + 1024)
	document.WriteString(`{"generated":"2026-07-24T09:15:00Z","items":[`)

	for index := 0; document.Len() < size; index++ {
		if index > 0 {
			document.WriteByte(',')
		}
		fmt.Fprintf(&document,
			`{"id":%d,"name":"item-%d","tags":["alpha","beta"],`+
				`"metrics":{"views":%d,"score":%.3f},"active":%t}`,
			index, index, index*7, float64(index%1000)/3, index%2 == 0)
	}
	document.WriteString("]}")

	path := filepath.Join(b.TempDir(), "document.json")
	if err := os.WriteFile(path, []byte(document.String()), 0o600); err != nil {
		b.Fatalf("write: %v", err)
	}
	return path
}

// writeLargeFile generates a file of arbitrary bytes for the digest
// benchmarks. The contents do not matter; the size does.
func writeLargeFile(b *testing.B, size int) string {
	b.Helper()

	path := filepath.Join(b.TempDir(), "payload.bin")
	file, err := os.Create(path)
	if err != nil {
		b.Fatalf("create: %v", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			b.Fatalf("close: %v", err)
		}
	}()

	block := make([]byte, 1<<20)
	for index := range block {
		block[index] = byte(index)
	}
	for written := 0; written < size; written += len(block) {
		if _, err := file.Write(block); err != nil {
			b.Fatalf("write: %v", err)
		}
	}
	return path
}

func BenchmarkJSONFormat(b *testing.B) {
	path := writeJSONDocument(b, benchDocumentBytes)
	report(b, path)

	b.ResetTimer()
	for range b.N {
		if _, err := data.Format(fs.System{}, data.FormatRequest{
			Request: data.Request{Path: path},
		}); err != nil {
			b.Fatalf("Format: %v", err)
		}
	}
}

func BenchmarkJSONMinify(b *testing.B) {
	path := writeJSONDocument(b, benchDocumentBytes)
	report(b, path)

	b.ResetTimer()
	for range b.N {
		if _, err := data.Minify(fs.System{}, data.MinifyRequest{
			Request: data.Request{Path: path},
		}); err != nil {
			b.Fatalf("Minify: %v", err)
		}
	}
}

// A query decodes the document and re-encodes only what it selected, which is
// why it costs more than reprinting the bytes.
func BenchmarkJSONQuery(b *testing.B) {
	path := writeJSONDocument(b, benchDocumentBytes)
	report(b, path)

	b.ResetTimer()
	for range b.N {
		if _, err := data.Query(fs.System{}, data.QueryRequest{
			Request:    data.Request{Path: path},
			Expression: "items[100].metrics.score",
		}); err != nil {
			b.Fatalf("Query: %v", err)
		}
	}
}

func BenchmarkHashSHA256(b *testing.B) {
	path := writeLargeFile(b, benchFileBytes)
	report(b, path)

	b.ResetTimer()
	for range b.N {
		if _, err := file.Hash(context.Background(), fs.System{}, file.HashRequest{
			Paths:      []string{path},
			Algorithms: []fs.Algorithm{fs.SHA256},
		}); err != nil {
			b.Fatalf("Hash: %v", err)
		}
	}
}

// Three digests from one pass over the file is the claim the shared digest
// helper exists to make. This benchmark is what keeps it honest: it should cost
// well under three times the single-algorithm run.
func BenchmarkHashThreeAlgorithms(b *testing.B) {
	path := writeLargeFile(b, benchFileBytes)
	report(b, path)

	b.ResetTimer()
	for range b.N {
		if _, err := file.Hash(context.Background(), fs.System{}, file.HashRequest{
			Paths:      []string{path},
			Algorithms: []fs.Algorithm{fs.SHA256, fs.SHA512, fs.MD5},
		}); err != nil {
			b.Fatalf("Hash: %v", err)
		}
	}
}
