package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/core/secret"
)

func finding(rule, severity, path string, line int) secret.Finding {
	return secret.Finding{
		Rule:        rule,
		Description: "a description",
		Severity:    severity,
		Path:        path,
		Line:        line,
		Column:      12,
		Redacted:    "AKIA…(20 chars)",
		Entropy:     3.68,
	}
}

// The text view has to say what the result is: candidates, not confirmed
// secrets. A tool that presents guesses as facts gets ignored after the second
// false positive.
func TestSecretScanTextCallsThemCandidates(t *testing.T) {
	result := secret.ScanResult{
		Root:         "/project",
		Findings:     []secret.Finding{finding("aws-access-key-id", secret.SeverityHigh, "config.yml", 3)},
		Count:        1,
		BySeverity:   map[string]int{secret.SeverityHigh: 1},
		FilesScanned: 240,
		RulesUsed:    16,
	}

	got := render(t, secretScanText(result))
	for _, want := range []string{"aws-access-key-id", "config.yml", "candidates, not confirmed"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// A clean result has to say how much was looked at. "Nothing found" over four
// files is a different claim from the same words over four thousand.
func TestSecretScanTextSaysHowMuchItLookedAt(t *testing.T) {
	result := secret.ScanResult{
		Root:         "/project",
		BySeverity:   map[string]int{},
		FilesScanned: 240,
		FilesSkipped: 12,
		RulesUsed:    16,
		Suppressed:   2,
	}

	got := render(t, secretScanText(result))
	for _, want := range []string{"No candidates", "240", "12 skipped", "suppressed"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// The rendered output must never contain a credential, and the only thing it
// is given is already redacted. This test guards the join between the two.
func TestSecretTextPrintsOnlyTheRedactedForm(t *testing.T) {
	const credential = "AKIA" + "IOSFODNN7EXAMPLE" // devnest:allow-secret

	result := secret.ScanResult{
		Findings:   []secret.Finding{finding("aws-access-key-id", secret.SeverityHigh, "a.yml", 1)},
		Count:      1,
		BySeverity: map[string]int{secret.SeverityHigh: 1},
	}

	if got := render(t, secretScanText(result)); strings.Contains(got, credential) {
		t.Error("the rendered output contains a credential in full")
	}
}

func TestSeveritySummaryOrdersWorstFirst(t *testing.T) {
	counts := map[string]int{
		secret.SeverityLow:      4,
		secret.SeverityCritical: 1,
		secret.SeverityMedium:   2,
	}

	got := severitySummary(counts)
	if got != "1 critical, 2 medium, 4 low" {
		t.Errorf("summary = %q, want the worst first and the empty level left out", got)
	}
	if severitySummary(map[string]int{}) != "none" {
		t.Errorf("summary of nothing = %q, want \"none\"", severitySummary(map[string]int{}))
	}
}

// A finding in history is a different problem from one in the tree, and the
// output has to say what the fix actually is.
func TestSecretHistoryTextSaysRotationIsTheFix(t *testing.T) {
	result := secret.HistoryResult{
		Findings: []secret.HistoryFinding{{
			Finding: finding("github-token", secret.SeverityCritical, "deploy.sh", 4),
			Commit:  "abc123def456",
			Author:  "Ana",
			Date:    time.Date(2026, time.March, 1, 9, 0, 0, 0, time.UTC),
		}},
		Count:      1,
		Commits:    120,
		BySeverity: map[string]int{secret.SeverityCritical: 1},
	}

	got := render(t, secretHistoryText(result))
	for _, want := range []string{"github-token", "abc123de", "Rotating it is the fix"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestSecretHistoryTextHandlesACleanHistory(t *testing.T) {
	got := render(t, secretHistoryText(secret.HistoryResult{Commits: 500}))

	if !strings.Contains(got, "No candidates") || !strings.Contains(got, "500") {
		t.Errorf("output = %q, want a sentence naming how many commits were read", got)
	}
}

func TestSecretTestTextReportsTheScoreWithoutTheValue(t *testing.T) {
	result := secret.InspectResult{
		Matched: true,
		Findings: []secret.Finding{
			finding("aws-access-key-id", secret.SeverityHigh, "", 0),
		},
		Entropy:   3.68,
		Length:    20,
		Redacted:  "AKIA…(20 chars)",
		RulesUsed: 16,
	}

	got := render(t, secretTestText(result))
	for _, want := range []string{"AKIA…(20 chars)", "3.68", "aws-access-key-id (high)"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestSecretTestTextSaysWhenNothingMatched(t *testing.T) {
	got := render(t, secretTestText(secret.InspectResult{
		Entropy: 2.1, Length: 14, Redacted: "just…(14 chars)", RulesUsed: 16,
	}))

	if !strings.Contains(got, "matched") || !strings.Contains(got, "nothing") {
		t.Errorf("output = %q, want it to say nothing matched", got)
	}
}

func TestSecretRulesTextExplainsTheFloors(t *testing.T) {
	got := render(t, secretRulesText(secret.Rules()))

	if !strings.Contains(got, "aws-access-key-id") {
		t.Errorf("output = %q, want the rules listed", got)
	}
	if !strings.Contains(got, "entropy floor") {
		t.Errorf("output = %q, want the floor column", got)
	}
}

func TestSecretPathDefaultsToHere(t *testing.T) {
	path, err := secretPath(nil)
	if err != nil || path != "." {
		t.Errorf("secretPath(nil) = %q, %v, want the current directory", path, err)
	}

	if _, err := secretPath([]string{"a", "b"}); err == nil {
		t.Error("two directories were accepted")
	}
}
