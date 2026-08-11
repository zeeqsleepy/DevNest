package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func TestTemplatesListsTheEmbeddedSet(t *testing.T) {
	names := Templates()
	if len(names) == 0 {
		t.Fatal("no templates were embedded")
	}
	for _, name := range []string{"blank", "go-cli"} {
		found := false
		for _, candidate := range names {
			if candidate == name {
				found = true
			}
		}
		if !found {
			t.Errorf("template %q is missing from %v", name, names)
		}
	}
}

func TestCreateBuildsAProject(t *testing.T) {
	target := filepath.Join(t.TempDir(), "proj")

	result, err := Create(context.Background(), Request{Template: "go-cli", Target: target})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, file := range []string{"go.mod", "main.go", "README.md"} {
		path := filepath.Join(target, filepath.FromSlash(file))
		if info, statErr := os.Stat(path); statErr != nil || info.IsDir() {
			t.Errorf("%s was not written", file)
		}
	}
	// The go.mod template is stored as .tpl so the module is not a nested Go
	// module; the copied file must carry the real name.
	if _, statErr := os.Stat(filepath.Join(target, "go.mod.tpl")); !os.IsNotExist(statErr) {
		t.Error("the .tpl suffix leaked into the copied project")
	}
	if len(result.Files) != 3 {
		t.Errorf("files = %v, want the three template files", result.Files)
	}
}

func TestCreateDefaultsToBlank(t *testing.T) {
	target := filepath.Join(t.TempDir(), "proj")

	_, err := Create(context.Background(), Request{Target: target})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(target, "README.md")); statErr != nil {
		t.Errorf("blank template README was not written")
	}
}

func TestCreateRefusesToOverwrite(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "existing.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	_, err := Create(context.Background(), Request{Template: "go-cli", Target: target})
	if err == nil {
		t.Fatal("Create overwrote a directory that already had files")
	}
	if got := errors.CodeOf(err); got != errors.CodeConflict {
		t.Errorf("code = %q, want %q", got, errors.CodeConflict)
	}
}

func TestCreateRefusesAFileAsTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "afile.txt")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := Create(context.Background(), Request{Template: "go-cli", Target: target}); err == nil {
		t.Fatal("Create wrote a template over a file")
	}
}

func TestCreateRefusesAnUnknownTemplate(t *testing.T) {
	target := filepath.Join(t.TempDir(), "proj")

	_, err := Create(context.Background(), Request{Template: "nope", Target: target})
	if err == nil {
		t.Fatal("Create accepted an unknown template")
	}
	if got := errors.CodeOf(err); got != errors.CodeNotFound {
		t.Errorf("code = %q, want %q", got, errors.CodeNotFound)
	}
}

func TestCreateRefusesAnEmptyTarget(t *testing.T) {
	_, err := Create(context.Background(), Request{Template: "go-cli"})
	if err == nil {
		t.Fatal("Create accepted an empty target")
	}
	if got := errors.CodeOf(err); got != errors.CodeInvalidInput {
		t.Errorf("code = %q, want %q", got, errors.CodeInvalidInput)
	}
}
