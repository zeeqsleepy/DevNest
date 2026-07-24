// Package data is DevNest's structured data module: validating, formatting,
// minifying, querying, and converting JSON and YAML.
//
// # Everything is held in memory
//
// This module reads a whole document before it does anything with it, which is
// the opposite of how the log module works and is not an oversight. A JSON
// document is a tree: pretty-printing the end of it depends on the start, a
// query can select the last key, and a converter has to know the shape before
// it can write a row. There is no streaming answer to those questions that is
// worth the complexity here.
//
// The consequence is a size limit, which is enforced and reported rather than
// discovered as an out-of-memory kill. Anything past it belongs in a streaming
// tool, and the error says so.
//
// # Parse failures are the product
//
// "Invalid JSON" on its own is useless: the user already knows it is invalid,
// which is why they ran the command. Every parse failure here reports the line
// and the column and quotes the text around it, because finding the missing
// comma is the entire job.
//
// # Nothing is written back
//
// Every operation prints to the caller. No command in this module edits the
// file it read; a formatter that rewrites in place is one interrupted run away
// from an empty configuration file, and a shell redirect does the same job
// where the user can see it.
package data

import (
	"io"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// Formats this module reads and writes.
const (
	FormatJSON = "json"
	FormatYAML = "yaml"
	FormatCSV  = "csv"
)

// Kinds a value can be, named the way the formats name them rather than the
// way Go does.
const (
	KindObject  = "object"
	KindArray   = "array"
	KindString  = "string"
	KindNumber  = "number"
	KindBoolean = "boolean"
	KindNull    = "null"
)

// maxInput is the largest document this module will read.
//
// The number is a judgement rather than a law: large enough for any
// configuration file, any API response worth reading, and any fixture, and
// small enough that a mistyped path pointed at a disk image fails with a
// sentence instead of taking the machine down with it.
const maxInput = 64 << 20 // 64 MiB

// stdinName is what a document read from a pipe is called in results and
// error messages. It has no path, and inventing one would be a lie a script
// might act on.
const stdinName = "<stdin>"

// Request names the input every operation reads: a file, or whatever the
// caller has already opened. Exactly one of the two is used, Path first.
type Request struct {
	Path  string
	Input io.Reader
}

// document is a loaded input, ready to parse.
type document struct {
	path  string
	data  []byte
	bytes int
}

// load reads the whole input named by a request.
func load(reader Reader, request Request) (*document, error) {
	if strings.TrimSpace(request.Path) == "" {
		return loadStream(request.Input)
	}

	resolved, err := reader.Resolve(request.Path)
	if err != nil {
		return nil, err
	}

	entry, err := reader.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if entry.IsDir {
		return nil, errors.New(errors.CodeInvalidInput, "%s is a directory", resolved).
			WithHint("pass a single file, or pipe the document in and read it from stdin")
	}
	if entry.Bytes > maxInput {
		return nil, tooLarge(resolved, entry.Bytes)
	}

	handle, err := reader.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer func() { _ = handle.Close() }()

	data, err := readAll(handle, resolved)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New(errors.CodeInvalidInput, "%s is empty", resolved)
	}

	return &document{path: resolved, data: data, bytes: len(data)}, nil
}

func loadStream(input io.Reader) (*document, error) {
	if input == nil {
		return nil, errors.New(errors.CodeInvalidInput, "no file was given").
			WithHint("pass the path of the document, or pipe it in and use --stdin")
	}

	data, err := readAll(input, stdinName)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New(errors.CodeInvalidInput, "standard input was empty").
			WithHint("pipe the document in, for example: cat config.json | devnest json --stdin")
	}

	return &document{path: stdinName, data: data, bytes: len(data)}, nil
}

// readAll reads up to the size limit, and reports having hit it rather than
// silently returning a truncated document that would then fail to parse for a
// reason the user cannot act on.
func readAll(from io.Reader, name string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(from, maxInput+1))
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeIO, "cannot read %s", name)
	}
	if len(data) > maxInput {
		return nil, tooLarge(name, int64(len(data)))
	}
	return data, nil
}

func tooLarge(path string, size int64) error {
	return errors.New(errors.CodeUnsupported,
		"%s is %d bytes; this command reads a document into memory and stops at %d",
		path, size, maxInput).
		WithHint("a document this large wants a streaming tool; " +
			"devnest log handles files too big to hold")
}

// lines counts the lines in a document, for the size reported by validate.
func lines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	count := strings.Count(string(data), "\n")
	if !strings.HasSuffix(string(data), "\n") {
		count++
	}
	return count
}
