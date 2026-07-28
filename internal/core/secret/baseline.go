package secret

import (
	"encoding/json"
	"sort"

	"github.com/devnest/devnest/internal/errors"
)

// Baseline is the set of findings a project has already read and decided to
// live with.
//
// It exists so that an old repository can start scanning. A tree with four
// hundred historical candidates in it fails every run, and a check that always
// fails is a check somebody turns off in a week; a baseline draws the line at
// today, so the next credential to arrive is the only thing that shows up.
//
// Accepting is not fixing, and nothing here pretends otherwise: the entries
// stay in the file, in a readable form, under review whenever somebody opens
// it.
type Baseline struct {
	Entries []BaselineEntry `json:"entries"`
}

// BaselineEntry identifies one accepted finding.
//
// The line number is deliberately not part of it. A finding that moved down a
// file because somebody added an import is the same finding, and a baseline
// that forgets what it accepted on every edit is a baseline nobody keeps.
// What stays put is the file, the rule, and the redacted excerpt.
//
// None of it is sensitive. The excerpt is the same four characters and length
// a finding carries, because the scanner never holds the value itself, which
// also means a baseline can be committed without committing a secret.
type BaselineEntry struct {
	Path     string `json:"path"`
	Rule     string `json:"rule"`
	Redacted string `json:"redacted"`
}

func (b BaselineEntry) key() string {
	return b.Path + "\x00" + b.Rule + "\x00" + b.Redacted
}

// NewBaseline records everything a scan found, so the next run starts from
// there.
//
// Duplicates collapse: the same value under the same rule in one file is one
// entry however many times it appears, because the entry says "this is known",
// not "this appears twice".
func NewBaseline(result ScanResult) Baseline {
	seen := make(map[string]bool, len(result.Findings))
	entries := make([]BaselineEntry, 0, len(result.Findings))

	for _, finding := range result.Findings {
		entry := BaselineEntry{
			Path:     finding.Path,
			Rule:     finding.Rule,
			Redacted: finding.Redacted,
		}
		if seen[entry.key()] {
			continue
		}
		seen[entry.key()] = true
		entries = append(entries, entry)
	}

	// Sorted, so that regenerating a baseline over an unchanged tree produces
	// an identical file and a review sees an empty diff.
	sort.Slice(entries, func(first, second int) bool {
		left, right := entries[first], entries[second]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Rule != right.Rule {
			return left.Rule < right.Rule
		}
		return left.Redacted < right.Redacted
	})

	return Baseline{Entries: entries}
}

// ParseBaseline reads a baseline file.
//
// An entry missing a field is refused rather than ignored. A baseline is a
// list of things that will not be reported, so one that is quietly half
// understood is the most dangerous shape this file can take.
func ParseBaseline(data []byte) (Baseline, error) {
	var baseline Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		return Baseline{}, errors.Wrap(err, errors.CodeParse, "the baseline file is not valid JSON").
			WithHint("regenerate it with \"devnest secret scan --baseline <path> --update-baseline\"")
	}

	for index, entry := range baseline.Entries {
		if entry.Path == "" || entry.Rule == "" || entry.Redacted == "" {
			return Baseline{}, errors.New(errors.CodeParse,
				"entry %d of the baseline is missing path, rule, or redacted", index+1).
				WithHint("every entry needs all three; regenerate the file rather than editing it by hand")
		}
	}

	return baseline, nil
}

// filter drops the findings a baseline has already accepted and counts both
// what it hid and what it no longer matches.
func (b Baseline) filter(result *ScanResult) {
	if len(b.Entries) == 0 {
		return
	}

	known := make(map[string]bool, len(b.Entries))
	for _, entry := range b.Entries {
		known[entry.key()] = true
	}

	matched := make(map[string]bool, len(b.Entries))
	kept := make([]Finding, 0, len(result.Findings))

	for _, finding := range result.Findings {
		key := BaselineEntry{
			Path:     finding.Path,
			Rule:     finding.Rule,
			Redacted: finding.Redacted,
		}.key()

		if known[key] {
			matched[key] = true
			result.Baselined++
			continue
		}
		kept = append(kept, finding)
	}

	result.Findings = kept
	// An entry that matched nothing is either a credential that was dealt with
	// or a file that moved. Either way the line is worth reporting: a baseline
	// nobody prunes eventually accepts things that are not there.
	result.BaselineStale = len(known) - len(matched)
}
