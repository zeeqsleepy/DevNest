package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func TestResolveForExportNamesWhatIsWrong(t *testing.T) {
	if _, err := resolveForExport("version"); err != nil {
		t.Errorf("a runnable command was rejected: %v", err)
	}
	if _, err := resolveForExport("secret rules"); err != nil {
		t.Errorf("a subcommand written as one argument was rejected: %v", err)
	}

	// "encode" is a group with no run of its own, so exporting it would have
	// nothing to export.
	_, err := resolveForExport("encode")
	if err == nil {
		t.Fatal("a group was accepted")
	}
	if !strings.Contains(errors.Classify(err).Hint, "encode ") {
		t.Errorf("hint = %q, want it to name a subcommand", errors.Classify(err).Hint)
	}

	if _, err := resolveForExport("nonsense"); err == nil {
		t.Error("an unknown command was accepted")
	}
	if _, err := resolveForExport("export"); err == nil {
		t.Error("export accepted itself")
	}
}

func TestWorseStatusKeepsTheWorst(t *testing.T) {
	cases := []struct{ current, next, want string }{
		{output.StatusOK, output.StatusWarning, output.StatusWarning},
		{output.StatusWarning, output.StatusOK, output.StatusWarning},
		{output.StatusWarning, output.StatusError, output.StatusError},
		{output.StatusError, output.StatusWarning, output.StatusError},
	}

	for _, test := range cases {
		if got := worseStatus(test.current, test.next); got != test.want {
			t.Errorf("worseStatus(%q, %q) = %q, want %q", test.current, test.next, got, test.want)
		}
	}
}

// The combined document holds one section per command, each carrying what that
// command's own result carried.
func TestExportCombinesResults(t *testing.T) {
	got := exec(t, nil, append(isolated(t), "export", "version", "secret rules", "--output", "json")...)
	if got.err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", got.err, got.stderr)
	}

	var envelope struct {
		Data struct {
			Status   string `json:"status"`
			Failed   int    `json:"failed"`
			Commands []struct {
				Command string `json:"command"`
				Status  string `json:"status"`
				Data    any    `json:"data"`
			} `json:"commands"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, got.stdout)
	}

	if len(envelope.Data.Commands) != 2 {
		t.Fatalf("commands = %d, want 2:\n%s", len(envelope.Data.Commands), got.stdout)
	}
	if envelope.Data.Commands[0].Command != "version" ||
		envelope.Data.Commands[1].Command != "secret rules" {
		t.Errorf("the sections are not the commands that were named:\n%s", got.stdout)
	}
	for index, section := range envelope.Data.Commands {
		if section.Status != output.StatusOK || section.Data == nil {
			t.Errorf("section %d carries no result: %+v", index, section)
		}
	}
	if envelope.Data.Failed != 0 || envelope.Data.Status != output.StatusOK {
		t.Errorf("status = %q, failed = %d", envelope.Data.Status, envelope.Data.Failed)
	}
}

// A failure in the middle must not cost the report: the sections after it are
// the reason somebody asked for a combined document.
func TestExportKeepsGoingAfterAFailure(t *testing.T) {
	args := append(isolated(t), "export", "json format", "version", "--output", "json")

	got := exec(t, nil, args...)
	if got.err == nil {
		t.Fatal("a failed section did not affect the exit code")
	}

	var envelope struct {
		Data struct {
			Failed   int `json:"failed"`
			Commands []struct {
				Command string `json:"command"`
				Status  string `json:"status"`
				Error   *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"commands"`
		} `json:"data"`
	}
	// The report comes first and the error report after it, so the first
	// document on stdout is the one being examined here.
	if err := json.NewDecoder(strings.NewReader(got.stdout)).Decode(&envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, got.stdout)
	}

	if len(envelope.Data.Commands) != 2 {
		t.Fatalf("commands = %d, want 2:\n%s", len(envelope.Data.Commands), got.stdout)
	}
	if envelope.Data.Commands[0].Status != output.StatusError || envelope.Data.Commands[0].Error == nil {
		t.Errorf("the failed section does not say what went wrong: %+v", envelope.Data.Commands[0])
	}
	if envelope.Data.Commands[1].Status != output.StatusOK {
		t.Errorf("the section after a failure did not run: %+v", envelope.Data.Commands[1])
	}
	if envelope.Data.Failed != 1 {
		t.Errorf("failed = %d, want 1", envelope.Data.Failed)
	}
}

func TestExportWithoutACommandSaysSo(t *testing.T) {
	got := exec(t, nil, append(isolated(t), "export")...)

	if got.err == nil {
		t.Fatal("export ran with no commands")
	}
	if errors.ExitCode(got.err) != errors.ExitUsage {
		t.Errorf("exit code = %d, want %d", errors.ExitCode(got.err), errors.ExitUsage)
	}
}
