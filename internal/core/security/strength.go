package security

import (
	"math"
	"strings"
	"unicode"

	"github.com/devnest/devnest/internal/errors"
)

// Ratings, weakest first.
const (
	RatingVeryWeak   = "very weak"
	RatingWeak       = "weak"
	RatingFair       = "fair"
	RatingStrong     = "strong"
	RatingVeryStrong = "very strong"
)

// Finding codes, so a caller can branch on the kind of weakness without
// matching on message text.
const (
	FindingTooShort      = "TOO_SHORT"
	FindingShort         = "SHORT"
	FindingFewClasses    = "FEW_CLASSES"
	FindingSingleClass   = "SINGLE_CLASS"
	FindingRepeatedRun   = "REPEATED_RUN"
	FindingRepeatedBlock = "REPEATED_BLOCK"
	FindingSequence      = "SEQUENCE"
	FindingKeyboard      = "KEYBOARD_PATTERN"
	FindingCommon        = "COMMON_PASSWORD"
	FindingCommonBase    = "COMMON_BASE"
	FindingDigitsOnly    = "DIGITS_ONLY"
	FindingLooksLikeYear = "LOOKS_LIKE_YEAR"
	FindingWhitespace    = "SURROUNDING_WHITESPACE"
)

// Finding is one thing wrong with a password.
//
// Nothing here quotes the password or any part of it. A finding describes the
// shape of the problem ("four characters in sequence") because that is
// enough to fix it and does not turn a result into a leak.
type Finding struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion"`
	// Penalty is subtracted from the entropy-derived score.
	Penalty int `json:"penalty"`
	// Cap is a ceiling the score cannot exceed while this finding stands.
	// Zero means no ceiling.
	//
	// Some weaknesses are not a matter of degree. A password built on a word
	// from every guessing list is found in milliseconds however many
	// characters surround it, and subtracting points from a large entropy
	// figure still leaves a respectable-looking number. A ceiling says the
	// thing the arithmetic cannot.
	Cap int `json:"scoreCap,omitempty"`
}

// StrengthResult is the verdict on a password.
//
// The password itself is deliberately absent, and so is every substring of it.
type StrengthResult struct {
	Length      int       `json:"length"`
	Score       int       `json:"score"`
	Rating      string    `json:"rating"`
	EntropyBits float64   `json:"entropyBits"`
	Classes     []string  `json:"classes"`
	Findings    []Finding `json:"findings"`
	Strong      bool      `json:"strong"`
}

// CheckStrength judges a password.
//
// The score starts from an entropy estimate and loses points for the patterns
// that make an estimate a lie. "Password123!" has four character classes and
// twelve characters, which looks respectable until you notice it is a word, a
// sequence, and a symbol on the end: the three things every cracking wordlist
// tries first.
//
// The password is never stored, never logged, and never appears in the result.
func CheckStrength(password string) (StrengthResult, error) {
	if password == "" {
		return StrengthResult{}, errors.New(errors.CodeInvalidInput, "no password was given").
			WithHint("pass the password, or use --stdin to read it from a pipe")
	}

	runes := []rune(password)
	classes := classify(password)

	result := StrengthResult{
		Length:   len(runes),
		Classes:  classes,
		Findings: []Finding{},
	}

	alphabet := alphabetSize(password)
	if alphabet > 0 {
		result.EntropyBits = round2(float64(len(runes)) * math.Log2(float64(alphabet)))
	}

	result.Findings = append(result.Findings, lengthFindings(len(runes))...)
	result.Findings = append(result.Findings, classFindings(classes)...)
	result.Findings = append(result.Findings, patternFindings(password, runes)...)
	result.Findings = append(result.Findings, dictionaryFindings(password)...)

	result.Score = score(result.EntropyBits, result.Findings)
	result.Rating = rating(result.Score)
	result.Strong = result.Score >= 60

	return result, nil
}

func lengthFindings(length int) []Finding {
	switch {
	case length < 8:
		return []Finding{{
			Code:       FindingTooShort,
			Message:    "it is shorter than 8 characters",
			Suggestion: "length matters more than anything else; aim for 16 or more",
			Penalty:    40,
		}}
	case length < 12:
		return []Finding{{
			Code:       FindingShort,
			Message:    "it is shorter than 12 characters",
			Suggestion: "add more characters; length buys more than complexity does",
			Penalty:    15,
		}}
	}
	return nil
}

func classFindings(classes []string) []Finding {
	switch len(classes) {
	case 1:
		return []Finding{{
			Code:       FindingSingleClass,
			Message:    "it uses only one kind of character",
			Suggestion: "mix in another kind, or make it considerably longer",
			Penalty:    25,
		}}
	case 2:
		return []Finding{{
			Code:       FindingFewClasses,
			Message:    "it uses only two kinds of character",
			Suggestion: "add digits or symbols, or make it longer",
			Penalty:    10,
		}}
	}
	return nil
}

func patternFindings(password string, runes []rune) []Finding {
	var findings []Finding

	if run := longestRun(runes); run >= 3 {
		findings = append(findings, Finding{
			Code:       FindingRepeatedRun,
			Message:    "it repeats the same character several times in a row",
			Suggestion: "break up the run",
			Penalty:    15,
		})
	}

	if repeatedBlock(runes) {
		findings = append(findings, Finding{
			Code:       FindingRepeatedBlock,
			Message:    "it is a short block repeated to fill the length",
			Suggestion: "a repeated block is as weak as the block alone",
			Penalty:    25,
		})
	}

	if longestSequence(runes) >= 4 {
		findings = append(findings, Finding{
			Code:       FindingSequence,
			Message:    "it contains four or more characters in consecutive order",
			Suggestion: "avoid runs like abcd or 1234; they are tried first",
			Penalty:    20,
		})
	}

	if keyboardRun(password) >= 4 {
		findings = append(findings, Finding{
			Code:       FindingKeyboard,
			Message:    "it contains a run of adjacent keyboard keys",
			Suggestion: "keyboard walks are in every cracking wordlist",
			Penalty:    20,
		})
	}

	if onlyDigits(runes) {
		findings = append(findings, Finding{
			Code:       FindingDigitsOnly,
			Message:    "it is made up entirely of digits",
			Suggestion: "digits alone leave very little to search",
			Penalty:    25,
		})

		if looksLikeYear(password) {
			findings = append(findings, Finding{
				Code:       FindingLooksLikeYear,
				Message:    "it looks like a year or a date",
				Suggestion: "dates are guessed early, especially ones near the present",
				Penalty:    15,
			})
		}
	}

	if strings.TrimSpace(password) != password {
		findings = append(findings, Finding{
			Code:       FindingWhitespace,
			Message:    "it begins or ends with whitespace",
			Suggestion: "leading or trailing spaces are usually a paste mistake",
			Penalty:    5,
		})
	}

	return findings
}

// longestRun returns the length of the longest run of one repeated character.
func longestRun(runes []rune) int {
	longest, current := 1, 1

	for index := 1; index < len(runes); index++ {
		if runes[index] == runes[index-1] {
			current++
			if current > longest {
				longest = current
			}
			continue
		}
		current = 1
	}

	if len(runes) == 0 {
		return 0
	}
	return longest
}

// repeatedBlock reports whether the password is one short block repeated.
//
// "abcabcabc" has the search space of "abc". Checking every block length up to
// half the password catches the cases people actually use.
func repeatedBlock(runes []rune) bool {
	length := len(runes)
	if length < 4 {
		return false
	}

	for size := 1; size <= length/2; size++ {
		if length%size != 0 {
			continue
		}
		if size*2 > length {
			break
		}

		matches := true
		for index := size; index < length && matches; index++ {
			if runes[index] != runes[index%size] {
				matches = false
			}
		}
		if matches {
			return true
		}
	}
	return false
}

// longestSequence returns the longest run of consecutive code points, in
// either direction. This catches abcd, 4321, and wxyz alike.
func longestSequence(runes []rune) int {
	if len(runes) < 2 {
		return 0
	}

	longest, ascending, descending := 1, 1, 1

	for index := 1; index < len(runes); index++ {
		difference := runes[index] - runes[index-1]

		switch difference {
		case 1:
			ascending++
			descending = 1
		case -1:
			descending++
			ascending = 1
		default:
			ascending, descending = 1, 1
		}

		if ascending > longest {
			longest = ascending
		}
		if descending > longest {
			longest = descending
		}
	}
	return longest
}

// keyboardRows are the sequences a finger walks across a QWERTY keyboard.
var keyboardRows = []string{
	"qwertyuiop",
	"asdfghjkl",
	"zxcvbnm",
	"1234567890",
	"!@#$%^&*()",
}

// keyboardRun returns the longest run of adjacent keys the password walks.
func keyboardRun(password string) int {
	lowered := strings.ToLower(password)
	longest := 0

	for _, row := range keyboardRows {
		reversed := reverse(row)
		for _, direction := range []string{row, reversed} {
			for start := 0; start < len(direction); start++ {
				for end := start + 4; end <= len(direction); end++ {
					if strings.Contains(lowered, direction[start:end]) && end-start > longest {
						longest = end - start
					}
				}
			}
		}
	}
	return longest
}

func reverse(value string) string {
	runes := []rune(value)
	for left, right := 0, len(runes)-1; left < right; left, right = left+1, right-1 {
		runes[left], runes[right] = runes[right], runes[left]
	}
	return string(runes)
}

func onlyDigits(runes []rune) bool {
	if len(runes) == 0 {
		return false
	}
	for _, character := range runes {
		if !unicode.IsDigit(character) {
			return false
		}
	}
	return true
}

func looksLikeYear(password string) bool {
	if len(password) != 4 {
		return false
	}
	return password >= "1900" && password <= "2099"
}

// score turns entropy and findings into a number between 0 and 100.
//
// The entropy is a ceiling, not the answer. A password that looks strong by
// character count and loses most of it to a dictionary match should score
// badly, because a cracker will find it in seconds regardless of how many
// combinations exist in theory.
func score(entropy float64, findings []Finding) int {
	base := entropy * 1.2
	if base > 100 {
		base = 100
	}

	ceiling := 100
	for _, finding := range findings {
		base -= float64(finding.Penalty)
		if finding.Cap > 0 && finding.Cap < ceiling {
			ceiling = finding.Cap
		}
	}

	if base > float64(ceiling) {
		base = float64(ceiling)
	}

	switch {
	case base < 0:
		return 0
	case base > 100:
		return 100
	default:
		return int(math.Round(base))
	}
}

func rating(score int) string {
	switch {
	case score >= 85:
		return RatingVeryStrong
	case score >= 60:
		return RatingStrong
	case score >= 40:
		return RatingFair
	case score >= 20:
		return RatingWeak
	default:
		return RatingVeryWeak
	}
}
