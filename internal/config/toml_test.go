package config

import (
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func parseOne(t *testing.T, contents string) []entry {
	t.Helper()
	entries, err := parseTOML("config.toml", []byte(contents))
	if err != nil {
		t.Fatalf("parseTOML: %v", err)
	}
	return entries
}

func TestParseValueKinds(t *testing.T) {
	entries := parseOne(t, `
[general]
output  = "json"
confirm = true
depth   = 12
ratio   = 4.5
literal = 'C:\raw\path'
list    = ["a", "b"]
empty   = []
`)

	got := make(map[string]any, len(entries))
	for _, e := range entries {
		got[e.key] = e.value
	}

	if got["output"] != "json" {
		t.Errorf("output = %#v, want \"json\"", got["output"])
	}
	if got["confirm"] != true {
		t.Errorf("confirm = %#v, want true", got["confirm"])
	}
	if got["depth"] != int64(12) {
		t.Errorf("depth = %#v, want int64(12)", got["depth"])
	}
	if got["ratio"] != 4.5 {
		t.Errorf("ratio = %#v, want 4.5", got["ratio"])
	}
	if got["literal"] != `C:\raw\path` {
		t.Errorf("literal = %#v, want the backslashes preserved", got["literal"])
	}
	if list, ok := got["list"].([]any); !ok || len(list) != 2 || list[0] != "a" {
		t.Errorf("list = %#v, want [a b]", got["list"])
	}
	if list, ok := got["empty"].([]any); !ok || len(list) != 0 {
		t.Errorf("empty = %#v, want an empty array", got["empty"])
	}
}

func TestParseIgnoresCommentsAndBlankLines(t *testing.T) {
	entries := parseOne(t, `
# a leading comment

[general]        # trailing comment on a section
output = "json"  # trailing comment on a value

`)

	if len(entries) != 1 {
		t.Fatalf("entries = %#v, want exactly one", entries)
	}
	if entries[0].value != "json" {
		t.Errorf("value = %#v, want the comment stripped", entries[0].value)
	}
}

func TestParseKeepsHashInsideStrings(t *testing.T) {
	entries := parseOne(t, "[secret]\nexclude_paths = [\"#temp\", \"b\"]\n")

	list, ok := entries[0].value.([]any)
	if !ok || len(list) != 2 || list[0] != "#temp" {
		t.Errorf("value = %#v, want the hash preserved inside the string", entries[0].value)
	}
}

func TestParseEscapeSequences(t *testing.T) {
	entries := parseOne(t, "[general]\noutput = \"a\\tb\\nc\\\"d\"\n")

	if entries[0].value != "a\tb\nc\"d" {
		t.Errorf("value = %q, want the escapes decoded", entries[0].value)
	}
}

// Notepad and several other Windows editors write a byte order mark. A
// configuration file saved with one has to load like any other.
func TestParseAcceptsAByteOrderMark(t *testing.T) {
	entries := parseOne(t, "\uFEFF[general]\noutput = \"json\"\n")

	if len(entries) != 1 {
		t.Fatalf("entries = %#v, want exactly one", entries)
	}
	if entries[0].section != "general" || entries[0].value != "json" {
		t.Errorf("entry = %#v, want general.output = json", entries[0])
	}
}

// CRLF line endings are the Windows default and must not reach the values.
func TestParseAcceptsWindowsLineEndings(t *testing.T) {
	entries := parseOne(t, "[general]\r\noutput = \"json\"\r\n")

	if len(entries) != 1 || entries[0].value != "json" {
		t.Errorf("entries = %#v, want the carriage return stripped", entries)
	}
}

func TestParseRecordsTheLineNumber(t *testing.T) {
	entries := parseOne(t, "\n\n[general]\noutput = \"json\"\n")

	if entries[0].line != 4 {
		t.Errorf("line = %d, want 4", entries[0].line)
	}
	if !strings.Contains(entries[0].where(), "line 4") {
		t.Errorf("where() = %q, want it to name the line", entries[0].where())
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantMsg string
	}{
		{"key before section", "output = \"json\"\n", "before any [section]"},
		{"missing equals", "[general]\noutput json\n", "expected \"key = value\""},
		{"unterminated string", "[general]\noutput = \"json\n", "unterminated string"},
		{"unterminated array", "[general]\nexclude = [\"a\",\n", "unterminated array"},
		{"malformed section", "[general\noutput = \"json\"\n", "malformed section header"},
		{"nested section", "[a.b]\noutput = \"json\"\n", "nested sections are not supported"},
		{"dotted key", "[general]\na.b = 1\n", "dotted keys are not supported"},
		{"bare value", "[general]\noutput = json\n", "unrecognised value"},
		{"no value", "[general]\noutput =\n", "no value"},
		{"bad escape", "[general]\noutput = \"a\\qb\"\n", "unsupported escape"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseTOML("config.toml", []byte(test.input))
			if err == nil {
				t.Fatal("parseTOML accepted invalid input")
			}
			if got := errors.CodeOf(err); got != errors.CodeParse {
				t.Errorf("code = %q, want %q", got, errors.CodeParse)
			}
			if !strings.Contains(err.Error(), test.wantMsg) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), test.wantMsg)
			}
			if !strings.Contains(err.Error(), "line ") {
				t.Errorf("error = %q, want it to name the line", err.Error())
			}
		})
	}
}

func TestEnvNameMatchesTheDocumentedPattern(t *testing.T) {
	index := fieldIndex()

	f, ok := index["scan.max_depth"]
	if !ok {
		t.Fatal("scan.max_depth is not a known field")
	}
	if got := f.envName(); got != "DEVNEST_SCAN_MAX_DEPTH" {
		t.Errorf("envName = %q, want DEVNEST_SCAN_MAX_DEPTH", got)
	}
}

func TestEveryFieldHasAUniqueEnvName(t *testing.T) {
	seen := make(map[string]string)
	for _, f := range fields() {
		name := f.envName()
		if previous, clash := seen[name]; clash {
			t.Errorf("%s.%s and %s share the environment name %s", f.section, f.key, previous, name)
		}
		seen[name] = f.section + "." + f.key
	}
}
