package secret

import (
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

// baselineFor scans a tree and records everything in it, which is what a
// project adopting the scanner does once.
func baselineFor(t *testing.T, system *fakeFS) Baseline {
	t.Helper()
	return NewBaseline(scan(t, system, ScanRequest{}))
}

func TestBaselineHidesWhatItAcceptedAndCountsIt(t *testing.T) {
	system := newFakeFS().with("config/settings.toml", "key = \""+awsKeyID+"\"\n")

	result := scan(t, system, ScanRequest{Baseline: baselineFor(t, system)})

	if result.Count != 0 || len(result.Findings) != 0 {
		t.Fatalf("result = %+v, want the accepted finding gone", result)
	}
	if result.Baselined != 1 {
		t.Errorf("Baselined = %d, want 1", result.Baselined)
	}
	// The gate reads BySeverity, so the filtering has to happen before the
	// counting or a baselined finding still fails a build.
	if len(result.BySeverity) != 0 {
		t.Errorf("BySeverity = %v, want it empty", result.BySeverity)
	}
}

// A finding that moved down a file is the same finding. A baseline keyed on
// line numbers would forget it the first time somebody added an import.
func TestBaselineSurvivesTheFindingMovingLine(t *testing.T) {
	before := newFakeFS().with("config/settings.toml", "key = \""+awsKeyID+"\"\n")
	after := newFakeFS().with("config/settings.toml",
		"# a comment\n# and another\nkey = \""+awsKeyID+"\"\n")

	result := scan(t, after, ScanRequest{Baseline: baselineFor(t, before)})

	if result.Count != 0 {
		t.Errorf("the finding reappeared after moving two lines down: %+v", result.Findings)
	}
}

func TestBaselineLetsNewFindingsThrough(t *testing.T) {
	before := newFakeFS().with("config/settings.toml", "key = \""+awsKeyID+"\"\n")
	after := newFakeFS().
		with("config/settings.toml", "key = \""+awsKeyID+"\"\n").
		with("config/new.env", "token = \""+githubToken+"\"\n")

	result := scan(t, after, ScanRequest{Baseline: baselineFor(t, before)})

	// A token line trips more than one rule, so the claim is about which file
	// is reported rather than how many rules had an opinion about it.
	if result.Count == 0 {
		t.Fatal("the new credential was not reported")
	}
	for _, finding := range result.Findings {
		if finding.Path != "config/new.env" {
			t.Errorf("reported %s in %s, which the baseline accepted",
				finding.Rule, finding.Path)
		}
	}
	if result.Baselined != 1 {
		t.Errorf("Baselined = %d, want the old finding still accepted", result.Baselined)
	}
}

// An entry that matches nothing is either a credential that was dealt with or
// a file that moved. Saying so is what stops a baseline rotting into a list
// nobody prunes.
func TestBaselineReportsEntriesThatMatchNothing(t *testing.T) {
	before := newFakeFS().
		with("config/settings.toml", "key = \""+awsKeyID+"\"\n").
		with("config/old.env", "key = \""+awsKeyID+"\"\n")
	after := newFakeFS().with("config/settings.toml", "key = \""+awsKeyID+"\"\n")

	result := scan(t, after, ScanRequest{Baseline: baselineFor(t, before)})

	if result.BaselineStale != 1 {
		t.Errorf("BaselineStale = %d, want 1", result.BaselineStale)
	}
	if result.Baselined != 1 {
		t.Errorf("Baselined = %d, want 1", result.Baselined)
	}
}

func TestNewBaselineDeduplicatesAndSorts(t *testing.T) {
	system := newFakeFS().
		with("z.env", "key = \""+awsKeyID+"\"\n").
		with("a.env", "one = \""+awsKeyID+"\"\ntwo = \""+awsKeyID+"\"\n")

	baseline := NewBaseline(scan(t, system, ScanRequest{}))

	// Three findings, two entries: the same value under the same rule in one
	// file is one accepted thing however often it appears.
	if len(baseline.Entries) != 2 {
		t.Fatalf("entries = %+v, want two", baseline.Entries)
	}
	if baseline.Entries[0].Path != "a.env" || baseline.Entries[1].Path != "z.env" {
		t.Errorf("entries are not sorted by path: %+v", baseline.Entries)
	}
}

// The file is committed, so it must never carry the value itself.
func TestBaselineCarriesOnlyTheRedactedExcerpt(t *testing.T) {
	system := newFakeFS().with("config/settings.toml", "key = \""+awsKeyID+"\"\n")

	for _, entry := range NewBaseline(scan(t, system, ScanRequest{})).Entries {
		if entry.Redacted == awsKeyID {
			t.Fatal("the baseline holds the credential in full")
		}
		if entry.Redacted == "" {
			t.Error("the baseline entry has no excerpt to match on")
		}
	}
}

func TestParseBaselineRoundTrips(t *testing.T) {
	system := newFakeFS().with("config/settings.toml", "key = \""+awsKeyID+"\"\n")
	written := NewBaseline(scan(t, system, ScanRequest{}))

	parsed, err := ParseBaseline([]byte(
		`{"entries":[{"path":"config/settings.toml","rule":"` + written.Entries[0].Rule +
			`","redacted":"` + written.Entries[0].Redacted + `"}]}`))
	if err != nil {
		t.Fatalf("ParseBaseline: %v", err)
	}

	result := scan(t, system, ScanRequest{Baseline: parsed})
	if result.Count != 0 {
		t.Errorf("a parsed baseline did not match what wrote it: %+v", result.Findings)
	}
}

func TestParseBaselineRejectsUnusableFiles(t *testing.T) {
	tests := map[string]string{
		"not json":     "entries: []",
		"no path":      `{"entries":[{"rule":"aws-access-key-id","redacted":"AKIA…(20 chars)"}]}`,
		"no rule":      `{"entries":[{"path":"a.env","redacted":"AKIA…(20 chars)"}]}`,
		"no excerpt":   `{"entries":[{"path":"a.env","rule":"aws-access-key-id"}]}`,
		"empty string": "",
	}

	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseBaseline([]byte(contents)); errors.CodeOf(err) != errors.CodeParse {
				t.Errorf("code = %q, want %q", errors.CodeOf(err), errors.CodeParse)
			}
		})
	}
}

// An empty baseline accepts nothing, which is what the zero value has to mean
// for every caller that does not ask for one.
func TestEmptyBaselineChangesNothing(t *testing.T) {
	system := newFakeFS().with("config/settings.toml", "key = \""+awsKeyID+"\"\n")

	result := scan(t, system, ScanRequest{Baseline: Baseline{}})

	if result.Count != 1 || result.Baselined != 0 || result.BaselineStale != 0 {
		t.Errorf("result = %+v, want the finding reported and no baseline counters", result)
	}
}
