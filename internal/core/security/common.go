package security

import (
	"strings"
	"unicode"
)

// commonPasswords is a short list of the passwords that appear at the top of
// every breach corpus.
//
// It is deliberately small. A real cracking wordlist has hundreds of millions
// of entries and belongs in a dedicated tool; embedding one here would add tens
// of megabytes to a binary whose whole appeal is that it is one small file. The
// purpose of this list is to catch the handful of choices that are so common
// they are guessed in the first second, and to make the point to the user that
// a memorable password is a guessed one.
//
// A miss here is not a pass mark. The result says as much.
var commonPasswords = []string{
	"123456", "123456789", "12345678", "12345", "1234567", "1234567890",
	"password", "qwerty", "abc123", "111111", "123123", "admin", "letmein",
	"welcome", "monkey", "dragon", "sunshine", "princess", "football",
	"iloveyou", "master", "login", "passw0rd", "starwars", "654321",
	"superman", "qazwsx", "trustno1", "baseball", "shadow", "michael",
	"jennifer", "hunter", "ranger", "batman", "soccer", "harley", "buster",
	"thomas", "robert", "matrix", "jordan", "daniel", "andrew", "joshua",
	"charlie", "michelle", "jessica", "computer", "internet", "samsung",
	"whatever", "freedom", "flower", "hottie", "loveme", "zaq1zaq1",
	"password1", "password123", "qwerty123", "qwertyuiop", "asdfgh",
	"zxcvbnm", "1q2w3e4r", "1qaz2wsx", "google", "facebook", "linkedin",
	"secret", "summer", "winter", "spring", "autumn", "changeme", "default",
	"guest", "root", "toor", "test", "test123", "demo", "temp", "pass",
	"letmein123", "welcome123", "admin123", "administrator", "azerty",
	"private", "access", "server", "database", "oracle", "postgres",
	"mysql", "redis", "docker", "jenkins", "gitlab", "github",
}

// commonIndex is built once and read afterwards.
var commonIndex = buildCommonIndex()

func buildCommonIndex() map[string]bool {
	index := make(map[string]bool, len(commonPasswords))
	for _, password := range commonPasswords {
		index[password] = true
	}
	return index
}

// dictionaryFindings checks the password against the common list, both as it
// stands and stripped of the decorations people add to get past a policy.
//
// "Password1!" is not meaningfully different from "password" to anything doing
// the guessing, and a checker that says otherwise is worse than no checker,
// because it tells the user they have solved a problem they still have.
func dictionaryFindings(password string) []Finding {
	lowered := strings.ToLower(password)

	if commonIndex[lowered] {
		return []Finding{{
			Code:       FindingCommon,
			Message:    "it is one of the most commonly used passwords",
			Suggestion: "choose something that is not on every guessing list",
			Penalty:    60,
			Cap:        5,
		}}
	}

	if base := stripDecoration(lowered); base != lowered && commonIndex[base] {
		return []Finding{{
			Code:    FindingCommonBase,
			Message: "it is a very common password with digits or symbols added",
			Suggestion: "adding 1 and an exclamation mark to a known word does not help; " +
				"guessing tools try exactly that",
			Penalty: 45,
			// A known base is found in milliseconds however long the
			// decoration is, so the character arithmetic must not be allowed
			// to produce a respectable number.
			Cap: 20,
		}}
	}

	return nil
}

// stripDecoration removes the trailing digits and symbols people append to
// satisfy a complexity rule, and the leading capital they add for the same
// reason. It is a small transformation on purpose: the aim is to recognise a
// known word wearing a hat, not to reimplement a cracking rule engine.
func stripDecoration(value string) string {
	trimmed := strings.TrimRightFunc(value, func(character rune) bool {
		return unicode.IsDigit(character) || unicode.IsPunct(character) || unicode.IsSymbol(character)
	})
	return strings.TrimSpace(trimmed)
}
