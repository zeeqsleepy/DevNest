package cli

import (
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/core/scaffold"
)

func TestInitListTextListsTemplates(t *testing.T) {
	got := render(t, initListText([]string{"blank", "go-cli"}))

	for _, want := range []string{"blank", "go-cli"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestInitResultTextNamesWhatWasScaffolded(t *testing.T) {
	got := render(t, initResultText(scaffold.Result{
		Template: "go-cli",
		Target:   "./api",
		Files:    []string{"go.mod", "main.go"},
	}))

	for _, want := range []string{"go-cli", "./api", "go.mod", "2 file(s)"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestInitTargetValidatesTheDirectory(t *testing.T) {
	if _, err := initTarget(nil, ""); err == nil {
		t.Error("no target was accepted")
	}
	target, err := initTarget([]string{"proj"}, "go-cli")
	if err != nil || target != "proj" {
		t.Errorf("target = %q, err = %v, want \"proj\" and no error", target, err)
	}
	if _, err := initTarget([]string{"a", "b"}, ""); err == nil {
		t.Error("two targets were accepted")
	}
}
