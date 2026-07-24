package config

import (
	"strings"
	"testing"
)

const handWritten = `# my settings
[general]
output = "table"   # what I usually want
verbosity = "warn"

[network]
timeout_ms = 30000
`

// The file is hand-written and hand-commented. Changing one value must leave
// every other line exactly as it was.
func TestSetInTextChangesOneLine(t *testing.T) {
	got, err := SetInText([]byte(handWritten), "general.output", "json")
	if err != nil {
		t.Fatalf("SetInText: %v", err)
	}

	if !strings.Contains(string(got), `output = "json"`) {
		t.Errorf("the value was not set:\n%s", got)
	}
	for _, want := range []string{"# my settings", `verbosity = "warn"`, "timeout_ms = 30000"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("%q was lost:\n%s", want, got)
		}
	}
}

func TestSetInTextAddsAKeyToAnExistingSection(t *testing.T) {
	got, err := SetInText([]byte(handWritten), "general.confirm", false)
	if err != nil {
		t.Fatalf("SetInText: %v", err)
	}

	general := section(t, string(got), "general")
	if !strings.Contains(general, "confirm = false") {
		t.Errorf("the key did not land in [general]:\n%s", got)
	}
	if strings.Contains(section(t, string(got), "network"), "confirm") {
		t.Errorf("the key landed in the wrong section:\n%s", got)
	}
}

func TestSetInTextAddsAMissingSection(t *testing.T) {
	got, err := SetInText([]byte(handWritten), "export.directory", "out")
	if err != nil {
		t.Fatalf("SetInText: %v", err)
	}

	if !strings.Contains(string(got), "[export]") || !strings.Contains(string(got), `directory = "out"`) {
		t.Errorf("the section was not added:\n%s", got)
	}
}

func TestSetInTextWritesIntoAnEmptyFile(t *testing.T) {
	got, err := SetInText(nil, "general.output", "json")
	if err != nil {
		t.Fatalf("SetInText: %v", err)
	}

	if string(got) != "[general]\noutput = \"json\"\n" {
		t.Errorf("got:\n%q", got)
	}
}

func TestUnsetInTextRemovesOnlyTheKey(t *testing.T) {
	got, removed, err := UnsetInText([]byte(handWritten), "general.output")
	if err != nil || !removed {
		t.Fatalf("UnsetInText = %v, %v", removed, err)
	}
	if strings.Contains(string(got), "output =") {
		t.Errorf("the key is still there:\n%s", got)
	}
	if !strings.Contains(string(got), `verbosity = "warn"`) {
		t.Errorf("a neighbouring key was removed:\n%s", got)
	}

	_, removed, err = UnsetInText([]byte(handWritten), "general.confirm")
	if err != nil {
		t.Fatalf("UnsetInText: %v", err)
	}
	if removed {
		t.Error("a key that was not in the file was reported as removed")
	}
}

// The file "config init" writes has to be a file DevNest itself accepts, and
// it has to carry every key rather than the ones somebody remembered.
func TestTemplateHoldsEveryKeyAndParses(t *testing.T) {
	template := Template()

	entries, err := parseTOML("template", template)
	if err != nil {
		t.Fatalf("the template does not parse: %v\n%s", err, template)
	}

	found := map[string]bool{}
	for _, e := range entries {
		found[e.section+"."+e.key] = true
	}
	for _, key := range Keys() {
		if !found[key] {
			t.Errorf("the template is missing %s", key)
		}
	}

	config := Default()
	if _, _, err := bind(&config, entries); err != nil {
		t.Fatalf("the template does not bind: %v", err)
	}
	if err := config.Validate(); err != nil {
		t.Errorf("the template does not validate: %v", err)
	}
}

func TestLiteralRendersEachKind(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{"json", `"json"`},
		{true, "true"},
		{int64(20), "20"},
		{4.5, "4.5"},
		{[]string{"a", "b"}, `["a", "b"]`},
	}

	for _, test := range cases {
		if got := Literal(test.value); got != test.want {
			t.Errorf("Literal(%v) = %q, want %q", test.value, got, test.want)
		}
	}
}

func TestKeyWithoutASectionIsRejected(t *testing.T) {
	if _, err := SetInText(nil, "output", "json"); err == nil {
		t.Error("a key with no section was accepted")
	}
}

// section returns the lines under one heading, for asserting where a key
// landed rather than only that it exists somewhere.
func section(t *testing.T, contents, name string) string {
	t.Helper()

	var collected []string
	current := ""
	for _, line := range strings.Split(contents, "\n") {
		trimmed := strings.TrimSpace(line)
		if heading, ok := sectionHeading(trimmed); ok {
			current = heading
			continue
		}
		if current == name {
			collected = append(collected, trimmed)
		}
	}
	return strings.Join(collected, "\n")
}
