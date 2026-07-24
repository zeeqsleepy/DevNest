package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/config"
	"github.com/devnest/devnest/internal/errors"
)

func confirmEnv(t *testing.T, input string, confirm bool) (*Env, *bytes.Buffer) {
	t.Helper()

	env, _, stderr := newTestEnv(t, "table")
	env.Stdin = strings.NewReader(input)
	env.Config = config.Default()
	env.Config.General.Confirm = confirm
	return env, stderr
}

// Every confirmation has a flag that answers it in advance, so nothing is
// unscriptable.
func TestConfirmSkippedByYes(t *testing.T) {
	env, stderr := confirmEnv(t, "", true)

	if err := env.Confirm("Delete everything?", true); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want no prompt", stderr.String())
	}
}

func TestConfirmSkippedByConfiguration(t *testing.T) {
	env, stderr := confirmEnv(t, "", false)

	if err := env.Confirm("Delete everything?", false); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want no prompt", stderr.String())
	}
}

// A prompt with nothing to answer on is a hang, so it fails instead, and the
// message names the flag to pass.
func TestConfirmWithoutATerminalNamesTheFlag(t *testing.T) {
	env, _ := confirmEnv(t, "y\n", true)

	err := env.Confirm("Move 12 files?", false)
	if err == nil {
		t.Fatal("Confirm succeeded without a terminal")
	}
	if got := errors.CodeOf(err); got != errors.CodeInvalidInput {
		t.Errorf("code = %q, want %q", got, errors.CodeInvalidInput)
	}
	if !strings.Contains(errors.Classify(err).Hint, "--yes") {
		t.Errorf("hint = %q, want it to name --yes", errors.Classify(err).Hint)
	}
}

func TestNeedsConfirmation(t *testing.T) {
	env, _ := confirmEnv(t, "", true)

	if !env.NeedsConfirmation(false) {
		t.Error("NeedsConfirmation(false) = false, want true")
	}
	if env.NeedsConfirmation(true) {
		t.Error("--yes should remove the need to ask")
	}

	env.Config.General.Confirm = false
	if env.NeedsConfirmation(false) {
		t.Error("configuration should be able to turn the prompt off")
	}
}
