package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/core/data"
	"github.com/devnest/devnest/internal/errors"
)

func TestValidateTextReportsTheShape(t *testing.T) {
	result := data.ValidateResult{
		Path:      "/work/config.json",
		Format:    data.FormatJSON,
		Valid:     true,
		Bytes:     2048,
		Lines:     64,
		Documents: 1,
		Kind:      data.KindObject,
		Entries:   12,
	}

	got := render(t, validateText(result))
	for _, want := range []string{"config.json", "json", "yes", "2.0 KB", "object (12 keys)"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
	// One document is the normal case and saying so is noise.
	if strings.Contains(got, "documents") {
		t.Errorf("output = %q, want the document count left out for a single document", got)
	}
}

func TestValidateTextCountsSeveralDocuments(t *testing.T) {
	result := data.ValidateResult{
		Path:      "/work/manifest.yaml",
		Format:    data.FormatYAML,
		Valid:     true,
		Documents: 3,
		Kind:      data.KindArray,
		Entries:   2,
	}

	got := render(t, validateText(result))
	if !strings.Contains(got, "documents") || !strings.Contains(got, "3") {
		t.Errorf("output = %q, want the document count", got)
	}
	if !strings.Contains(got, "array (2 entries)") {
		t.Errorf("output = %q, want the entry count", got)
	}
}

// Formatting and conversion print the document and nothing else, because the
// output is meant to be redirected and a heading would end up in the file.
func TestWriteDocumentAddsNothing(t *testing.T) {
	const document = "{\n  \"a\": 1\n}\n"

	if got := render(t, writeDocument(document)); got != document {
		t.Errorf("output = %q, want exactly the document", got)
	}
}

func TestQueryTextIndentsTheValue(t *testing.T) {
	result := data.QueryResult{
		Kind:  data.KindObject,
		Value: json.RawMessage(`{"b":2,"a":1}`),
	}

	got := render(t, queryText(result, false))
	if !strings.Contains(got, "{\n  \"b\": 2,\n  \"a\": 1\n}") {
		t.Errorf("output = %q, want indented JSON", got)
	}
}

func TestQueryTextRawStripsTheQuotesFromAString(t *testing.T) {
	result := data.QueryResult{Kind: data.KindString, Value: json.RawMessage(`"ana@example.com"`)}

	if got := render(t, queryText(result, true)); got != "ana@example.com\n" {
		t.Errorf("output = %q, want the bare string", got)
	}

	// --raw on anything that is not a string prints the value as it is; there
	// is no unquoted form of an object.
	object := data.QueryResult{Kind: data.KindObject, Value: json.RawMessage(`{"a":1}`)}
	if got := render(t, queryText(object, true)); !strings.Contains(got, `"a": 1`) {
		t.Errorf("output = %q, want the object printed as JSON", got)
	}
}

func TestCSVTextWritesRealCSV(t *testing.T) {
	result := data.CSVResult{
		Columns: []string{"name", "note"},
		Rows:    [][]string{{"ana", "has, a comma"}, {"budi", ""}},
	}

	got := render(t, csvText(result))
	want := "name,note\nana,\"has, a comma\"\nbudi,\n"
	if strings.ReplaceAll(got, "\r\n", "\n") != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestCSVTableMatchesTheColumns(t *testing.T) {
	result := data.CSVResult{
		Columns: []string{"a", "b"},
		Rows:    [][]string{{"1", "2"}},
	}

	table := csvTable(result)()
	if len(table.Columns) != 2 || table.Columns[1].Title != "b" {
		t.Errorf("columns = %+v, want the same two", table.Columns)
	}
	if len(table.Rows) != 1 {
		t.Errorf("rows = %d, want 1", len(table.Rows))
	}
}

func TestDataRequestResolvesWhereTheDocumentComesFrom(t *testing.T) {
	env := &Env{Stdin: strings.NewReader("{}")}

	request, err := dataRequest(env, []string{"config.json"}, false)
	if err != nil {
		t.Fatalf("dataRequest: %v", err)
	}
	if request.Path != "config.json" || request.Input != nil {
		t.Errorf("request = %+v, want the path alone", request)
	}

	piped, err := dataRequest(env, nil, true)
	if err != nil {
		t.Fatalf("dataRequest: %v", err)
	}
	if piped.Input == nil || piped.Path != "" {
		t.Errorf("request = %+v, want the stream alone", piped)
	}
}

func TestDataRequestRejectsAmbiguousInput(t *testing.T) {
	env := &Env{Stdin: strings.NewReader("{}")}

	cases := []struct {
		name     string
		args     []string
		useStdin bool
	}{
		{"both", []string{"config.json"}, true},
		{"neither", nil, false},
		{"two files", []string{"a.json", "b.json"}, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := dataRequest(env, testCase.args, testCase.useStdin)
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := errors.CodeOf(err); got != errors.CodeInvalidInput {
				t.Errorf("code = %q, want %q", got, errors.CodeInvalidInput)
			}
		})
	}
}

func TestQueryArgumentsSplitsTheFileFromTheExpression(t *testing.T) {
	path, expression, err := queryArguments([]string{"api.json", "users[0]"}, false)
	if err != nil {
		t.Fatalf("queryArguments: %v", err)
	}
	if len(path) != 1 || path[0] != "api.json" || expression != "users[0]" {
		t.Errorf("path = %v, expression = %q", path, expression)
	}

	path, expression, err = queryArguments([]string{"users[0]"}, true)
	if err != nil {
		t.Fatalf("queryArguments: %v", err)
	}
	if len(path) != 0 || expression != "users[0]" {
		t.Errorf("path = %v, expression = %q, want the expression alone", path, expression)
	}

	if _, _, err := queryArguments([]string{"api.json"}, false); err == nil {
		t.Error("a missing expression was accepted")
	}
	if _, _, err := queryArguments([]string{"api.json", "users[0]"}, true); err == nil {
		t.Error("a file was accepted alongside --stdin")
	}
}
