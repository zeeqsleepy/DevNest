package data

import (
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func TestToCSVConvertsAnArrayOfObjects(t *testing.T) {
	system := newFakeFS().with("users.json",
		`[{"name":"ana","age":31},{"name":"budi","age":24}]`)

	result, err := ToCSV(system, CSVRequest{Request: file("users.json")})
	if err != nil {
		t.Fatalf("ToCSV: %v", err)
	}

	if strings.Join(result.Columns, ",") != "age,name" {
		t.Errorf("columns = %v, want them sorted and complete", result.Columns)
	}
	if result.Records != 2 || len(result.Rows) != 2 {
		t.Errorf("records = %d, rows = %d, want 2 and 2", result.Records, len(result.Rows))
	}
	if strings.Join(result.Rows[0], "|") != "31|ana" {
		t.Errorf("row = %v, want the cells under their own columns", result.Rows[0])
	}
}

func TestToCSVUnionsColumnsAcrossRecords(t *testing.T) {
	system := newFakeFS().with("mixed.json",
		`[{"a":1},{"b":2},{"a":3,"b":4}]`)

	result, err := ToCSV(system, CSVRequest{Request: file("mixed.json")})
	if err != nil {
		t.Fatalf("ToCSV: %v", err)
	}

	if len(result.Columns) != 2 {
		t.Fatalf("columns = %v, want both keys", result.Columns)
	}
	// A record missing a key gets an empty cell, never a shifted row.
	if strings.Join(result.Rows[0], "|") != "1|" {
		t.Errorf("row = %v, want an empty cell for the missing key", result.Rows[0])
	}
	if strings.Join(result.Rows[1], "|") != "|2" {
		t.Errorf("row = %v, want the value under its own column", result.Rows[1])
	}
}

func TestToCSVTakesASingleObjectAsOneRow(t *testing.T) {
	system := newFakeFS().with("one.json", `{"name":"devnest","stars":0}`)

	result, err := ToCSV(system, CSVRequest{Request: file("one.json")})
	if err != nil {
		t.Fatalf("ToCSV: %v", err)
	}
	if result.Records != 1 || len(result.Rows) != 1 {
		t.Errorf("result = %+v, want one row", result)
	}
}

func TestToCSVTakesAnArrayOfScalarsAsOneColumn(t *testing.T) {
	system := newFakeFS().with("ids.json", `["a","b","c"]`)

	result, err := ToCSV(system, CSVRequest{Request: file("ids.json")})
	if err != nil {
		t.Fatalf("ToCSV: %v", err)
	}

	if len(result.Columns) != 1 || result.Columns[0] != "value" {
		t.Errorf("columns = %v, want one called value", result.Columns)
	}
	if len(result.Rows) != 3 {
		t.Errorf("rows = %d, want 3", len(result.Rows))
	}
}

func TestToCSVRefusesNestedValuesUntilAskedToFlatten(t *testing.T) {
	system := newFakeFS().with("nested.json",
		`[{"name":"ana","address":{"city":"Ipoh"},"tags":["x","y"]}]`)

	_, err := ToCSV(system, CSVRequest{Request: file("nested.json")})
	assertCode(t, err, errors.CodeInvalidInput)

	if text := message(t, err); !strings.Contains(text, "--flatten") {
		t.Errorf("message = %q, want it to name the way out", text)
	}

	flattened, err := ToCSV(system, CSVRequest{Request: file("nested.json"), Flatten: true})
	if err != nil {
		t.Fatalf("ToCSV --flatten: %v", err)
	}

	want := "address.city,name,tags.0,tags.1"
	if got := strings.Join(flattened.Columns, ","); got != want {
		t.Errorf("columns = %s, want %s", got, want)
	}
	if !flattened.Flattened {
		t.Error("the result does not record that it was flattened")
	}
}

func TestToCSVKeepsNumbersExact(t *testing.T) {
	system := newFakeFS().with("ids.json", `[{"id":9007199254740993}]`)

	result, err := ToCSV(system, CSVRequest{Request: file("ids.json")})
	if err != nil {
		t.Fatalf("ToCSV: %v", err)
	}
	if result.Rows[0][0] != "9007199254740993" {
		t.Errorf("cell = %q, want the digits unchanged", result.Rows[0][0])
	}
}

func TestToCSVRefusesWhatIsNotATable(t *testing.T) {
	system := newFakeFS().
		with("scalar.json", `"just a string"`).
		with("empty.json", `[]`).
		with("matrix.json", `[[1,2],[3,4]]`)

	for _, name := range []string{"scalar.json", "empty.json", "matrix.json"} {
		t.Run(name, func(t *testing.T) {
			_, err := ToCSV(system, CSVRequest{Request: file(name)})
			assertCode(t, err, errors.CodeInvalidInput)
		})
	}
}

func TestToCSVWritesNullAsAnEmptyCell(t *testing.T) {
	system := newFakeFS().with("nulls.json", `[{"a":null,"b":false}]`)

	result, err := ToCSV(system, CSVRequest{Request: file("nulls.json")})
	if err != nil {
		t.Fatalf("ToCSV: %v", err)
	}
	if strings.Join(result.Rows[0], "|") != "|false" {
		t.Errorf("row = %v, want null as an empty cell", result.Rows[0])
	}
}
