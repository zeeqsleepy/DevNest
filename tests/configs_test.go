package tests

import (
	"path/filepath"
	"testing"

	"github.com/devnest/devnest/internal/config"
)

// The shipped example is the file users copy. If it stops parsing, or sets a
// key DevNest no longer knows, it teaches the wrong thing to exactly the
// people least able to notice.
func TestExampleConfigurationIsValid(t *testing.T) {
	path := filepath.Join(moduleRoot(t), "configs", "config.example.toml")

	loaded, warnings, err := config.Load(config.Source{
		Path:      path,
		Explicit:  true,
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatalf("the example configuration does not load: %v", err)
	}
	for _, warning := range warnings {
		t.Errorf("%s: %s", warning.Source, warning.Message)
	}
	if err := loaded.Validate(); err != nil {
		t.Fatalf("the example configuration is not valid: %v", err)
	}
}

// Every value in the example is the compiled default, so a user who copies it
// and changes nothing gets the behaviour they had before.
func TestExampleConfigurationMatchesTheDefaults(t *testing.T) {
	path := filepath.Join(moduleRoot(t), "configs", "config.example.toml")

	loaded, _, err := config.Load(config.Source{
		Path:      path,
		Explicit:  true,
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	defaults := config.Default()
	if loaded.General != defaults.General {
		t.Errorf("[general] = %+v, want %+v", loaded.General, defaults.General)
	}
	if loaded.Network != defaults.Network {
		t.Errorf("[network] = %+v, want %+v", loaded.Network, defaults.Network)
	}
	if loaded.Export != defaults.Export {
		t.Errorf("[export] = %+v, want %+v", loaded.Export, defaults.Export)
	}
	if loaded.Scan.MaxDepth != defaults.Scan.MaxDepth {
		t.Errorf("scan.max_depth = %d, want %d", loaded.Scan.MaxDepth, defaults.Scan.MaxDepth)
	}
	if loaded.Secret.EntropyThreshold != defaults.Secret.EntropyThreshold {
		t.Errorf("secret.entropy_threshold = %v, want %v",
			loaded.Secret.EntropyThreshold, defaults.Secret.EntropyThreshold)
	}
}
