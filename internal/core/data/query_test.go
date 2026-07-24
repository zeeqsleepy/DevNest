package data

import (
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

// sample is a document with every shape a query has to walk through.
const sample = `{
  "service": {"name": "api", "port": 8080, "debug": false},
  "users": [
    {"name": "ana", "roles": ["admin", "dev"]},
    {"name": "budi", "roles": []}
  ],
  "limits": null,
  "odd.key": {"inner": 1},
  "big": 9007199254740993
}`

func query(t *testing.T, expression string) QueryResult {
	t.Helper()

	system := newFakeFS().with("app.json", sample)
	result, err := Query(system, QueryRequest{Request: file("app.json"), Expression: expression})
	if err != nil {
		t.Fatalf("Query(%q): %v", expression, err)
	}
	return result
}

func TestQuerySelectsValues(t *testing.T) {
	cases := []struct {
		expression string
		want       string
		kind       string
	}{
		{"service.name", `"api"`, KindString},
		{".service.port", `8080`, KindNumber},
		{"$service.debug", `false`, KindBoolean},
		{"users[1].name", `"budi"`, KindString},
		{"users[0].roles[1]", `"dev"`, KindString},
		{"limits", `null`, KindNull},
		{`["odd.key"].inner`, `1`, KindNumber},
		{`['odd.key']`, `{"inner":1}`, KindObject},
	}

	for _, testCase := range cases {
		t.Run(testCase.expression, func(t *testing.T) {
			result := query(t, testCase.expression)
			if got := string(result.Value); got != testCase.want {
				t.Errorf("value = %s, want %s", got, testCase.want)
			}
			if result.Kind != testCase.kind {
				t.Errorf("kind = %q, want %q", result.Kind, testCase.kind)
			}
		})
	}
}

// A number too large for a float64 has to come back with the digits it was
// written with. Losing the last digit of an identifier is the kind of silent
// corruption that takes a day to find.
func TestQueryKeepsLargeNumbersExact(t *testing.T) {
	if got := string(query(t, "big").Value); got != "9007199254740993" {
		t.Errorf("value = %s, want the digits unchanged", got)
	}
}

func TestQueryCountsWhatItSelected(t *testing.T) {
	result := query(t, "users")

	if result.Kind != KindArray || result.Entries != 2 {
		t.Errorf("result = %+v, want an array of two", result)
	}
}

func TestQueryReportsAMissingKeyWithTheOnesThatExist(t *testing.T) {
	system := newFakeFS().with("app.json", sample)

	_, err := Query(system, QueryRequest{
		Request:    file("app.json"),
		Expression: "service.prot",
	})
	assertCode(t, err, errors.CodeNotFound)

	text := message(t, err)
	if !strings.Contains(text, "port") {
		t.Errorf("message = %q, want the available keys listed", text)
	}
}

func TestQueryReportsTheWrongShape(t *testing.T) {
	system := newFakeFS().with("app.json", sample)

	cases := map[string]errors.Code{
		"service[0]":     errors.CodeInvalidInput,
		"users.name":     errors.CodeInvalidInput,
		"users[9]":       errors.CodeNotFound,
		"service.name.x": errors.CodeInvalidInput,
	}

	for expression, want := range cases {
		t.Run(expression, func(t *testing.T) {
			_, err := Query(system, QueryRequest{
				Request:    file("app.json"),
				Expression: expression,
			})
			assertCode(t, err, want)
		})
	}
}

func TestQueryRejectsExpressionsItDoesNotSpeak(t *testing.T) {
	system := newFakeFS().with("app.json", sample)

	for _, expression := range []string{"", ".", "users[]", "users[-1]", "users[a]", "users[0", "a..b"} {
		t.Run(expression, func(t *testing.T) {
			_, err := Query(system, QueryRequest{
				Request:    file("app.json"),
				Expression: expression,
			})
			assertCode(t, err, errors.CodeInvalidInput)
		})
	}
}

// The expression is checked before the file is read, so a typo in it fails the
// same way whether the path exists or not.
func TestQueryChecksTheExpressionFirst(t *testing.T) {
	_, err := Query(newFakeFS(), QueryRequest{
		Request:    file("nowhere.json"),
		Expression: "users[]",
	})
	assertCode(t, err, errors.CodeInvalidInput)
}
