package version

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestGetReportsRuntimeValues(t *testing.T) {
	info := Get()

	if info.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", info.GoVersion, runtime.Version())
	}
	if info.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", info.OS, runtime.GOOS)
	}
	if info.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", info.Arch, runtime.GOARCH)
	}
}

func TestGetNeverReturnsEmptyFields(t *testing.T) {
	info := Get()

	fields := map[string]string{
		"Version":   info.Version,
		"Commit":    info.Commit,
		"BuildDate": info.BuildDate,
	}
	for name, value := range fields {
		if value == "" {
			t.Errorf("%s is empty; a build without -ldflags must still report a fallback", name)
		}
	}
}

func TestShortMatchesVersionField(t *testing.T) {
	if Short() != Get().Version {
		t.Errorf("Short() = %q, want %q", Short(), Get().Version)
	}
}

func TestStringIncludesVersionAndCommit(t *testing.T) {
	info := Get()
	text := info.String()

	if !strings.Contains(text, info.Version) {
		t.Errorf("String() = %q, want it to contain the version %q", text, info.Version)
	}
	if !strings.Contains(text, info.Commit) {
		t.Errorf("String() = %q, want it to contain the commit %q", text, info.Commit)
	}
}

// The JSON field names are part of the output contract, so a rename has to be
// a deliberate change rather than a side effect of renaming a Go field.
func TestInfoJSONFieldNames(t *testing.T) {
	encoded, err := json.Marshal(Get())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	for _, name := range []string{"version", "commit", "buildDate", "goVersion", "os", "arch"} {
		if _, ok := decoded[name]; !ok {
			t.Errorf("missing JSON field %q in %s", name, encoded)
		}
	}
}
