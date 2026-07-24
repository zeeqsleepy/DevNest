package cli

import (
	"flag"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func TestParseSize(t *testing.T) {
	tests := map[string]int64{
		"0":      0,
		"512":    512,
		"512B":   512,
		"1KB":    1024,
		"1k":     1024,
		"10MB":   10 * 1024 * 1024,
		"1.5GB":  1610612736,
		"2G":     2 * 1024 * 1024 * 1024,
		" 4 MB ": 4 * 1024 * 1024,
	}

	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			got, err := ParseSize(input)
			if err != nil {
				t.Fatalf("ParseSize(%q): %v", input, err)
			}
			if got != want {
				t.Errorf("ParseSize(%q) = %d, want %d", input, got, want)
			}
		})
	}
}

func TestParseSizeRejectsNonsense(t *testing.T) {
	for _, input := range []string{"", "big", "-5", "MB", "1XB"} {
		t.Run(input, func(t *testing.T) {
			_, err := ParseSize(input)
			if err == nil {
				t.Fatalf("ParseSize(%q) returned no error", input)
			}
			if got := errors.CodeOf(err); got != errors.CodeInvalidInput {
				t.Errorf("code = %q, want %q", got, errors.CodeInvalidInput)
			}
		})
	}
}

func TestSizeValueFlag(t *testing.T) {
	var target int64
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.Var(newSizeValue(&target, 7), "min-size", "")

	if target != 7 {
		t.Errorf("default = %d, want the initial value 7", target)
	}
	if err := set.Parse([]string{"--min-size", "2KB"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if target != 2048 {
		t.Errorf("target = %d, want 2048", target)
	}
}

func TestRepeatableFlagAccumulates(t *testing.T) {
	var values repeatable
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.Var(&values, "exclude", "")

	err := set.Parse([]string{"--exclude", "a", "--exclude", " b ", "--exclude", "  "})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(values) != 2 || values[0] != "a" || values[1] != "b" {
		t.Errorf("values = %v, want [a b] with blanks dropped", values)
	}
}

func TestParseReplacements(t *testing.T) {
	replacements, err := parseReplacements([]string{"IMG_=photo-", "old=", "a=b"})
	if err != nil {
		t.Fatalf("parseReplacements: %v", err)
	}
	if len(replacements) != 3 {
		t.Fatalf("got %d replacements, want 3", len(replacements))
	}
	// An empty replacement is how a substring is deleted.
	if replacements[1].From != "old" || replacements[1].To != "" {
		t.Errorf("replacement = %+v, want a deletion", replacements[1])
	}
}

func TestParseReplacementsRejectsMalformedPairs(t *testing.T) {
	for _, input := range []string{"nofrom", "=to"} {
		if _, err := parseReplacements([]string{input}); err == nil {
			t.Errorf("parseReplacements(%q) returned no error", input)
		}
	}
}

func TestParseSequencePosition(t *testing.T) {
	for _, input := range []string{"", "suffix", "SUFFIX"} {
		got, err := parseSequencePosition(input)
		if err != nil || got != "suffix" {
			t.Errorf("parseSequencePosition(%q) = %q, %v", input, got, err)
		}
	}
	if got, err := parseSequencePosition("prefix"); err != nil || got != "prefix" {
		t.Errorf("parseSequencePosition(\"prefix\") = %q, %v", got, err)
	}
	if _, err := parseSequencePosition("middle"); err == nil {
		t.Error("parseSequencePosition(\"middle\") returned no error")
	}
}

func TestChooseAlgorithms(t *testing.T) {
	chosen, err := chooseAlgorithms(nil, false)
	if err != nil || len(chosen) != 1 || chosen[0] != "sha256" {
		t.Errorf("default = %v, %v; want [sha256]", chosen, err)
	}

	chosen, err = chooseAlgorithms([]string{"md5", "MD5", "sha512"}, false)
	if err != nil {
		t.Fatalf("chooseAlgorithms: %v", err)
	}
	if len(chosen) != 2 {
		t.Errorf("chosen = %v, want duplicates dropped", chosen)
	}

	if _, err := chooseAlgorithms([]string{"sha1"}, false); err == nil {
		t.Error("an unsupported algorithm was accepted")
	}

	chosen, err = chooseAlgorithms([]string{"md5"}, true)
	if err != nil || len(chosen) != 3 {
		t.Errorf("--all = %v, %v; want every supported digest", chosen, err)
	}
}
