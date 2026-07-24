package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

type run struct {
	stdout string
	stderr string
	err    error
}

// exec runs one invocation against in-memory streams. Nothing here spawns a
// process, which is the point of Execute taking its streams as parameters.
func exec(t *testing.T, env map[string]string, args ...string) run {
	t.Helper()

	var stdout, stderr bytes.Buffer
	opts := Options{
		Args:   args,
		Stdout: &stdout,
		Stderr: &stderr,
		LookupEnv: func(name string) (string, bool) {
			value, ok := env[name]
			return value, ok
		},
	}

	err := Execute(context.Background(), opts)
	if err != nil {
		ReportError(err, opts)
	}
	return run{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

// isolated points configuration at an empty file, so a developer's own
// configuration cannot change what the tests observe.
func isolated(t *testing.T) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write empty configuration: %v", err)
	}
	return []string{"--config", path}
}

func TestVersionTextOutput(t *testing.T) {
	got := exec(t, nil, append(isolated(t), "version")...)
	if got.err != nil {
		t.Fatalf("Execute: %v", got.err)
	}

	for _, label := range []string{"version", "commit", "built", "go", "platform"} {
		if !strings.Contains(got.stdout, label) {
			t.Errorf("stdout = %q, want the %q field", got.stdout, label)
		}
	}
	if got.stderr != "" {
		t.Errorf("stderr = %q, want it empty on success", got.stderr)
	}
}

func TestVersionJSONMatchesTheEnvelope(t *testing.T) {
	got := exec(t, nil, append(isolated(t), "version", "--output", "json")...)
	if got.err != nil {
		t.Fatalf("Execute: %v", got.err)
	}

	var envelope struct {
		DevNest struct {
			Version string `json:"version"`
			Command string `json:"command"`
		} `json:"devnest"`
		Status   string          `json:"status"`
		Data     map[string]any  `json:"data"`
		Warnings []any           `json:"warnings"`
		Error    json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, got.stdout)
	}

	if envelope.Status != "ok" {
		t.Errorf("status = %q, want \"ok\"", envelope.Status)
	}
	if envelope.DevNest.Command != "version" {
		t.Errorf("command = %q, want \"version\"", envelope.DevNest.Command)
	}
	if envelope.Warnings == nil {
		t.Error("warnings is null; it must always be an array")
	}
	if _, ok := envelope.Data["goVersion"]; !ok {
		t.Errorf("data = %v, want the version fields", envelope.Data)
	}
}

func TestFlagPositionDoesNotMatter(t *testing.T) {
	before := exec(t, nil, append(isolated(t), "--output", "json", "version")...)
	after := exec(t, nil, append(isolated(t), "version", "--output", "json")...)

	if before.err != nil || after.err != nil {
		t.Fatalf("Execute: %v / %v", before.err, after.err)
	}
	if (before.stdout == "") || (after.stdout == "") {
		t.Fatal("one of the invocations produced no output")
	}
	if strings.Contains(before.stdout, "\"status\"") != strings.Contains(after.stdout, "\"status\"") {
		t.Error("flag position changed the output format")
	}
}

func TestInlineFlagValue(t *testing.T) {
	got := exec(t, nil, append(isolated(t), "version", "--output=json")...)
	if got.err != nil {
		t.Fatalf("Execute: %v", got.err)
	}
	if !strings.HasPrefix(strings.TrimSpace(got.stdout), "{") {
		t.Errorf("stdout = %q, want JSON", got.stdout)
	}
}

func TestBareInvocationShowsHelp(t *testing.T) {
	got := exec(t, nil, isolated(t)...)

	if got.err != nil {
		t.Fatalf("Execute: %v", got.err)
	}
	if !strings.Contains(got.stdout, "Usage:") {
		t.Errorf("stdout = %q, want the help output", got.stdout)
	}
	if !strings.Contains(got.stdout, "version") {
		t.Errorf("stdout = %q, want the command list", got.stdout)
	}
}

func TestHelpCommandAndHelpFlagAgree(t *testing.T) {
	viaCommand := exec(t, nil, append(isolated(t), "help", "version")...)
	viaFlag := exec(t, nil, append(isolated(t), "version", "--help")...)

	if viaCommand.err != nil || viaFlag.err != nil {
		t.Fatalf("Execute: %v / %v", viaCommand.err, viaFlag.err)
	}
	if viaCommand.stdout != viaFlag.stdout {
		t.Errorf("help output differs:\n--- help version ---\n%s\n--- version --help ---\n%s",
			viaCommand.stdout, viaFlag.stdout)
	}
	if !strings.Contains(viaFlag.stdout, "Examples:") {
		t.Errorf("stdout = %q, want the examples section", viaFlag.stdout)
	}
}

func TestUnknownCommandIsAUsageError(t *testing.T) {
	got := exec(t, nil, append(isolated(t), "frobnicate")...)

	if got.err == nil {
		t.Fatal("Execute accepted an unknown command")
	}
	if code := errors.ExitCode(got.err); code != errors.ExitUsage {
		t.Errorf("exit code = %d, want %d", code, errors.ExitUsage)
	}
	if !strings.Contains(got.stderr, "unknown command") {
		t.Errorf("stderr = %q, want the failure explained", got.stderr)
	}
	if got.stdout != "" {
		t.Errorf("stdout = %q, want it empty on failure", got.stdout)
	}
}

func TestUnknownFlagIsAUsageError(t *testing.T) {
	got := exec(t, nil, append(isolated(t), "version", "--nonsense")...)

	if got.err == nil {
		t.Fatal("Execute accepted an undefined flag")
	}
	if code := errors.ExitCode(got.err); code != errors.ExitUsage {
		t.Errorf("exit code = %d, want %d", code, errors.ExitUsage)
	}
}

func TestUnexpectedArgumentIsAUsageError(t *testing.T) {
	got := exec(t, nil, append(isolated(t), "version", "extra")...)

	if got.err == nil {
		t.Fatal("Execute accepted an unexpected argument")
	}
	if code := errors.ExitCode(got.err); code != errors.ExitUsage {
		t.Errorf("exit code = %d, want %d", code, errors.ExitUsage)
	}
}

// A file the user named with --config must exist. Silently falling back to
// defaults would produce results they did not ask for.
func TestMissingExplicitConfigIsNotFound(t *testing.T) {
	got := exec(t, nil, "--config", filepath.Join(t.TempDir(), "absent.toml"), "version")

	if got.err == nil {
		t.Fatal("Execute accepted a --config path that does not exist")
	}
	if code := errors.ExitCode(got.err); code != errors.ExitNotFound {
		t.Errorf("exit code = %d, want %d", code, errors.ExitNotFound)
	}
}

func TestUnsupportedOutputFormatIsRejected(t *testing.T) {
	got := exec(t, nil, append(isolated(t), "version", "--output", "pdf")...)

	if got.err == nil {
		t.Fatal("Execute accepted an unsupported output format")
	}
	if !strings.Contains(got.stderr, "table, json, csv") {
		t.Errorf("stderr = %q, want the supported formats listed", got.stderr)
	}
}

// A command whose result is not rows says so rather than inventing a shape
// somebody would then build a script on.
func TestCSVIsRefusedByACommandWithoutRows(t *testing.T) {
	got := exec(t, nil, append(isolated(t), "version", "--output", "csv")...)

	if got.err == nil {
		t.Fatal("a result with no row view was rendered as csv")
	}
	if !strings.Contains(got.stderr, "no csv output") {
		t.Errorf("stderr = %q, want it to say the command has no csv form", got.stderr)
	}
}

func TestEnvironmentSelectsTheOutputFormat(t *testing.T) {
	got := exec(t, map[string]string{"DEVNEST_GENERAL_OUTPUT": "json"},
		append(isolated(t), "version")...)

	if got.err != nil {
		t.Fatalf("Execute: %v", got.err)
	}
	if !strings.HasPrefix(strings.TrimSpace(got.stdout), "{") {
		t.Errorf("stdout = %q, want JSON selected by the environment", got.stdout)
	}
}

func TestFlagsOverrideTheEnvironment(t *testing.T) {
	got := exec(t, map[string]string{"DEVNEST_GENERAL_OUTPUT": "json"},
		append(isolated(t), "version", "--output", "table")...)

	if got.err != nil {
		t.Fatalf("Execute: %v", got.err)
	}
	if strings.HasPrefix(strings.TrimSpace(got.stdout), "{") {
		t.Errorf("stdout = %q, want the flag to win over the environment", got.stdout)
	}
}

func TestStdoutCarriesNoLogOutput(t *testing.T) {
	for _, verbosity := range []string{"--quiet", "--verbose", ""} {
		args := isolated(t)
		if verbosity != "" {
			args = append(args, verbosity)
		}
		got := exec(t, nil, append(args, "version", "--output", "json")...)

		if got.err != nil {
			t.Fatalf("Execute(%q): %v", verbosity, got.err)
		}
		var envelope map[string]any
		if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
			t.Errorf("stdout at %q is not pure JSON: %v\n%s", verbosity, err, got.stdout)
		}
	}
}

func TestConfigWarningsGoToStderrNotStdout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[general]\nfuture_key = 1\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got := exec(t, nil, "--config", path, "version", "--output", "json")
	if got.err != nil {
		t.Fatalf("Execute: %v", got.err)
	}

	if !strings.Contains(got.stderr, "unknown configuration key") {
		t.Errorf("stderr = %q, want the warning", got.stderr)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Errorf("stdout is not pure JSON: %v\n%s", err, got.stdout)
	}
}

func TestErrorInJSONModeIsStillValidJSON(t *testing.T) {
	got := exec(t, nil, append(isolated(t), "frobnicate", "--output", "json")...)

	if got.err == nil {
		t.Fatal("Execute accepted an unknown command")
	}

	var envelope struct {
		Status string `json:"status"`
		Error  struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Hint    string `json:"hint"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, got.stdout)
	}
	if envelope.Status != "error" {
		t.Errorf("status = %q, want \"error\"", envelope.Status)
	}
	if envelope.Error.Code != string(errors.CodeInvalidInput) {
		t.Errorf("code = %q, want %q", envelope.Error.Code, errors.CodeInvalidInput)
	}
	if envelope.Error.Hint == "" {
		t.Error("hint is empty; a usage error should name the next action")
	}
}

func TestVersionFlagOnRootRunsTheVersionCommand(t *testing.T) {
	got := exec(t, nil, append(isolated(t), "--version")...)

	if got.err != nil {
		t.Fatalf("Execute: %v", got.err)
	}
	if !strings.Contains(got.stdout, "platform") {
		t.Errorf("stdout = %q, want the version listing", got.stdout)
	}
}
