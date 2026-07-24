package data

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/devnest/devnest/internal/errors"
)

// CSVRequest asks for JSON as rows.
type CSVRequest struct {
	Request
	// Flatten turns nested values into columns named with a dot, so that
	// {"a":{"b":1}} becomes a column called a.b. Without it, a nested value
	// is an error rather than a guess.
	Flatten bool
}

// CSVResult is the tabular form of a document.
//
// Columns and Rows are returned rather than a block of CSV text: the interface
// layer already owns a CSV writer and a table renderer, and a module that
// formatted its own rows would be the second implementation of quoting rules
// that nobody remembers to keep in step.
type CSVResult struct {
	Path      string     `json:"path"`
	From      string     `json:"from"`
	Columns   []string   `json:"columns"`
	Rows      [][]string `json:"rows"`
	Records   int        `json:"records"`
	Flattened bool       `json:"flattened"`
}

// ToCSV converts JSON to rows and columns.
//
// What converts cleanly is an array of objects, which is what an API returns
// and what a spreadsheet expects. A single object becomes one row, and an
// array of scalars becomes one column. Anything else is reported rather than
// forced: a nested object stringified into a cell is data that looks converted
// and is not, and nobody notices until the spreadsheet is already in a report.
func ToCSV(reader Reader, request CSVRequest) (CSVResult, error) {
	doc, err := load(reader, request.Request)
	if err != nil {
		return CSVResult{}, err
	}

	value, err := parseJSON(doc)
	if err != nil {
		return CSVResult{}, err
	}

	records, err := recordsOf(value)
	if err != nil {
		return CSVResult{}, err
	}

	result := CSVResult{
		Path:      doc.path,
		From:      FormatJSON,
		Records:   len(records),
		Flattened: request.Flatten,
	}

	// Columns are collected in the order the keys are first seen rather than
	// sorted, so the output reads in the order the document was written.
	seen := make(map[string]int, 8)
	cells := make([]map[string]string, 0, len(records))

	for index, record := range records {
		flat, err := flatten(record, request.Flatten, index)
		if err != nil {
			return CSVResult{}, err
		}
		for _, column := range flat.order {
			if _, known := seen[column]; !known {
				seen[column] = len(result.Columns)
				result.Columns = append(result.Columns, column)
			}
		}
		cells = append(cells, flat.values)
	}

	result.Rows = make([][]string, 0, len(cells))
	for _, values := range cells {
		row := make([]string, len(result.Columns))
		for column, position := range seen {
			row[position] = values[column]
		}
		result.Rows = append(result.Rows, row)
	}

	return result, nil
}

// recordsOf turns a document into the list of records a table is made of.
func recordsOf(value any) ([]any, error) {
	switch typed := value.(type) {
	case []any:
		if len(typed) == 0 {
			return nil, errors.New(errors.CodeInvalidInput,
				"the document is an empty array, so there is nothing to convert")
		}
		return typed, nil

	case map[string]any:
		return []any{typed}, nil

	default:
		return nil, errors.New(errors.CodeInvalidInput,
			"the document is %s; CSV needs an array of objects", kindOf(value)).
			WithHint("select the part that is a list first, " +
				"for example: devnest json query data.json items")
	}
}

// flat is one record turned into cells, with the order its keys appeared in.
type flat struct {
	order  []string
	values map[string]string
}

// flatten turns one record into cells.
func flatten(record any, allowNesting bool, index int) (flat, error) {
	result := flat{values: make(map[string]string, 8)}

	object, ok := record.(map[string]any)
	if !ok {
		if kind := kindOf(record); kind == KindArray {
			return flat{}, errors.New(errors.CodeInvalidInput,
				"record %d is an array of its own; CSV needs a flat list", index+1).
				WithHint("an array of arrays has no column names; " +
					"select one level down with \"devnest json query\"")
		}
		// An array of scalars is a single column, which is the honest reading
		// of ["a","b","c"] and is genuinely useful for a list of identifiers.
		result.order = []string{"value"}
		result.values["value"] = cell(record)
		return result, nil
	}

	// Keys of a decoded object come out of a Go map, whose order is random, so
	// the columns are sorted per record and the first record's order is what
	// the table ends up in. Deterministic beats faithful here: the alternative
	// is output that changes between two runs over the same file.
	for _, key := range sortedKeys(object) {
		if err := add(&result, key, object[key], allowNesting, index); err != nil {
			return flat{}, err
		}
	}

	return result, nil
}

// add writes one value into a record, descending into it when flattening.
func add(result *flat, name string, value any, allowNesting bool, index int) error {
	switch typed := value.(type) {
	case map[string]any:
		if !allowNesting {
			return nestedValue(name, index)
		}
		for _, key := range sortedKeys(typed) {
			if err := add(result, name+"."+key, typed[key], allowNesting, index); err != nil {
				return err
			}
		}

	case []any:
		if !allowNesting {
			return nestedValue(name, index)
		}
		for position, element := range typed {
			child := name + "." + strconv.Itoa(position)
			if err := add(result, child, element, allowNesting, index); err != nil {
				return err
			}
		}

	default:
		result.order = append(result.order, name)
		result.values[name] = cell(value)
	}

	return nil
}

func nestedValue(name string, index int) error {
	return errors.New(errors.CodeInvalidInput,
		"record %d has a nested value at %q; CSV has no way to hold it", index+1, name).
		WithHint("pass --flatten to spread it across columns named with a dot, " +
			"or select the flat part with \"devnest json query\"")
}

// cell renders one value as text.
//
// Numbers keep the digits they were written with, because a column of
// identifiers turned into 1.234568e+06 is a column of ruined data.
func cell(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case json.Number:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

func sortedKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
