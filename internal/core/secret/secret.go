// Package secret is the credential scanner: it finds strings shaped like keys
// and tokens in a working tree or in a repository's history, and reports them
// without ever printing one in full.
//
// # It reports candidates
//
// Every scanner of this kind is judged by its false positives, and a scanner
// people have learned to ignore finds nothing. So this one says what it is:
// the result is a list of candidates, the output says so in as many words, and
// the decision is the reader's. The mitigations are an entropy floor in front
// of every generic rule, path exclusions for the places test data lives, and an
// inline comment that silences one line.
//
// # Nothing is ever printed in full
//
// A finding carries the rule, the file, the line, and a redacted excerpt. The
// excerpt is the first few characters and a length, never the value. This holds
// in the JSON output as well as the table, at every verbosity, because a report
// is exported, attached to a ticket, and read by people who should not be given
// a working credential by the tool that found it.
//
// The redaction happens where the finding is built, not where it is rendered.
// A redaction applied by a renderer is one `--output json` away from being
// bypassed.
package secret

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// suppression silences a line. It is deliberately spelled out rather than
// something short: a marker that appears by accident is a marker that hides a
// real finding.
const suppression = "devnest:allow-secret"

// Finding is one candidate credential.
//
// There is no field holding the matched value, and that is the point: it cannot
// be leaked by a renderer, an export, or a verbose flag, because it is not in
// the structure at all.
type Finding struct {
	Rule        string `json:"rule"`
	Description string `json:"description"`
	Severity    string `json:"severity"`
	Path        string `json:"path"`
	Line        int    `json:"line"`
	// Column is where on the line the match began, so an editor can be sent
	// straight to it.
	Column int `json:"column"`
	// Redacted is the first few characters and the length, in the form
	// "AKIA…(20 chars)". Enough to recognise which credential it is when you
	// are looking at the file; never enough to use.
	Redacted string `json:"redacted"`
	// Entropy is what the value scored, so a reader can judge a generic match
	// rather than take it on faith.
	Entropy float64 `json:"entropy"`
}

// redact turns a matched value into something safe to print.
//
// Four characters and a length. The prefix is what makes a finding
// recognisable: "AKIA" says which of the twelve keys in a config file this is,
// and four characters of a forty-character key is not a meaningful head start
// for anybody.
//
// A short value is not shown at all. Four characters of an eight-character
// value is half of it.
func redact(value string) string {
	const prefix = 4
	const shortest = 12

	length := utf8.RuneCountInString(value)
	if length < shortest {
		return "…(" + itoa(length) + " chars)"
	}

	runes := []rune(value)
	return string(runes[:prefix]) + "…(" + itoa(length) + " chars)"
}

// entropy is Shannon entropy over the bytes of a string, in bits per character.
//
// It is the standard measure and it is used the standard way: as a floor, not
// as a detector. A random forty-character key scores above four; "password" and
// "XXXXXXXXXXXX" score far below, and that is the whole job.
func entropy(value string) float64 {
	if value == "" {
		return 0
	}

	counts := make(map[rune]int, len(value))
	total := 0
	for _, character := range value {
		counts[character]++
		total++
	}

	score := 0.0
	for _, count := range counts {
		probability := float64(count) / float64(total)
		score -= probability * math.Log2(probability)
	}
	return round2(score)
}

// suppressed reports whether a line carries the inline marker, or the line
// above it does.
//
// Both positions are honoured because a comment goes above the line in some
// languages and beside it in others, and a user who put it in the wrong place
// has still clearly stated their intent.
func suppressed(line, previous string) bool {
	return strings.Contains(line, suppression) || strings.Contains(previous, suppression)
}

// round2 keeps entropy to two decimal places, so two runs over one file
// produce byte-identical output.
func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func itoa(value int) string { return strconv.Itoa(value) }
