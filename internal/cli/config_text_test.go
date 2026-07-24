package cli

import (
	"strings"
	"testing"

	baseconfig "github.com/devnest/devnest/internal/config"
	"github.com/devnest/devnest/internal/core/config"
)

func showResult() config.ShowResult {
	return config.ShowResult{
		Path:   "/home/someone/.config/devnest/config.toml",
		Exists: true,
		Values: []baseconfig.Value{
			{Key: "general.output", Value: "json", Kind: "string", Origin: baseconfig.OriginFile},
			{Key: "general.color", Value: "auto", Kind: "string", Origin: baseconfig.OriginDefault},
			{Key: "scan.exclude", Value: []string{".git", "node_modules"},
				Kind: "array of strings", Origin: baseconfig.OriginEnvironment},
		},
		FromFile:        1,
		FromEnvironment: 1,
	}
}

// The origin column is the whole point: it is the answer to "why is it
// behaving like that".
func TestConfigShowTextCarriesTheOrigins(t *testing.T) {
	got := render(t, configShowText(showResult()))

	for _, want := range []string{"config.toml", "general.output", "json", "file", "environment", "default"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// A list rendered as Go prints it is a value nobody can type back in.
func TestConfigValueTextRendersLists(t *testing.T) {
	if got, want := configValueText([]string{".git", "node_modules"}), ".git, node_modules"; got != want {
		t.Errorf("configValueText = %q, want %q", got, want)
	}
	if got, want := configValueText([]string{}), "(empty)"; got != want {
		t.Errorf("configValueText(empty) = %q, want %q", got, want)
	}
	if got, want := configValueText(int64(20)), "20"; got != want {
		t.Errorf("configValueText(int) = %q, want %q", got, want)
	}
}

// Saying what a value was is what makes a mistyped change easy to undo.
func TestConfigSetTextSaysWhatItReplaced(t *testing.T) {
	result := config.WriteResult{
		Path: "config.toml", Key: "general.output", Value: "json", Previous: "table", Changed: true,
	}

	got := render(t, configWriteText(result, "set"))
	if !strings.Contains(got, "general.output = json") || !strings.Contains(got, "was table") {
		t.Errorf("output = %q", got)
	}
}

func TestConfigUnsetTextNamesTheDefaultItRestored(t *testing.T) {
	result := config.WriteResult{
		Path: "config.toml", Key: "general.output", Value: "table", Changed: true,
	}

	got := render(t, configWriteText(result, "unset"))
	if !strings.Contains(got, "removed") || !strings.Contains(got, "table") {
		t.Errorf("output = %q", got)
	}
}

// Nothing to remove is a success, and reads like one.
func TestConfigUnsetTextOnAKeyThatWasNotSet(t *testing.T) {
	result := config.WriteResult{Path: "config.toml", Key: "general.output"}

	got := render(t, configWriteText(result, "unset"))
	if !strings.Contains(got, "already unset") {
		t.Errorf("output = %q", got)
	}
}

func TestConfigValidateTextDistinguishesNoFileFromABadOne(t *testing.T) {
	absent := render(t, configValidateText(config.ValidateResult{Path: "config.toml", Valid: true}))
	if !strings.Contains(absent, "no file") || !strings.Contains(absent, "defaults") {
		t.Errorf("output = %q", absent)
	}

	valid := render(t, configValidateText(config.ValidateResult{
		Path: "config.toml", Exists: true, Valid: true, Keys: 3,
		Warnings: []baseconfig.Warning{{Message: "unknown configuration key general.colour", Source: "line 4"}},
	}))
	for _, want := range []string{"is valid", "3 key(s)", "general.colour"} {
		if !strings.Contains(valid, want) {
			t.Errorf("output = %q, want it to contain %q", valid, want)
		}
	}
}
