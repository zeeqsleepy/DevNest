// Package security is DevNest's defensive security module: generating
// passwords, judging their strength, hashing, verifying checksums, and Base64
// encoding and decoding.
//
// Everything here happens locally. Nothing in this package opens a socket,
// writes a file, or keeps anything after the call returns.
//
// # Handling secrets
//
// Two rules shape the whole module, and both are load-bearing rather than
// decorative.
//
// A password given to the strength checker never appears in the result. Not
// the password, not a substring of it, not a quoted example of the weak pattern
// that was found. Findings describe what was wrong in general terms ("four
// consecutive characters in sequence" rather than "1234") because a result is
// rendered, exported, pasted into a ticket, and read by people who were never
// meant to see the input. Describing the shape of a weakness is enough to fix
// it.
//
// Nothing here logs. The module has no logger, which is the simplest way to
// guarantee a password never reaches one.
//
// # Clearing memory
//
// Generated passwords are built in rune slices, which are zeroed as soon as
// the value has been copied out. That is worth doing and worth being honest about:
// the string handed back to the caller cannot be zeroed, because Go strings are
// immutable and the runtime may have copied them during a garbage collection.
// This reduces the window in which a password sits in a reusable buffer. It
// does not eliminate the value from the process.
package security

import (
	"strings"
	"unicode"
)

// Character class names, used in requests, results, and help text.
const (
	ClassLowercase = "lowercase"
	ClassUppercase = "uppercase"
	ClassDigits    = "digits"
	ClassSymbols   = "symbols"
)

// Character pools.
//
// The symbol set deliberately leaves out the quote characters and the
// backslash. A generated password is going to be pasted into a shell, a YAML
// file, a connection string, or a CI variable, and those three characters are
// the ones that break it. A password nobody can use safely gets replaced with
// a weaker one the user picks by hand, which is a worse outcome than a
// slightly smaller alphabet.
const (
	poolLowercase = "abcdefghijklmnopqrstuvwxyz"
	poolUppercase = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	poolDigits    = "0123456789"
	poolSymbols   = "!#$%&()*+,-./:;<=>?@[]^_{|}~"
)

// ambiguous are the characters people misread when copying a password by hand
// or reading it off a screen.
const ambiguous = "0OQD1lI|5S2Z8B`'\""

// classify reports which character classes a string draws on.
func classify(value string) []string {
	var hasLower, hasUpper, hasDigit, hasSymbol bool

	for _, character := range value {
		switch {
		case unicode.IsLower(character):
			hasLower = true
		case unicode.IsUpper(character):
			hasUpper = true
		case unicode.IsDigit(character):
			hasDigit = true
		default:
			hasSymbol = true
		}
	}

	classes := make([]string, 0, 4)
	if hasLower {
		classes = append(classes, ClassLowercase)
	}
	if hasUpper {
		classes = append(classes, ClassUppercase)
	}
	if hasDigit {
		classes = append(classes, ClassDigits)
	}
	if hasSymbol {
		classes = append(classes, ClassSymbols)
	}
	return classes
}

// alphabetSize estimates the pool an attacker would have to search, from the
// classes the value actually uses.
//
// This is the conventional estimate and it is generous: it assumes the
// attacker knows which classes were used and nothing else. A password drawn
// from a real word gets a large number here and a low score, which is why the
// score is not simply the entropy.
func alphabetSize(value string) int {
	size := 0
	for _, class := range classify(value) {
		switch class {
		case ClassLowercase:
			size += 26
		case ClassUppercase:
			size += 26
		case ClassDigits:
			size += 10
		case ClassSymbols:
			size += 33
		}
	}
	return size
}

// removeRunes returns pool without any character appearing in remove.
func removeRunes(pool, remove string) string {
	if remove == "" {
		return pool
	}

	excluded := make(map[rune]bool, len(remove))
	for _, character := range remove {
		excluded[character] = true
	}

	var kept strings.Builder
	kept.Grow(len(pool))
	for _, character := range pool {
		if !excluded[character] {
			kept.WriteRune(character)
		}
	}
	return kept.String()
}
