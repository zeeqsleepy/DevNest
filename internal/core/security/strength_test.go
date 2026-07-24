package security

import (
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func findingCodes(result StrengthResult) map[string]bool {
	codes := make(map[string]bool, len(result.Findings))
	for _, finding := range result.Findings {
		codes[finding.Code] = true
	}
	return codes
}

func check(t *testing.T, password string) StrengthResult {
	t.Helper()
	result, err := CheckStrength(password)
	if err != nil {
		t.Fatalf("CheckStrength(%q): %v", password, err)
	}
	return result
}

// leakCorpus is deliberately varied: common words, keyboard walks, sequences,
// repeats, and a long random one.
var leakCorpus = []string{
	"Password123!",
	"correcthorsebatterystaple",
	"aaaaaaaa",
	"qwertyuiop",
	"1234567890",
	"MySecretC0mpanyP@ssword",
	"Kx9$Tm2#pQ7vRn4!Wj8&",
	"letmein!!",
	"1987",
	// Surrounded by whitespace, but with a body that cannot coincide with the
	// English in a static finding message: the point of the test is leakage,
	// not vocabulary.
	"  Zq7!Rp3@  ",
}

// The single most important property of this command: nothing it returns can
// leak what was typed. Results get rendered, exported, and pasted into tickets.
//
// The invariant that actually guarantees it is that every message is a fixed
// string chosen by its code, never assembled from the input. Testing for
// substrings of the password in the output cannot express that: a static
// message about common passwords legitimately contains the word "password",
// and would fail such a check for a user whose password is "Password123!".
//
// So this asserts the real thing: a given finding code always produces
// byte-identical text, whatever password produced it.
func TestCheckStrengthFindingsNeverVaryWithTheInput(t *testing.T) {
	type text struct {
		message    string
		suggestion string
		from       string
	}
	seen := make(map[string]text)

	for _, password := range leakCorpus {
		result := check(t, password)

		for _, finding := range result.Findings {
			previous, known := seen[finding.Code]
			if !known {
				seen[finding.Code] = text{finding.Message, finding.Suggestion, password}
				continue
			}
			if previous.message != finding.Message || previous.suggestion != finding.Suggestion {
				t.Errorf("finding %s differs between inputs:\n  from %q: %q / %q\n  from %q: %q / %q",
					finding.Code,
					previous.from, previous.message, previous.suggestion,
					password, finding.Message, finding.Suggestion)
			}
		}
	}

	if len(seen) < 6 {
		t.Errorf("only %d finding codes were exercised; the corpus is too narrow "+
			"to prove much", len(seen))
	}
}

// The serialised result is what gets exported and attached to tickets, so the
// check is run against that rather than against selected fields.
func TestCheckStrengthResultDoesNotContainThePassword(t *testing.T) {
	for _, password := range leakCorpus {
		t.Run(password, func(t *testing.T) {
			result := check(t, password)

			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			trimmed := strings.TrimSpace(password)
			if len(trimmed) >= 4 && strings.Contains(string(encoded), trimmed) {
				t.Fatalf("the serialised result contains the password:\n%s", encoded)
			}
		})
	}
}

// A weak password must not be described by quoting the weak part back.
func TestCheckStrengthDoesNotQuoteTheWeakFragment(t *testing.T) {
	result := check(t, "Kx9$qwertyTm2#")

	for _, finding := range result.Findings {
		combined := strings.ToLower(finding.Message + " " + finding.Suggestion)
		if strings.Contains(combined, "qwerty") {
			t.Errorf("finding %s quotes the fragment it found: %q", finding.Code, finding.Message)
		}
	}
}

func TestCheckStrengthRatesAGeneratedPasswordWell(t *testing.T) {
	generated, err := GeneratePassword(rand.Reader, PasswordRequest{
		Length: 24, Count: 1,
		Lowercase: true, Uppercase: true, Digits: true, Symbols: true,
		RequireEach: true,
	})
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}

	result := check(t, generated.Passwords[0])
	if !result.Strong {
		t.Errorf("a generated 24-character password scored %d (%s) with findings %v",
			result.Score, result.Rating, result.Findings)
	}
}

func TestCheckStrengthFindsShortPasswords(t *testing.T) {
	short := check(t, "Ab3$xY")
	if !findingCodes(short)[FindingTooShort] {
		t.Errorf("findings = %v, want TOO_SHORT", findingCodes(short))
	}

	medium := check(t, "Ab3$xY9pQ2")
	if !findingCodes(medium)[FindingShort] {
		t.Errorf("findings = %v, want SHORT", findingCodes(medium))
	}
}

func TestCheckStrengthFindsLimitedVariety(t *testing.T) {
	single := check(t, "abcdefghijklmnop")
	if !findingCodes(single)[FindingSingleClass] {
		t.Errorf("findings = %v, want SINGLE_CLASS", findingCodes(single))
	}

	// Two classes, and deliberately not a sequence or a dictionary word.
	two := check(t, "kwfjRtpVzmXqbnLd")
	if !findingCodes(two)[FindingFewClasses] {
		t.Errorf("findings = %v, want FEW_CLASSES", findingCodes(two))
	}
}

func TestCheckStrengthFindsRepeatedRuns(t *testing.T) {
	result := check(t, "Kx9$aaaaTm2#pQ7v")
	if !findingCodes(result)[FindingRepeatedRun] {
		t.Errorf("findings = %v, want REPEATED_RUN", findingCodes(result))
	}
}

// "abcabcabc" has the search space of "abc".
func TestCheckStrengthFindsRepeatedBlocks(t *testing.T) {
	for _, password := range []string{"abcabcabcabc", "Xy7!Xy7!Xy7!"} {
		result := check(t, password)
		if !findingCodes(result)[FindingRepeatedBlock] {
			t.Errorf("%q: findings = %v, want REPEATED_BLOCK", password, findingCodes(result))
		}
	}
}

func TestCheckStrengthFindsSequences(t *testing.T) {
	for _, password := range []string{"Kx9$abcdeTm2#", "Zq7!98765Rp3@"} {
		result := check(t, password)
		if !findingCodes(result)[FindingSequence] {
			t.Errorf("%q: findings = %v, want SEQUENCE", password, findingCodes(result))
		}
	}
}

func TestCheckStrengthFindsKeyboardWalks(t *testing.T) {
	for _, password := range []string{"Kx9$qwertyTm2#", "Zq7!asdfghRp3@"} {
		result := check(t, password)
		if !findingCodes(result)[FindingKeyboard] {
			t.Errorf("%q: findings = %v, want KEYBOARD_PATTERN", password, findingCodes(result))
		}
	}
}

func TestCheckStrengthFindsCommonPasswords(t *testing.T) {
	for _, password := range []string{"password", "qwerty", "letmein", "PASSWORD", "Monkey"} {
		result := check(t, password)
		if !findingCodes(result)[FindingCommon] {
			t.Errorf("%q: findings = %v, want COMMON_PASSWORD", password, findingCodes(result))
		}
		if result.Strong {
			t.Errorf("%q was rated strong", password)
		}
	}
}

// A checker that passes "Password1!" is worse than none: it tells the user
// they have solved a problem they still have.
func TestCheckStrengthSeesThroughDecoratedCommonPasswords(t *testing.T) {
	for _, password := range []string{"password1", "Password123!", "letmein!!", "monkey2024"} {
		result := check(t, password)
		codes := findingCodes(result)

		if !codes[FindingCommon] && !codes[FindingCommonBase] {
			t.Errorf("%q: findings = %v, want a dictionary finding", password, codes)
		}
		// Not merely "not strong": a known base is found in milliseconds, and
		// anything above weak would tell the user they have solved a problem
		// they still have.
		if result.Score > 25 {
			t.Errorf("%q scored %d (%s); a known base must not score respectably",
				password, result.Score, result.Rating)
		}
	}
}

// Character arithmetic must not be able to rescue a password built on a word
// from every guessing list, however much decoration surrounds it.
func TestCheckStrengthCapsDictionaryMatchesRegardlessOfLength(t *testing.T) {
	long := check(t, "Password123!$%^&*()_+{}|:<>?")

	if long.EntropyBits < 100 {
		t.Fatalf("test premise failed: entropy = %v, expected a large figure", long.EntropyBits)
	}
	if long.Score > 25 {
		t.Errorf("a long decorated common password scored %d despite %v bits of "+
			"nominal entropy", long.Score, long.EntropyBits)
	}
}

func TestCheckStrengthCapsExactCommonPasswords(t *testing.T) {
	result := check(t, "qwertyuiop")
	if result.Score > 10 {
		t.Errorf("a password straight off the list scored %d", result.Score)
	}
}

func TestCheckStrengthFindsDigitOnlyPasswords(t *testing.T) {
	result := check(t, "5837194620")
	if !findingCodes(result)[FindingDigitsOnly] {
		t.Errorf("findings = %v, want DIGITS_ONLY", findingCodes(result))
	}
}

func TestCheckStrengthFindsYears(t *testing.T) {
	result := check(t, "1987")
	codes := findingCodes(result)
	if !codes[FindingLooksLikeYear] {
		t.Errorf("findings = %v, want LOOKS_LIKE_YEAR", codes)
	}
	if result.Score > 20 {
		t.Errorf("a four-digit year scored %d", result.Score)
	}
}

func TestCheckStrengthFindsSurroundingWhitespace(t *testing.T) {
	result := check(t, " Kx9$Tm2#pQ7vRn4! ")
	if !findingCodes(result)[FindingWhitespace] {
		t.Errorf("findings = %v, want SURROUNDING_WHITESPACE", findingCodes(result))
	}
}

func TestCheckStrengthScoreIsBounded(t *testing.T) {
	passwords := []string{
		"a", "password", "1234", "aaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"Kx9$Tm2#pQ7vRn4!Wj8&Zc1%Yb6^",
		strings.Repeat("Xq7!", 40),
	}

	for _, password := range passwords {
		result := check(t, password)
		if result.Score < 0 || result.Score > 100 {
			t.Errorf("%q scored %d, outside 0 to 100", password, result.Score)
		}
		if result.Rating == "" {
			t.Errorf("%q has no rating", password)
		}
	}
}

func TestCheckStrengthOrdersRatings(t *testing.T) {
	weak := check(t, "password")
	fair := check(t, "kwfjRtpVzmXq")
	strong := check(t, "Kx9$Tm2#pQ7vRn4!Wj8&")

	if weak.Score >= fair.Score {
		t.Errorf("a common password scored %d, not below %d", weak.Score, fair.Score)
	}
	if fair.Score >= strong.Score {
		t.Errorf("a fair password scored %d, not below %d", fair.Score, strong.Score)
	}
}

func TestCheckStrengthRejectsAnEmptyPassword(t *testing.T) {
	_, err := CheckStrength("")
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestCheckStrengthReportsClasses(t *testing.T) {
	result := check(t, "Kx9$Tm2#pQ7v")

	classes := strings.Join(result.Classes, ",")
	for _, want := range []string{ClassLowercase, ClassUppercase, ClassDigits, ClassSymbols} {
		if !strings.Contains(classes, want) {
			t.Errorf("classes = %v, want %q", result.Classes, want)
		}
	}
}

func TestLongestRun(t *testing.T) {
	tests := map[string]int{
		"abc":     1,
		"aab":     2,
		"aaab":    3,
		"abbbbc":  4,
		"a":       1,
		"aaaaaaa": 7,
	}
	for input, want := range tests {
		if got := longestRun([]rune(input)); got != want {
			t.Errorf("longestRun(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestLongestSequence(t *testing.T) {
	tests := map[string]int{
		"abcd":  4,
		"dcba":  4,
		"1234":  4,
		"acbd":  2,
		"xyz":   3,
		"aXbYc": 1,
	}
	for input, want := range tests {
		if got := longestSequence([]rune(input)); got != want {
			t.Errorf("longestSequence(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestRepeatedBlock(t *testing.T) {
	repeated := []string{"abab", "abcabc", "xyxyxyxy", "aaaa"}
	for _, input := range repeated {
		if !repeatedBlock([]rune(input)) {
			t.Errorf("repeatedBlock(%q) = false, want true", input)
		}
	}

	varied := []string{"abcd", "abcabd", "Kx9$Tm2#", "abc"}
	for _, input := range varied {
		if repeatedBlock([]rune(input)) {
			t.Errorf("repeatedBlock(%q) = true, want false", input)
		}
	}
}

func TestStripDecoration(t *testing.T) {
	tests := map[string]string{
		"password1":    "password",
		"password123!": "password",
		"monkey2024":   "monkey",
		"letmein!!":    "letmein",
		"password":     "password",
	}
	for input, want := range tests {
		if got := stripDecoration(input); got != want {
			t.Errorf("stripDecoration(%q) = %q, want %q", input, got, want)
		}
	}
}
