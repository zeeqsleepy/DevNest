package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeProjectFile writes a .devnest.toml under a directory and returns the
// top of the search tree, so a test can place files at a real working level.
func writeProjectFile(t *testing.T, parts ...string) string {
	t.Helper()
	root := t.TempDir()
	dir := root
	for _, part := range parts {
		dir = filepath.Join(dir, part)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	path := filepath.Join(dir, projectFileName)
	if err := os.WriteFile(path, []byte(projectContents), 0o600); err != nil {
		t.Fatalf("write project file: %v", err)
	}
	return root
}

const projectContents = `
[general]
output    = "json"

[scan]
max_depth = 5

[clean]
patterns = ["vendor_artifacts"]
`

func TestProjectFileDiscoveredByWalkingUp(t *testing.T) {
	root := writeProjectFile(t, "src", "app")

	config, warnings, err := Load(Source{
		Path:       filepath.Join(t.TempDir(), "absent.toml"),
		LookupEnv:  noEnv,
		ProjectDir: filepath.Join(root, "src", "app"),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want the single clean-patterns refusal", warnings)
	}
	if config.General.Output != "json" {
		t.Errorf("Output = %q, want \"json\" from the project file", config.General.Output)
	}
	if config.Scan.MaxDepth != 5 {
		t.Errorf("MaxDepth = %d, want 5", config.Scan.MaxDepth)
	}
}

func TestProjectFileDoesNotAllowDeletingKeys(t *testing.T) {
	root := writeProjectFile(t)

	config, warnings, err := Load(Source{
		Path:       filepath.Join(t.TempDir(), "absent.toml"),
		LookupEnv:  noEnv,
		ProjectDir: root,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// The clean key is recognised but not allowed in a project file, so it is
	// ignored with a warning: a file that travels with a clone must never
	// widen what a delete command will remove.
	wantDefault := Default().Clean.Patterns
	if !reflect.DeepEqual(config.Clean.Patterns, wantDefault) {
		t.Errorf("clean.patterns = %v, want the default %v left alone",
			config.Clean.Patterns, wantDefault)
	}
	found := false
	for _, warning := range warnings {
		if warning.Message == "clean.patterns is not allowed in a project file and has been ignored" {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want one explaining clean.patterns is ignored", warnings)
	}
}

func TestProjectFileBeatsTheUserFile(t *testing.T) {
	root := writeProjectFile(t)

	userFile := filepath.Join(t.TempDir(), "user.toml")
	if err := os.WriteFile(userFile, []byte("[general]\noutput = \"csv\"\n"), 0o600); err != nil {
		t.Fatalf("write user file: %v", err)
	}

	config, _, err := Load(Source{
		Path:       userFile,
		LookupEnv:  noEnv,
		ProjectDir: root,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// The project file is nearer the call site than the machine's file and
	// beats it, but the environment and flags still win.
	if config.General.Output != "json" {
		t.Errorf("Output = %q, want the project value to beat the user file", config.General.Output)
	}
}

func TestEnvironmentStillBeatsTheProjectFile(t *testing.T) {
	root := writeProjectFile(t)

	config, _, err := Load(Source{
		Path:       filepath.Join(t.TempDir(), "absent.toml"),
		LookupEnv:  envMap(map[string]string{"DEVNEST_GENERAL_OUTPUT": "csv"}),
		ProjectDir: root,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if config.General.Output != "csv" {
		t.Errorf("Output = %q, want the environment to beat the project file", config.General.Output)
	}
}

func TestNoProjectDirDisablesDiscovery(t *testing.T) {
	root := writeProjectFile(t)

	config, _, err := Load(Source{
		Path:      filepath.Join(t.TempDir(), "absent.toml"),
		LookupEnv: noEnv,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if config.General.Output != Default().General.Output {
		t.Errorf("Output = %q, want the default (project discovery disabled)", config.General.Output)
	}
	if config.Scan.MaxDepth != 0 {
		t.Errorf("MaxDepth = %d, want 0", config.Scan.MaxDepth)
	}
	_ = root
}

func TestNoProjectFileMeansNoChange(t *testing.T) {
	dir := t.TempDir()

	config, warnings, err := Load(Source{
		Path:       filepath.Join(t.TempDir(), "absent.toml"),
		LookupEnv:  noEnv,
		ProjectDir: dir,
	})
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

func TestProjectFileMalformedIsFatal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, projectFileName),
		[]byte("[general\noutput="), 0o600); err != nil {
		t.Fatalf("write project file: %v", err)
	}

	_, _, err := Load(Source{
		Path:       filepath.Join(t.TempDir(), "absent.toml"),
		LookupEnv:  noEnv,
		ProjectDir: root,
	})
	if err == nil {
		t.Fatal("Load returned no error for a malformed project file")
	}
}

func TestFindProjectFileWalksUpToTheTop(t *testing.T) {
	root := writeProjectFile(t, "deep", "deeper")

	found, err := findProjectFile(filepath.Join(root, "deep", "deeper", "leaf"))
	if err != nil {
		t.Fatalf("findProjectFile: %v", err)
	}
	// The file was written at the deepest level, and a leaf sits beneath it,
	// so walking up from the leaf must find the file that level up.
	want := filepath.Join(root, "deep", "deeper", projectFileName)
	if filepath.Clean(found) != filepath.Clean(want) {
		t.Errorf("found = %q, want %q", found, want)
	}
}
