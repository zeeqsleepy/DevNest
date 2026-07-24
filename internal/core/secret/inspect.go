package secret

import (
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// InspectRequest asks whether one string would be reported.
type InspectRequest struct {
	Value string
	Rules []string
}

// InspectResult is the answer.
//
// Like every other result here it carries no copy of the input. Somebody
// tuning a rule set is testing values that are real credentials as often as
// not, and a command that echoed them into a terminal, a shell history, or a
// JSON file would be the leak it exists to prevent.
type InspectResult struct {
	Matched  bool      `json:"matched"`
	Findings []Finding `json:"findings"`
	// Entropy of the value as a whole, which is the number somebody tuning a
	// threshold is looking for.
	Entropy float64 `json:"entropy"`
	Length  int     `json:"length"`
	// Redacted is the value in the same form a finding reports it, so what is
	// on screen while tuning matches what a report would say.
	Redacted  string `json:"redacted"`
	RulesUsed int    `json:"rulesUsed"`
}

// Inspect reports whether a string would be flagged, and by which rules.
//
// This is the command for tuning: paste the value that was or was not caught,
// see which rules fire and what it scored, then adjust a threshold or add an
// exclusion. It reads nothing and runs nothing.
func Inspect(request InspectRequest) (InspectResult, error) {
	value := strings.TrimSpace(request.Value)
	if value == "" {
		return InspectResult{}, errors.New(errors.CodeInvalidInput, "no value was given").
			WithHint("pass the string to test, or use --stdin to keep it out of " +
				"your shell history")
	}

	active, missing := selected(request.Rules)
	if len(missing) > 0 {
		return InspectResult{}, errors.New(errors.CodeInvalidInput,
			"no rule named %s", strings.Join(missing, ", ")).
			WithHint("run \"devnest secret rules\" to see the names")
	}

	findings := matchLine(value, active, 0)

	return InspectResult{
		Matched:   len(findings) > 0,
		Findings:  findings,
		Entropy:   entropy(value),
		Length:    len([]rune(value)),
		Redacted:  redact(value),
		RulesUsed: len(active),
	}, nil
}

// Threshold validates a --fail-on value and reports whether findings meet it.
//
// It is exported because the interface layer decides the exit code and the
// module decides what the severities mean, and neither should guess about the
// other.
func Threshold(value string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return "", nil
	}
	if !validSeverity(trimmed) {
		return "", errors.New(errors.CodeInvalidInput, "%q is not a severity", value).
			WithHint("choose one of: low, medium, high, critical")
	}
	return trimmed, nil
}

// MeetsThreshold reports whether any finding is at or above a severity.
func MeetsThreshold(counts map[string]int, threshold string) bool {
	if threshold == "" {
		return false
	}

	for severity, count := range counts {
		if count > 0 && atLeast(severity, threshold) {
			return true
		}
	}
	return false
}
