package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/config"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

var exportMoment = time.Date(2026, 7, 23, 14, 15, 2, 0, time.UTC)

func exportSettings() config.Export {
	return config.Export{Directory: "reports", TimestampFiles: false}
}

// A bare filename lands in the configured directory; a path the user typed is
// used as typed, because somebody who named a location meant it.
func TestExportPathHonoursTheConfiguredDirectory(t *testing.T) {
	settings := exportSettings()

	if got, want := exportPath(settings, "scan.json", exportMoment), filepath.Join("reports", "scan.json"); got != want {
		t.Errorf("exportPath(bare) = %q, want %q", got, want)
	}

	typed := filepath.Join("out", "scan.json")
	if got := exportPath(settings, typed, exportMoment); got != typed {
		t.Errorf("exportPath(typed) = %q, want %q", got, typed)
	}
}

// Timestamped files exist so that repeated runs do not overwrite each other,
// which only works if the timestamp goes before the extension.
func TestExportPathInsertsATimestampBeforeTheExtension(t *testing.T) {
	settings := exportSettings()
	settings.TimestampFiles = true

	got := exportPath(settings, "scan.json", exportMoment)
	want := filepath.Join("reports", "scan-20260723-141502.json")
	if got != want {
		t.Errorf("exportPath = %q, want %q", got, want)
	}
}

func TestExportFormatComesFromTheExtension(t *testing.T) {
	cases := map[string]string{
		"scan.json":     "json",
		"scan.csv":      "csv",
		"scan.md":       "markdown",
		"scan.markdown": "markdown",
		"scan.txt":      "table",
	}

	for path, want := range cases {
		got, err := formatForExtension(path)
		if err != nil {
			t.Errorf("formatForExtension(%q): %v", path, err)
			continue
		}
		if got != want {
			t.Errorf("formatForExtension(%q) = %q, want %q", path, got, want)
		}
	}

	if _, err := formatForExtension("scan.dat"); err == nil {
		t.Error("an unknown extension was accepted")
	}
}

// The format is what makes the file readable, so guessing one would be worse
// than asking.
func TestExportRejectsAnUnknownExtensionWithAWayOut(t *testing.T) {
	_, err := newExport(exportSettings(), "scan.dat", "", exportMoment)
	if err == nil {
		t.Fatal("newExport accepted an unknown extension")
	}
	if !strings.Contains(errors.Classify(err).Hint, "--export-format") {
		t.Errorf("hint = %q, want it to name --export-format", errors.Classify(err).Hint)
	}

	if _, err := newExport(exportSettings(), "scan.dat", "json", exportMoment); err != nil {
		t.Errorf("an explicit format was rejected: %v", err)
	}
}

// A flag that quietly does nothing is how somebody ends up with no report and
// no idea why.
func TestExportFormatWithoutAPathIsAnError(t *testing.T) {
	if _, err := newExport(exportSettings(), "", "json", exportMoment); err == nil {
		t.Error("--export-format was accepted without --export")
	}
}

func TestNoExportFlagMeansNoExport(t *testing.T) {
	exported, err := newExport(exportSettings(), "", "", exportMoment)
	if err != nil || exported != nil {
		t.Errorf("newExport = %v, %v, want nil, nil", exported, err)
	}
}

// The whole point of the atomic write is that the file on disk is complete or
// not there at all. The check here is the simpler half: it is complete.
func TestExportWritesTheRenderedResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out", "result.json")

	exported, err := newExport(exportSettings(), path, "", exportMoment)
	if err != nil {
		t.Fatalf("newExport: %v", err)
	}
	if exported.exists() {
		t.Fatal("the target exists before anything was written")
	}

	envelope := output.NewEnvelope(output.Meta{Command: "version"}, map[string]string{"version": "1.0.0"})
	if err := exported.write(envelope, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(contents), "\"version\": \"1.0.0\"") {
		t.Errorf("file does not hold the result:\n%s", contents)
	}
	if !exported.exists() {
		t.Error("exists() is false for a file that was just written")
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("the export directory holds %d files, want 1", len(entries))
	}
}
