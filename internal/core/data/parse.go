package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/devnest/devnest/internal/errors"
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// parseJSON decodes a document into plain Go values.
//
// Numbers are kept as json.Number rather than float64, so that an identifier
// with more precision than a float can hold survives a query or a conversion
// unchanged. Turning 9007199254740993 into 9007199254740992 in a tool people
// use to inspect data would be a bug worth an issue.
func parseJSON(doc *document) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(doc.data))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, jsonError(doc, err)
	}

	// A second value after the first is not a JSON document. Saying so is more
	// useful than decoding the first half and reporting success.
	if decoder.More() {
		offset := decoder.InputOffset()
		return nil, located(doc, offset,
			"there is more than one value in this document").
			WithHint("JSON holds a single top-level value; " +
				"a stream of them is JSON Lines, which devnest log reads")
	}

	return value, nil
}

// jsonError turns a decoder failure into a message that names the position.
func jsonError(doc *document, err error) error {
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return located(doc, syntax.Offset, "%s", syntax.Error())
	}

	var mismatch *json.UnmarshalTypeError
	if errors.As(err, &mismatch) {
		return located(doc, mismatch.Offset, "%s", mismatch.Error())
	}

	if err.Error() == "unexpected EOF" || strings.Contains(err.Error(), "unexpected end") {
		return located(doc, int64(len(doc.data)),
			"the document ends before the value is finished").
			WithHint("a bracket or a brace is probably unclosed")
	}

	return errors.Wrap(err, errors.CodeParse, "%s is not valid JSON: %s", doc.path, err.Error())
}

// located reports a parse failure at a byte offset, translated into the line
// and column a person can find in an editor, with the text around it quoted.
func located(doc *document, offset int64, format string, args ...any) *errors.Error {
	line, column, fragment := position(doc.data, offset)

	message := strings.TrimSpace(fmt.Sprintf(format, args...))
	message = strings.TrimPrefix(message, "invalid character ")

	return errors.New(errors.CodeParse,
		"%s is not valid JSON: line %d, column %d: %s", doc.path, line, column, message).
		WithHint("near: %s", fragment)
}

// position translates a byte offset into a line, a column, and the text of
// that line.
//
// The offset a JSON decoder reports is the byte after the problem, which is
// where a reader looking for the missing comma should start, and both numbers
// are one-based because that is what every editor shows.
func position(data []byte, offset int64) (line, column int, fragment string) {
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	if offset < 0 {
		offset = 0
	}

	line = 1 + bytes.Count(data[:offset], []byte("\n"))

	start := int64(bytes.LastIndexByte(data[:offset], '\n') + 1)
	column = int(offset - start)
	if column < 1 {
		column = 1
	}

	end := int64(bytes.IndexByte(data[start:], '\n'))
	if end < 0 {
		end = int64(len(data))
	} else {
		end += start
	}

	return line, column, clip(strings.TrimSpace(string(data[start:end])))
}

// clip trims a long line down to something that fits in a terminal, keeping
// the start of it, which is where the structure is.
//
// The text is not quoted. A JSON line is already full of quotation marks, and
// escaping them to fit inside another pair turns the one thing the user is
// supposed to read into a puzzle.
func clip(text string) string {
	const limit = 72
	if len([]rune(text)) > limit {
		return string([]rune(text)[:limit]) + "..."
	}
	return text
}

// yamlLocation matches the position goccy puts at the front of its errors.
var yamlLocation = regexp.MustCompile(`^\[(\d+):(\d+)\]\s*`)

// parseYAML decodes every document in a YAML source.
//
// Multi-document sources are normal in YAML and rejecting them would rule out
// most Kubernetes manifests, so each document is decoded and the caller
// decides what a set of them means.
func parseYAML(doc *document) ([]any, error) {
	file, err := parser.ParseBytes(doc.data, 0)
	if err != nil {
		return nil, yamlError(doc, err)
	}

	values := make([]any, 0, len(file.Docs))
	for _, document := range file.Docs {
		if isEmptyDocument(document) {
			continue
		}
		var value any
		if err := yaml.Unmarshal([]byte(document.String()), &value); err != nil {
			return nil, yamlError(doc, err)
		}
		values = append(values, value)
	}

	if len(values) == 0 {
		return nil, errors.New(errors.CodeParse, "%s holds no YAML documents", doc.path).
			WithHint("a file of comments and separators has nothing to validate")
	}

	return values, nil
}

// isEmptyDocument reports a document that is only comments or separators.
func isEmptyDocument(document *ast.DocumentNode) bool {
	if document == nil || document.Body == nil {
		return true
	}
	return strings.TrimSpace(document.Body.String()) == ""
}

// yamlError rewrites a parser failure into DevNest's error model.
//
// The library already reports the position and quotes the source, which is
// exactly what this module promises; the work here is separating the first
// line from the excerpt so that one becomes the message and the other becomes
// the hint, rather than a five-line blob in a field meant for a sentence.
func yamlError(doc *document, err error) error {
	text := strings.TrimSpace(err.Error())
	first, rest, _ := strings.Cut(text, "\n")

	location := ""
	if match := yamlLocation.FindStringSubmatch(first); match != nil {
		location = "line " + match[1] + ", column " + match[2] + ": "
		first = yamlLocation.ReplaceAllString(first, "")
	}

	failure := errors.Wrap(err, errors.CodeParse,
		"%s is not valid YAML: %s%s", doc.path, location, first)

	if excerpt := firstSourceLine(rest); excerpt != "" {
		return failure.WithHint("near: %s", clip(excerpt))
	}
	return failure
}

// firstSourceLine picks the offending line out of the library's excerpt, which
// marks it with a > and numbers every line it shows.
func firstSourceLine(excerpt string) string {
	for _, line := range strings.Split(excerpt, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, ">") {
			continue
		}
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
		if _, source, found := strings.Cut(trimmed, "|"); found {
			return strings.TrimSpace(source)
		}
	}
	return ""
}

// kindOf names the shape of a decoded value.
//
// The numeric cases are wider than JSON needs because the YAML decoder hands
// back Go's own integer types, and a number has to be reported as a number
// whichever format it arrived in.
func kindOf(value any) string {
	switch value.(type) {
	case nil:
		return KindNull
	case map[string]any:
		return KindObject
	case []any:
		return KindArray
	case string:
		return KindString
	case bool:
		return KindBoolean
	case json.Number, int, int64, uint64, float64, float32:
		return KindNumber
	default:
		return KindString
	}
}

// entriesIn counts what a container holds: keys for an object, elements for an
// array, nothing for a scalar.
func entriesIn(value any) int {
	switch typed := value.(type) {
	case map[string]any:
		return len(typed)
	case []any:
		return len(typed)
	default:
		return 0
	}
}
