package data

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// defaultIndent is what every formatter uses when the caller does not say.
// Two spaces is what the ecosystem writes and what a diff of a reformatted
// file should not be full of.
const defaultIndent = 2

// maxIndent is where an indent width stops being a preference and starts being
// a typo.
const maxIndent = 10

// ValidateRequest asks whether a document parses.
type ValidateRequest struct {
	Request
	// Format is json or yaml. The command the user ran decides it; nothing
	// here guesses from the file extension, because a .txt full of JSON is
	// still JSON and a .json full of YAML is a mistake worth reporting.
	Format string
}

// ValidateResult describes a document that parsed.
//
// There is no Valid field set to false anywhere: a document that does not
// parse comes back as an error carrying the line and column, because that is
// the answer the user wants and because it is what makes the exit code useful
// in a pre-commit hook.
type ValidateResult struct {
	Path      string `json:"path"`
	Format    string `json:"format"`
	Valid     bool   `json:"valid"`
	Bytes     int    `json:"bytes"`
	Lines     int    `json:"lines"`
	Documents int    `json:"documents"`
	Kind      string `json:"kind"`
	Entries   int    `json:"entries"`
}

// Validate parses a document and reports what it is.
func Validate(reader Reader, request ValidateRequest) (ValidateResult, error) {
	doc, err := load(reader, request.Request)
	if err != nil {
		return ValidateResult{}, err
	}

	result := ValidateResult{
		Path:      doc.path,
		Format:    request.Format,
		Valid:     true,
		Bytes:     doc.bytes,
		Lines:     lines(doc.data),
		Documents: 1,
	}

	switch request.Format {
	case FormatYAML:
		values, err := parseYAML(doc)
		if err != nil {
			return ValidateResult{}, err
		}
		result.Documents = len(values)
		result.Kind = kindOf(values[0])
		result.Entries = entriesIn(values[0])

	default:
		value, err := parseJSON(doc)
		if err != nil {
			return ValidateResult{}, err
		}
		result.Format = FormatJSON
		result.Kind = kindOf(value)
		result.Entries = entriesIn(value)
	}

	return result, nil
}

// FormatRequest asks for a document reprinted.
type FormatRequest struct {
	Request
	// Indent is the number of spaces per level. Zero means the default.
	Indent int
}

// FormatResult is the reprinted document.
type FormatResult struct {
	Path   string `json:"path"`
	Format string `json:"format"`
	Indent int    `json:"indent"`
	Bytes  int    `json:"bytes"`
	Output string `json:"output"`
}

// Format reprints JSON with consistent indentation.
//
// The document is reprinted from its own bytes rather than decoded and
// re-encoded, so key order survives, numbers keep the precision they were
// written with, and the only thing that changes is the whitespace. A formatter
// that reorders keys produces a diff nobody can review.
func Format(reader Reader, request FormatRequest) (FormatResult, error) {
	doc, err := load(reader, request.Request)
	if err != nil {
		return FormatResult{}, err
	}
	if _, err := parseJSON(doc); err != nil {
		return FormatResult{}, err
	}

	indent, err := indentWidth(request.Indent)
	if err != nil {
		return FormatResult{}, err
	}

	var formatted bytes.Buffer
	if err := json.Indent(&formatted, doc.data, "", strings.Repeat(" ", indent)); err != nil {
		return FormatResult{}, jsonError(doc, err)
	}

	output := strings.TrimSpace(formatted.String()) + "\n"
	return FormatResult{
		Path:   doc.path,
		Format: FormatJSON,
		Indent: indent,
		Bytes:  len(output),
		Output: output,
	}, nil
}

// MinifyRequest asks for a document with its whitespace removed.
type MinifyRequest struct {
	Request
}

// MinifyResult is the compacted document, with what it saved.
type MinifyResult struct {
	Path         string  `json:"path"`
	Format       string  `json:"format"`
	Bytes        int     `json:"bytes"`
	WasBytes     int     `json:"wasBytes"`
	SavedBytes   int     `json:"savedBytes"`
	SavedPercent float64 `json:"savedPercent"`
	Output       string  `json:"output"`
}

// Minify strips the whitespace from JSON.
func Minify(reader Reader, request MinifyRequest) (MinifyResult, error) {
	doc, err := load(reader, request.Request)
	if err != nil {
		return MinifyResult{}, err
	}
	if _, err := parseJSON(doc); err != nil {
		return MinifyResult{}, err
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, doc.data); err != nil {
		return MinifyResult{}, jsonError(doc, err)
	}

	output := compact.String()
	saved := doc.bytes - len(output)

	return MinifyResult{
		Path:         doc.path,
		Format:       FormatJSON,
		Bytes:        len(output),
		WasBytes:     doc.bytes,
		SavedBytes:   saved,
		SavedPercent: percent(saved, doc.bytes),
		Output:       output,
	}, nil
}

func indentWidth(requested int) (int, error) {
	switch {
	case requested == 0:
		return defaultIndent, nil
	case requested < 0 || requested > maxIndent:
		return 0, errors.New(errors.CodeInvalidInput,
			"an indent of %d is out of range", requested).
			WithHint("choose between 1 and %d spaces", maxIndent)
	default:
		return requested, nil
	}
}

// percent keeps one decimal place, so two runs over one document produce
// byte-identical output.
func percent(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	value := float64(part) / float64(whole) * 100
	return float64(int(value*10+0.5)) / 10
}
