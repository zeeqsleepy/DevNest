package cli

import (
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/core/doctor"
)

func doctorResult(checks ...doctor.Check) doctor.Result {
	result := doctor.Result{
		Version:   "1.0.0",
		Commit:    "abc1234",
		Platform:  "linux/amd64",
		GoVersion: "go1.24.0",
		Checks:    checks,
	}
	for _, check := range checks {
		switch check.Status {
		case doctor.StatusFailed:
			result.Failed++
		case doctor.StatusWarning:
			result.Warned++
		}
	}
	result.Healthy = result.Failed == 0
	return result
}

func TestDoctorTextSaysWhatIsWrongAndWhatToDo(t *testing.T) {
	result := doctorResult(
		doctor.Check{Name: "configuration", Status: doctor.StatusFailed,
			Detail: "~/.config/devnest/config.toml: line 4: unexpected token",
			Hint:   "fix the file, or move it aside to fall back to the defaults"},
		doctor.Check{Name: "rule sets", Status: doctor.StatusOK, Detail: "secret 16, clean 21"},
	)

	got := render(t, doctorText(result))
	for _, want := range []string{
		"failed", "unexpected token", "configuration: fix the file", "1 failed", "needs fixing",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// A warning is not a problem to fix, and the summary has to say so or every
// machine without git looks broken.
func TestDoctorTextSeparatesWarningsFromFailures(t *testing.T) {
	result := doctorResult(
		doctor.Check{Name: "git", Status: doctor.StatusWarning, Detail: "not found on PATH"},
		doctor.Check{Name: "rule sets", Status: doctor.StatusOK, Detail: "secret 16, clean 21"},
	)

	got := render(t, doctorText(result))
	if !strings.Contains(got, "nothing failed") {
		t.Errorf("output = %q, want it to say nothing failed", got)
	}
	if strings.Contains(got, "needs fixing") {
		t.Errorf("a warning is presented as a failure:\n%s", got)
	}
}

func TestDoctorTextOnAHealthyInstallation(t *testing.T) {
	result := doctorResult(
		doctor.Check{Name: "rule sets", Status: doctor.StatusOK, Detail: "secret 16, clean 21"},
	)

	got := render(t, doctorText(result))
	for _, want := range []string{"1.0.0", "linux/amd64", "in working order"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// The hint on a passing check is noise. It exists to explain a warning.
func TestDoctorTextKeepsHintsForTheChecksThatNeedThem(t *testing.T) {
	result := doctorResult(
		doctor.Check{Name: "output", Status: doctor.StatusOK, Detail: "format table", Hint: "unused"},
	)

	if got := render(t, doctorText(result)); strings.Contains(got, "unused") {
		t.Errorf("a hint was printed for a passing check:\n%s", got)
	}
}
