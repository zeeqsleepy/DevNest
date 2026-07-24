// Package tests holds checks that span the whole application and therefore
// have no single package to live in. Unit tests live beside the code they
// test.
package tests

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/devnest/devnest"

// forbidden lists the import edges that the layering in docs/architecture.md
// rules out. Keeping this mechanical means nobody has to argue it in review,
// and a sideways dependency between modules cannot creep in one merge at a
// time.
var forbidden = []struct {
	from   string // package path prefix, relative to the module root
	to     []string
	reason string
}{
	{
		from:   "internal/core",
		to:     []string{"internal/cli"},
		reason: "a module must not know that a command line exists",
	},
	{
		from:   "internal/core",
		to:     []string{"internal/output"},
		reason: "a module produces data; how it looks is an interface concern",
	},
	{
		from:   "internal/platform",
		to:     []string{"internal/core", "internal/cli"},
		reason: "the platform layer knows nothing about what its callers are doing",
	},
	{
		from:   "internal/errors",
		to:     []string{"internal/cli", "internal/core", "internal/platform", "internal/output", "internal/config"},
		reason: "cross-cutting packages are leaves",
	},
	{
		from:   "internal/logging",
		to:     []string{"internal/cli", "internal/core", "internal/platform", "internal/output", "internal/config"},
		reason: "cross-cutting packages are leaves",
	},
	{
		from:   "internal/output",
		to:     []string{"internal/cli", "internal/core", "internal/platform", "internal/config"},
		reason: "cross-cutting packages are leaves",
	},
	{
		from:   "internal/config",
		to:     []string{"internal/cli", "internal/core", "internal/platform", "internal/output"},
		reason: "cross-cutting packages are leaves",
	},
	{
		from:   "internal/version",
		to:     []string{"internal/cli", "internal/core", "internal/platform", "internal/output", "internal/config"},
		reason: "cross-cutting packages are leaves",
	},
	{
		from:   "pkg",
		to:     []string{"internal"},
		reason: "public packages must not depend on private ones",
	},
}

func TestImportBoundaries(t *testing.T) {
	root := moduleRoot(t)

	for directory, imports := range collectImports(t, root) {
		for _, rule := range forbidden {
			if !within(directory, rule.from) {
				continue
			}
			for _, imported := range imports {
				for _, banned := range rule.to {
					if within(imported, banned) {
						t.Errorf("%s imports %s: %s", directory, imported, rule.reason)
					}
				}
			}
		}
	}
}

// A module may not import another module. Anything two modules need lives one
// layer down, never sideways.
func TestModulesDoNotImportEachOther(t *testing.T) {
	root := moduleRoot(t)

	for directory, imports := range collectImports(t, root) {
		owner, ok := moduleName(directory)
		if !ok {
			continue
		}
		for _, imported := range imports {
			other, ok := moduleName(imported)
			if ok && other != owner {
				t.Errorf("module %q imports module %q; move the shared code down a layer",
					owner, other)
			}
		}
	}
}

// The entrypoint owns process concerns only. Anything that grows belongs in
// internal/cli, and the size check is what keeps it honest.
func TestEntrypointStaysThin(t *testing.T) {
	root := moduleRoot(t)
	path := filepath.Join(root, "cmd", "devnest", "main.go")

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	lines := strings.Count(string(contents), "\n")
	if lines > 150 {
		t.Errorf("%s has %d lines; process startup should not need that much, "+
			"so something has leaked out of internal/cli", path, lines)
	}
}

// moduleName returns the module a package belongs to, if any.
func moduleName(directory string) (string, bool) {
	const prefix = "internal/core/"
	if !strings.HasPrefix(directory, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(directory, prefix)
	name, _, _ := strings.Cut(rest, "/")
	return name, name != ""
}

func within(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// collectImports maps each package directory, relative to the module root, to
// the DevNest packages it imports.
func collectImports(t *testing.T, root string) map[string][]string {
	t.Helper()

	imports := make(map[string][]string)
	fileSet := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipDirectory(entry.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		parsed, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}

		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		directory := filepath.ToSlash(relative)

		for _, spec := range parsed.Imports {
			value, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if trimmed, found := strings.CutPrefix(value, modulePath+"/"); found {
				imports[directory] = append(imports[directory], trimmed)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the source tree: %v", err)
	}

	return imports
}

func skipDirectory(name string) bool {
	switch name {
	case ".git", "dist", "vendor", "testdata", "fixtures", "tools":
		return true
	}
	return false
}

// moduleRoot walks up from the test's working directory to the directory
// holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()

	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("go.mod not found above the working directory")
		}
		directory = parent
	}
}
