package data

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/devnest/devnest/internal/errors"
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/parser"
)

// ConvertRequest asks for a document in another format.
type ConvertRequest struct {
	Request
	// Indent applies to JSON output only. YAML is written at two spaces,
	// which is the only indentation the format is ever written with.
	Indent int
}

// ConvertResult is the converted document.
type ConvertResult struct {
	Path      string `json:"path"`
	From      string `json:"from"`
	To        string `json:"to"`
	Documents int    `json:"documents"`
	Bytes     int    `json:"bytes"`
	Output    string `json:"output"`
}

// ToYAML converts JSON to YAML.
//
// Key order is preserved, which matters more here than anywhere else: a
// converted document is usually about to be committed, and a version of it
// with the keys rearranged is unreviewable.
func ToYAML(reader Reader, request ConvertRequest) (ConvertResult, error) {
	doc, err := load(reader, request.Request)
	if err != nil {
		return ConvertResult{}, err
	}
	if _, err := parseJSON(doc); err != nil {
		return ConvertResult{}, err
	}

	converted, err := yaml.JSONToYAML(doc.data)
	if err != nil {
		return ConvertResult{}, errors.Wrap(err, errors.CodeInternal,
			"%s parsed as JSON but could not be written as YAML", doc.path)
	}

	output := string(converted)
	return ConvertResult{
		Path:      doc.path,
		From:      FormatJSON,
		To:        FormatYAML,
		Documents: 1,
		Bytes:     len(output),
		Output:    output,
	}, nil
}

// ToJSON converts YAML to JSON.
//
// A multi-document source becomes a JSON array, because JSON has one top-level
// value and a stream of documents has to become something. Anchors and aliases
// are resolved on the way through, since JSON has no way to express them.
func ToJSON(reader Reader, request ConvertRequest) (ConvertResult, error) {
	doc, err := load(reader, request.Request)
	if err != nil {
		return ConvertResult{}, err
	}

	// Parsing first means a broken document fails with a line and a column
	// rather than with whatever the converter says about it.
	if _, err := parseYAML(doc); err != nil {
		return ConvertResult{}, err
	}

	indent, err := indentWidth(request.Indent)
	if err != nil {
		return ConvertResult{}, err
	}

	documents, err := jsonDocuments(doc)
	if err != nil {
		return ConvertResult{}, err
	}

	source := documents[0]
	if len(documents) > 1 {
		source = append(append([]byte("["), bytes.Join(documents, []byte(","))...), ']')
	}

	var formatted bytes.Buffer
	if err := json.Indent(&formatted, source, "", strings.Repeat(" ", indent)); err != nil {
		return ConvertResult{}, errors.Wrap(err, errors.CodeInternal,
			"%s converted to JSON that cannot be reprinted", doc.path)
	}

	output := strings.TrimSpace(formatted.String()) + "\n"
	return ConvertResult{
		Path:      doc.path,
		From:      FormatYAML,
		To:        FormatJSON,
		Documents: len(documents),
		Bytes:     len(output),
		Output:    output,
	}, nil
}

// jsonDocuments converts each YAML document separately.
//
// Going document by document through the library's own converter is what
// keeps key order: decoding into a Go map would sort the keys on the way out,
// and a converted file whose keys have been rearranged is a file nobody can
// review.
func jsonDocuments(doc *document) ([][]byte, error) {
	file, err := parser.ParseBytes(doc.data, 0)
	if err != nil {
		return nil, yamlError(doc, err)
	}

	documents := make([][]byte, 0, len(file.Docs))
	for _, document := range file.Docs {
		if isEmptyDocument(document) {
			continue
		}
		converted, err := yaml.YAMLToJSON([]byte(document.String()))
		if err != nil {
			return nil, yamlError(doc, err)
		}
		documents = append(documents, bytes.TrimSpace(converted))
	}

	if len(documents) == 0 {
		return nil, errors.New(errors.CodeParse, "%s holds no YAML documents", doc.path)
	}
	return documents, nil
}
