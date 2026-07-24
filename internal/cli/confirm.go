package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

// Confirm asks the user to approve an operation that changes the disk.
//
// Three rules shape this, all from docs/design.md:
//
// The prompt is written to stderr, so a command whose result is being piped
// still asks its question somewhere the user can see it.
//
// Every confirmation has a flag that answers it in advance, so nothing here is
// unscriptable. When --yes was passed, or the user turned confirmation off in
// their configuration, this returns immediately.
//
// When stdin is not a terminal, an unanswered prompt is a hang rather than a
// question. In that case the command fails and the message names the flag to
// pass instead.
func (e *Env) Confirm(question string, assumeYes bool) error {
	if !e.NeedsConfirmation(assumeYes) {
		return nil
	}

	if !output.IsTerminal(e.Stdin) {
		return unanswerable()
	}

	fmt.Fprintf(e.Stderr, "%s [y/N]: ", question)

	reader := bufio.NewReader(e.Stdin)
	answer, err := reader.ReadString('\n')

	// Reaching end of input without a single character means there was nobody
	// to ask after all. On Windows the null device is a character device, so
	// stdin can look interactive while being nothing of the kind; this is
	// where that case actually surfaces, and the user gets the flag to pass
	// rather than a bare "cancelled".
	if err != nil && strings.TrimSpace(answer) == "" {
		return unanswerable()
	}

	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	default:
		return errors.New(errors.CodeCancelled, "cancelled")
	}
}

// NeedsConfirmation reports whether the user will actually be asked. Callers
// use it to skip printing a plan that nobody is going to be shown.
func (e *Env) NeedsConfirmation(assumeYes bool) bool {
	return !assumeYes && e.Config.General.Confirm
}

func unanswerable() error {
	return errors.New(errors.CodeInvalidInput,
		"this command needs confirmation, but there is nothing to answer on").
		WithHint("pass --yes to confirm in advance")
}
