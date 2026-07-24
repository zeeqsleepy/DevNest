package output

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

type extension struct {
	Extension string `json:"extension"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
}

type scanResult struct {
	Root        string      `json:"root"`
	TotalFiles  int         `json:"totalFiles"`
	TotalBytes  int64       `json:"totalBytes"`
	Clean       bool        `json:"clean"`
	DurationMs  int64       `json:"durationMs"`
	ByExtension []extension `json:"byExtension"`
	Empty       []extension `json:"empty"`
}

func markdown(t *testing.T, envelope Envelope) string {
	t.Helper()

	var buffer bytes.Buffer
	if err := (markdownRenderer{}).Render(&buffer, envelope, nil); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return buffer.String()
}

func fixture() Envelope {
	meta := Meta{
		Version:    "1.0.0",
		Command:    "scan",
		StartedAt:  time.Date(2026, 7, 23, 14, 15, 2, 0, time.UTC),
		DurationMs: 1834,
	}
	return NewEnvelope(meta, scanResult{
		Root:       `C:\projects\api`,
		TotalFiles: 38412,
		TotalBytes: 2469606195,
		Clean:      true,
		DurationMs: 1834,
		ByExtension: []extension{
			{Extension: ".ts", Files: 1842, Bytes: 8934112},
			{Extension: ".json", Files: 312, Bytes: 1204331},
		},
	})
}

// The report is for a person, so the numbers are formatted. The field-name
// conventions are what make that possible without a per-command view.
func TestMarkdownFormatsValuesForReading(t *testing.T) {
	got := markdown(t, fixture())

	for _, want := range []string{
		"# Scan report",
		"Generated 2026-07-23 14:15:02 UTC with DevNest 1.0.0",
		"| Total files | 38,412 |",
		"| Total bytes | 2.3 GB |",
		"| Clean | yes |",
		"| Duration | 1.8 s |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report does not contain %q:\n%s", want, got)
		}
	}
}

// A list of objects is the shape most results have, and a table is what it is
// for. The column order has to be the order the result declared, or a report
// re-run tomorrow reads differently for no reason.
func TestMarkdownRendersAListOfObjectsAsATable(t *testing.T) {
	got := markdown(t, fixture())

	want := "\n## By extension\n\n| Extension | Files | Bytes |\n|---|---|---|\n" +
		"| .ts | 1,842 | 8.5 MB |\n| .json | 312 | 1.1 MB |\n"
	if !strings.Contains(got, want) {
		t.Errorf("report does not contain\n%s\ngot:\n%s", want, got)
	}
}

// An empty list gets a row saying so rather than a heading with nothing under
// it, which reads like the report was cut off.
func TestMarkdownSaysWhenAListIsEmpty(t *testing.T) {
	got := markdown(t, fixture())

	if !strings.Contains(got, "| Empty | none |") {
		t.Errorf("report does not report the empty list:\n%s", got)
	}
	if strings.Contains(got, "## Empty") {
		t.Errorf("report has a heading for an empty list:\n%s", got)
	}
}

func TestMarkdownIncludesWarnings(t *testing.T) {
	envelope := fixture().WithWarnings([]Warning{
		{Code: "PERMISSION_DENIED", Message: "cannot read directory", Path: `node_modules\.cache`},
	})

	got := markdown(t, envelope)
	if !strings.Contains(got, "## Warnings") || !strings.Contains(got, "cannot read directory") {
		t.Errorf("report does not carry the warning:\n%s", got)
	}
}

// A pipe inside a value would end the cell and shift every column after it.
func TestMarkdownEscapesPipesInValues(t *testing.T) {
	envelope := NewEnvelope(Meta{Command: "log"}, map[string]any{
		"pattern": "GET |POST",
	})

	got := markdown(t, envelope)
	if !strings.Contains(got, `GET \|POST`) {
		t.Errorf("a pipe was left unescaped:\n%s", got)
	}
}

func TestMarkdownReportsAFailure(t *testing.T) {
	envelope := fixture().WithError(ErrorInfo{
		Code: "NOT_FOUND", Message: "no such directory", Hint: "check the path",
	})

	got := markdown(t, envelope)
	for _, want := range []string{"## Failed", "no such directory", "check the path"} {
		if !strings.Contains(got, want) {
			t.Errorf("report does not contain %q:\n%s", want, got)
		}
	}
}

func TestSentenceReadsLikeALabel(t *testing.T) {
	cases := map[string]string{
		"byExtension": "By extension",
		"totalBytes":  "Total bytes",
		"durationMs":  "Duration",
		"root":        "Root",
	}

	for name, want := range cases {
		if got := sentence(name); got != want {
			t.Errorf("sentence(%q) = %q, want %q", name, got, want)
		}
	}
}
