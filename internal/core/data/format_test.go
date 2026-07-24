package data

import (
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func TestValidateReportsWhatADocumentIs(t *testing.T) {
	system := newFakeFS().with("config.json", `{"name":"devnest","ports":[80,443]}`)

	result, err := Validate(system, ValidateRequest{Request: file("config.json"), Format: FormatJSON})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if !result.Valid || result.Kind != KindObject || result.Entries != 2 {
		t.Errorf("result = %+v, want a valid object with two keys", result)
	}
	if result.Documents != 1 || result.Format != FormatJSON {
		t.Errorf("result = %+v, want one JSON document", result)
	}
}

func TestValidateCountsYAMLDocuments(t *testing.T) {
	system := newFakeFS().with("manifest.yaml", "kind: Service\n---\nkind: Deployment\n")

	result, err := Validate(system, ValidateRequest{
		Request: file("manifest.yaml"),
		Format:  FormatYAML,
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if result.Documents != 2 {
		t.Errorf("documents = %d, want 2", result.Documents)
	}
	if result.Kind != KindObject {
		t.Errorf("kind = %q, want %q", result.Kind, KindObject)
	}
}

func TestValidateReportsTheLineAndColumnOfABrokenDocument(t *testing.T) {
	system := newFakeFS().with("broken.json", "{\n  \"a\": 1,\n  \"b\": ,\n}\n")

	_, err := Validate(system, ValidateRequest{Request: file("broken.json"), Format: FormatJSON})
	assertCode(t, err, errors.CodeParse)

	text := message(t, err)
	if !strings.Contains(text, "line 3") {
		t.Errorf("message = %q, want it to name line 3", text)
	}
	if !strings.Contains(text, `near: "b": ,`) {
		t.Errorf("message = %q, want the offending line quoted", text)
	}
}

func TestValidateReportsTheLineOfABrokenYAMLDocument(t *testing.T) {
	system := newFakeFS().with("broken.yaml", "name: one\n  bad: [1, 2\n")

	_, err := Validate(system, ValidateRequest{Request: file("broken.yaml"), Format: FormatYAML})
	assertCode(t, err, errors.CodeParse)

	text := message(t, err)
	if !strings.Contains(text, "line 1") && !strings.Contains(text, "line 2") {
		t.Errorf("message = %q, want it to name a line", text)
	}
}

func TestValidateRejectsAStreamOfValues(t *testing.T) {
	system := newFakeFS().with("stream.json", "{\"a\":1}\n{\"a\":2}\n")

	_, err := Validate(system, ValidateRequest{Request: file("stream.json"), Format: FormatJSON})
	assertCode(t, err, errors.CodeParse)

	if text := message(t, err); !strings.Contains(text, "more than one value") {
		t.Errorf("message = %q, want it to explain that JSON holds one value", text)
	}
}

func TestValidateReadsStandardInput(t *testing.T) {
	result, err := Validate(newFakeFS(), ValidateRequest{
		Request: piped(`[1,2,3]`),
		Format:  FormatJSON,
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if result.Path != stdinName {
		t.Errorf("path = %q, want %q", result.Path, stdinName)
	}
	if result.Kind != KindArray || result.Entries != 3 {
		t.Errorf("result = %+v, want an array of three", result)
	}
}

func TestLoadRefusesWhatItCannotRead(t *testing.T) {
	system := newFakeFS().
		with("empty.json", "").
		with("huge.json", "{}").
		withSize("huge.json", maxInput+1)

	cases := []struct {
		name    string
		request Request
		want    errors.Code
	}{
		{"missing", file("nowhere.json"), errors.CodeNotFound},
		{"directory", Request{Path: path()}, errors.CodeInvalidInput},
		{"empty file", file("empty.json"), errors.CodeInvalidInput},
		{"too large", file("huge.json"), errors.CodeUnsupported},
		{"nothing at all", Request{}, errors.CodeInvalidInput},
		{"empty pipe", piped(""), errors.CodeInvalidInput},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Validate(system, ValidateRequest{
				Request: testCase.request,
				Format:  FormatJSON,
			})
			assertCode(t, err, testCase.want)
		})
	}
}

func TestFormatKeepsKeyOrderAndAppliesTheIndent(t *testing.T) {
	system := newFakeFS().with("config.json", `{"zeta":1,"alpha":{"b":2}}`)

	result, err := Format(system, FormatRequest{Request: file("config.json"), Indent: 4})
	if err != nil {
		t.Fatalf("Format: %v", err)
	}

	want := "{\n    \"zeta\": 1,\n    \"alpha\": {\n        \"b\": 2\n    }\n}\n"
	if result.Output != want {
		t.Errorf("output =\n%q\nwant\n%q", result.Output, want)
	}
	if result.Indent != 4 {
		t.Errorf("indent = %d, want 4", result.Indent)
	}
}

func TestFormatDefaultsToTwoSpaces(t *testing.T) {
	system := newFakeFS().with("config.json", `{"a":1}`)

	result, err := Format(system, FormatRequest{Request: file("config.json")})
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if result.Indent != defaultIndent || !strings.Contains(result.Output, "  \"a\": 1") {
		t.Errorf("output = %q at indent %d, want two spaces", result.Output, result.Indent)
	}
}

func TestFormatRejectsAnAbsurdIndent(t *testing.T) {
	system := newFakeFS().with("config.json", `{"a":1}`)

	for _, width := range []int{-1, maxIndent + 1} {
		_, err := Format(system, FormatRequest{Request: file("config.json"), Indent: width})
		assertCode(t, err, errors.CodeInvalidInput)
	}
}

func TestMinifyReportsWhatItSaved(t *testing.T) {
	system := newFakeFS().with("config.json", "{\n  \"a\": 1,\n  \"b\": [1, 2]\n}\n")

	result, err := Minify(system, MinifyRequest{Request: file("config.json")})
	if err != nil {
		t.Fatalf("Minify: %v", err)
	}

	if result.Output != `{"a":1,"b":[1,2]}` {
		t.Errorf("output = %q", result.Output)
	}
	if result.SavedBytes != result.WasBytes-result.Bytes || result.SavedBytes <= 0 {
		t.Errorf("result = %+v, want a positive saving that adds up", result)
	}
	if result.SavedPercent <= 0 || result.SavedPercent >= 100 {
		t.Errorf("savedPercent = %v, want a share between 0 and 100", result.SavedPercent)
	}
}
