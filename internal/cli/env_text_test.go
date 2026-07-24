package cli

import (
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/core/env"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/sys"
)

func sampleTools() []env.Tool {
	return []env.Tool{
		{Name: "go", Kind: env.KindLanguage, Found: true, Version: "1.25.0", Path: "/usr/bin/go"},
		{Name: "npm", Kind: env.KindPackage, Found: true, Version: "10.5.0", Path: "/usr/bin/npm"},
		{
			Name: "python", Kind: env.KindLanguage, Found: true, Path: "/usr/bin/python",
			Shadowed: []string{"/usr/local/bin/python"},
			Detail:   "no version could be read from its output",
		},
		{Name: "rustc", Kind: env.KindLanguage},
	}
}

func TestEnvSummaryTextDescribesTheMachine(t *testing.T) {
	result := env.SummaryResult{
		Machine: sys.Info{
			OS: "linux", Architecture: "amd64", CPUs: 8,
			Hostname: "workbench", Shell: "bash",
		},
		Tools:        sampleTools(),
		Found:        3,
		Missing:      1,
		PathEntries:  12,
		PathProblems: 2,
		DurationMs:   140,
	}

	got := render(t, envSummaryText(result))
	for _, want := range []string{
		"linux/amd64", "workbench", "bash", "3 of 4", "140 ms",
		"Languages and runtimes", "Package managers", "go", "1.25.0",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// Unavailable answers are reported as unknown rather than as an empty column,
// so a blank space is never mistaken for a value.
func TestEnvSummaryTextMarksWhatTheMachineWouldNotSay(t *testing.T) {
	got := render(t, envSummaryText(env.SummaryResult{
		Machine: sys.Info{OS: "windows", Architecture: "amd64"},
	}))

	if !strings.Contains(got, "(unknown)") {
		t.Errorf("output = %q, want unavailable answers marked", got)
	}
}

// The version column says why there is no version, which is more useful than
// an empty cell three different situations could have produced.
func TestToolVersionExplainsEveryOutcome(t *testing.T) {
	tests := []struct {
		tool env.Tool
		want string
	}{
		{env.Tool{Found: true, Version: "1.2.3"}, "1.2.3"},
		{env.Tool{Found: true, Detail: "exited 1"}, "unknown"},
		{env.Tool{Found: true}, "installed"},
		{env.Tool{}, "not found"},
	}

	for _, test := range tests {
		if got := toolVersion(test.tool); got != test.want {
			t.Errorf("toolVersion(%+v) = %q, want %q", test.tool, got, test.want)
		}
	}
}

func TestEnvListTextNamesTheShadowedCopies(t *testing.T) {
	result := env.ListResult{Tools: sampleTools(), Found: 3, Missing: 1, DurationMs: 90}

	got := render(t, envListText(result))
	if !strings.Contains(got, "Shadowed copies") {
		t.Errorf("output = %q, want the shadowed listing", got)
	}
	if !strings.Contains(got, "/usr/local/bin/python") {
		t.Errorf("output = %q, want the copy that does not run", got)
	}

	clean := render(t, envListText(env.ListResult{
		Tools: []env.Tool{{Name: "go", Kind: env.KindLanguage, Found: true, Version: "1.25.0"}},
	}))
	if strings.Contains(clean, "Shadowed") {
		t.Errorf("output = %q, want no shadow section when there is nothing to say", clean)
	}
}

func TestEnvPathTextFlagsProblems(t *testing.T) {
	result := env.PathResult{
		Entries: []env.PathEntry{
			{Position: 1, Path: "/usr/bin", Executables: 240, Problems: []env.PathProblem{}},
			{Position: 2, Path: "/gone", Problems: []env.PathProblem{env.ProblemMissing}},
		},
		Shadowed: []env.Shadow{{
			Name:   "python",
			Winner: "/usr/local/bin/python",
			Hidden: []string{"/usr/bin/python"},
		}},
		Problems: 1,
	}

	got := render(t, envPathText(result))
	for _, want := range []string{"/usr/bin", "missing", "ok", "Shadowed executables", "python"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestProblemTextReadsAsALine(t *testing.T) {
	if got := problemText(nil); got != "ok" {
		t.Errorf("problemText(nil) = %q, want ok", got)
	}
	got := problemText([]env.PathProblem{env.ProblemDuplicate, env.ProblemMissing})
	if got != "duplicate, missing" {
		t.Errorf("problemText = %q, want both joined", got)
	}
}

func TestEnvWhichTextShowsEveryCopy(t *testing.T) {
	result := env.WhichResult{
		Name:     "python",
		Winner:   "/usr/local/bin/python",
		Shadowed: true,
		Locations: []env.Location{
			{Position: 1, Path: "/usr/local/bin/python", Version: "3.12.1"},
			{Position: 2, Path: "/usr/bin/python", Detail: "no version could be read"},
		},
	}

	got := render(t, envWhichText(result))
	for _, want := range []string{"found in 2 places", "/usr/local/bin/python", "3.12.1",
		"no version could be read"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}

	empty := render(t, envWhichText(env.WhichResult{Name: "absent"}))
	if !strings.Contains(empty, "not on PATH") {
		t.Errorf("output = %q, want it to say the tool was not found", empty)
	}
}

// The masking is done by the module. What the renderer must not do is print
// the raw value it was handed for a variable marked hidden.
func TestEnvVarsTextPrintsWhatTheModuleDecided(t *testing.T) {
	result := env.VarsResult{
		Variables: []env.Variable{
			{Name: "GITHUB_TOKEN", Value: "(hidden, 19 characters)", Masked: true},
			{Name: "PATH", Value: "/usr/bin:/bin", Entries: 2},
			{Name: "EDITOR", Value: "vim"},
		},
		Total:  3,
		Masked: 1,
	}

	got := render(t, envVarsText(result))
	if !strings.Contains(got, "(hidden, 19 characters)") {
		t.Errorf("output = %q, want the masked value as the module produced it", got)
	}
	if strings.Contains(got, "/usr/bin:/bin") {
		t.Errorf("output = %q, want a path variable summarised rather than printed", got)
	}
	if !strings.Contains(got, "2 entries") {
		t.Errorf("output = %q, want the entry count", got)
	}
}

// Row views carry the numbers unformatted, and the masked value rather than
// the real one: a CSV of this listing gets attached to tickets too.
func TestEnvTablesAreMachineReadable(t *testing.T) {
	tools := envToolTable(sampleTools())()
	if len(tools.Rows) != 4 {
		t.Fatalf("rows = %d, want one per tool", len(tools.Rows))
	}
	if tools.Rows[0][2] != "true" || tools.Rows[3][2] != "false" {
		t.Errorf("found column = %q and %q, want booleans",
			tools.Rows[0][2], tools.Rows[3][2])
	}

	vars := envVarsTable(env.VarsResult{
		Variables: []env.Variable{{Name: "T", Value: "(hidden, 3 characters)", Masked: true}},
	})()
	if vars.Rows[0][1] != "(hidden, 3 characters)" {
		t.Errorf("value = %q, want the masked form", vars.Rows[0][1])
	}

	paths := envPathTable(env.PathResult{
		Entries: []env.PathEntry{{Position: 1, Path: "/usr/bin", Executables: 3}},
	})()
	if paths.Rows[0][2] != "3" {
		t.Errorf("executables = %q, want an unformatted number", paths.Rows[0][2])
	}

	which := envWhichTable(env.WhichResult{
		Name:      "go",
		Locations: []env.Location{{Position: 1, Path: "/usr/bin/go", Version: "1.25.0"}},
	})()
	if which.Rows[0][0] != "go" || which.Rows[0][3] != "1.25.0" {
		t.Errorf("row = %v, want the tool and its version", which.Rows[0])
	}
}

func TestNoArgumentsRejectsALeftoverArgument(t *testing.T) {
	if err := noArguments(nil, "devnest env path"); err != nil {
		t.Errorf("noArguments(nil) = %v, want nil", err)
	}

	err := noArguments([]string{"extra"}, "devnest env path")
	if err == nil {
		t.Fatal("a leftover argument was accepted")
	}
	if errors.CodeOf(err) != errors.CodeInvalidInput {
		t.Errorf("code = %q, want %q", errors.CodeOf(err), errors.CodeInvalidInput)
	}
}
