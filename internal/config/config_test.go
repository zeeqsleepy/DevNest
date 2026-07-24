package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func noEnv(string) (string, bool) { return "", false }

func envMap(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("the compiled defaults must be valid: %v", err)
	}
}

func TestMissingDefaultFileIsNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.toml")

	config, warnings, err := Load(Source{Path: path, LookupEnv: noEnv})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if config.General.Output != Default().General.Output {
		t.Errorf("Output = %q, want the default", config.General.Output)
	}
}

func TestMissingExplicitFileIsFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.toml")

	_, _, err := Load(Source{Path: path, Explicit: true, LookupEnv: noEnv})
	if err == nil {
		t.Fatal("Load returned no error for a file the user named explicitly")
	}
	if got := errors.CodeOf(err); got != errors.CodeNotFound {
		t.Errorf("code = %q, want %q", got, errors.CodeNotFound)
	}
}

func TestFileOverridesDefaults(t *testing.T) {
	path := writeConfig(t, `
# DevNest configuration
[general]
output    = "json"
verbosity = "debug"
confirm   = false

[scan]
max_depth = 5
exclude   = [".git", "vendor"]

[secret]
entropy_threshold = 5.5
`)

	config, warnings, err := Load(Source{Path: path, LookupEnv: noEnv})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}

	if config.General.Output != "json" {
		t.Errorf("Output = %q, want \"json\"", config.General.Output)
	}
	if config.General.Confirm {
		t.Error("Confirm = true, want false")
	}
	if config.Scan.MaxDepth != 5 {
		t.Errorf("MaxDepth = %d, want 5", config.Scan.MaxDepth)
	}
	if len(config.Scan.Exclude) != 2 || config.Scan.Exclude[1] != "vendor" {
		t.Errorf("Exclude = %v, want [.git vendor]", config.Scan.Exclude)
	}
	if config.Secret.EntropyThreshold != 5.5 {
		t.Errorf("EntropyThreshold = %v, want 5.5", config.Secret.EntropyThreshold)
	}
	// Keys the file did not set keep their defaults.
	if config.General.Color != Default().General.Color {
		t.Errorf("Color = %q, want the default", config.General.Color)
	}
}

func TestEnvironmentOverridesFile(t *testing.T) {
	path := writeConfig(t, "[general]\noutput = \"table\"\n")

	config, _, err := Load(Source{
		Path:      path,
		LookupEnv: envMap(map[string]string{"DEVNEST_GENERAL_OUTPUT": "json"}),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if config.General.Output != "json" {
		t.Errorf("Output = %q, want the environment value", config.General.Output)
	}
}

func TestEnvironmentParsesEveryKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.toml")

	config, _, err := Load(Source{Path: path, LookupEnv: envMap(map[string]string{
		"DEVNEST_GENERAL_CONFIRM":          "false",
		"DEVNEST_SCAN_MAX_DEPTH":           "7",
		"DEVNEST_SCAN_EXCLUDE":             ".git, dist , node_modules",
		"DEVNEST_SECRET_ENTROPY_THRESHOLD": "6",
		"DEVNEST_EXPORT_DIRECTORY":         "out",
	})})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if config.General.Confirm {
		t.Error("Confirm = true, want false")
	}
	if config.Scan.MaxDepth != 7 {
		t.Errorf("MaxDepth = %d, want 7", config.Scan.MaxDepth)
	}
	if len(config.Scan.Exclude) != 3 || config.Scan.Exclude[1] != "dist" {
		t.Errorf("Exclude = %v, want the list split and trimmed", config.Scan.Exclude)
	}
	if config.Secret.EntropyThreshold != 6 {
		t.Errorf("EntropyThreshold = %v, want 6", config.Secret.EntropyThreshold)
	}
	if config.Export.Directory != "out" {
		t.Errorf("Directory = %q, want \"out\"", config.Export.Directory)
	}
}

func TestMalformedEnvironmentValueIsFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.toml")

	_, _, err := Load(Source{
		Path:      path,
		LookupEnv: envMap(map[string]string{"DEVNEST_SCAN_MAX_DEPTH": "deep"}),
	})
	if err == nil {
		t.Fatal("Load accepted a non-numeric value for an integer key")
	}
	if got := errors.CodeOf(err); got != errors.CodeInvalidInput {
		t.Errorf("code = %q, want %q", got, errors.CodeInvalidInput)
	}
}

func TestUnknownKeyIsAWarningNotAFailure(t *testing.T) {
	path := writeConfig(t, "[general]\noutput = \"json\"\nfuture_key = 1\n")

	config, warnings, err := Load(Source{Path: path, LookupEnv: noEnv})
	if err != nil {
		t.Fatalf("Load: %v; an older binary must still read a newer file", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	if config.General.Output != "json" {
		t.Errorf("Output = %q, want the known key still applied", config.General.Output)
	}
}

func TestTypeMismatchIsFatalAndNamesTheKey(t *testing.T) {
	path := writeConfig(t, "[general]\noutput = 14\n")

	_, _, err := Load(Source{Path: path, LookupEnv: noEnv})
	if err == nil {
		t.Fatal("Load accepted an integer for a string key")
	}
	if got := errors.CodeOf(err); got != errors.CodeParse {
		t.Errorf("code = %q, want %q", got, errors.CodeParse)
	}
	if want := "general.output"; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to name %q", err.Error(), want)
	}
}

func TestValidateRejectsUnknownEnumValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"output", func(c *Config) { c.General.Output = "markdown" }},
		{"color", func(c *Config) { c.General.Color = "sometimes" }},
		{"verbosity", func(c *Config) { c.General.Verbosity = "trace" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := Default()
			test.mutate(&config)
			err := config.Validate()
			if err == nil {
				t.Fatal("Validate accepted an out-of-range value")
			}
			if got := errors.CodeOf(err); got != errors.CodeInvalidInput {
				t.Errorf("code = %q, want %q", got, errors.CodeInvalidInput)
			}
		})
	}
}

func TestValidateRejectsOutOfRangeNumbers(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"negative depth", func(c *Config) { c.Scan.MaxDepth = -1 }},
		{"entropy too high", func(c *Config) { c.Secret.EntropyThreshold = 12 }},
		{"zero timeout", func(c *Config) { c.Network.TimeoutMs = 0 }},
		{"negative redirects", func(c *Config) { c.Network.MaxRedirects = -1 }},
		{"no attempts", func(c *Config) { c.Network.Attempts = 0 }},
		{"too many attempts", func(c *Config) { c.Network.Attempts = 5000 }},
		{"negative interval", func(c *Config) { c.Network.IntervalMs = -1 }},
		{"empty export directory", func(c *Config) { c.Export.Directory = "  " }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := Default()
			test.mutate(&config)
			if err := config.Validate(); err == nil {
				t.Error("Validate accepted an out-of-range value")
			}
		})
	}
}

func TestDefaultPathIsPlatformSpecific(t *testing.T) {
	path, err := DefaultPath()
	if err != nil {
		t.Skipf("no user configuration directory on this system: %v", err)
	}
	if filepath.Base(path) != "config.toml" {
		t.Errorf("path = %q, want it to end in config.toml", path)
	}
	if filepath.Base(filepath.Dir(path)) != "devnest" {
		t.Errorf("path = %q, want it inside a devnest directory", path)
	}
}
