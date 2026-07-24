package security

import (
	"crypto/rand"
	"io"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

// fixedReader is a deterministic stand-in for the system random source, so a
// test can assert on structure without asserting on a value that must never be
// predictable in production.
type fixedReader struct {
	pattern []byte
	offset  int
}

func (f *fixedReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = f.pattern[f.offset%len(f.pattern)]
		f.offset++
	}
	return len(buffer), nil
}

func newFixedReader() io.Reader {
	return &fixedReader{pattern: []byte{
		0x1f, 0x8b, 0x42, 0x07, 0xc3, 0x55, 0x90, 0x2e,
		0x7a, 0x11, 0xe9, 0x64, 0x38, 0xd0, 0xab, 0x5c,
	}}
}

func defaultRequest() PasswordRequest {
	return PasswordRequest{
		Length:    20,
		Count:     1,
		Lowercase: true,
		Uppercase: true,
		Digits:    true,
		Symbols:   true,
	}
}

func assertCode(t *testing.T, err error, want errors.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with code %q, got nil", want)
	}
	if got := errors.CodeOf(err); got != want {
		t.Fatalf("code = %q, want %q (%v)", got, want, err)
	}
}

func TestGeneratePasswordRespectsLength(t *testing.T) {
	for _, length := range []int{8, 16, 20, 64, 128} {
		request := defaultRequest()
		request.Length = length

		result, err := GeneratePassword(rand.Reader, request)
		if err != nil {
			t.Fatalf("GeneratePassword(%d): %v", length, err)
		}
		if got := len([]rune(result.Passwords[0])); got != length {
			t.Errorf("length = %d, want %d", got, length)
		}
	}
}

func TestGeneratePasswordCount(t *testing.T) {
	request := defaultRequest()
	request.Count = 5

	result, err := GeneratePassword(rand.Reader, request)
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}
	if len(result.Passwords) != 5 {
		t.Fatalf("got %d passwords, want 5", len(result.Passwords))
	}

	// Five draws from a pool this large producing a duplicate would mean the
	// randomness is broken, not that we were unlucky.
	seen := make(map[string]bool, 5)
	for _, password := range result.Passwords {
		if seen[password] {
			t.Fatal("two generated passwords are identical")
		}
		seen[password] = true
	}
}

// A generator whose output can be predicted is not a generator. Two calls with
// the real random source must not agree.
func TestGeneratePasswordIsNotReproducible(t *testing.T) {
	first, err := GeneratePassword(rand.Reader, defaultRequest())
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}
	second, err := GeneratePassword(rand.Reader, defaultRequest())
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}

	if first.Passwords[0] == second.Passwords[0] {
		t.Fatal("two runs produced the same password")
	}
}

func TestGeneratePasswordHonoursDisabledClasses(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*PasswordRequest)
		illegal string
	}{
		{"no digits", func(r *PasswordRequest) { r.Digits = false }, poolDigits},
		{"no symbols", func(r *PasswordRequest) { r.Symbols = false }, poolSymbols},
		{"no uppercase", func(r *PasswordRequest) { r.Uppercase = false }, poolUppercase},
		{"no lowercase", func(r *PasswordRequest) { r.Lowercase = false }, poolLowercase},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := defaultRequest()
			request.Length = 200
			test.mutate(&request)

			result, err := GeneratePassword(rand.Reader, request)
			if err != nil {
				t.Fatalf("GeneratePassword: %v", err)
			}
			if strings.ContainsAny(result.Passwords[0], test.illegal) {
				t.Errorf("password contains a character from a disabled class: %q",
					result.Passwords[0])
			}
		})
	}
}

func TestGeneratePasswordRequireEach(t *testing.T) {
	request := defaultRequest()
	request.Length = 8
	request.RequireEach = true

	// Run several times: the guarantee has to hold every time, not usually.
	for attempt := 0; attempt < 50; attempt++ {
		result, err := GeneratePassword(rand.Reader, request)
		if err != nil {
			t.Fatalf("GeneratePassword: %v", err)
		}

		password := result.Passwords[0]
		for _, pool := range []string{poolLowercase, poolUppercase, poolDigits, poolSymbols} {
			if !strings.ContainsAny(password, pool) {
				t.Fatalf("password %q is missing a required class", password)
			}
		}
	}
}

// The required characters must not sit at fixed positions, or their classes
// would be known to anyone who has read this code, which is everyone.
func TestGeneratePasswordShufflesRequiredCharacters(t *testing.T) {
	request := defaultRequest()
	request.Length = 12
	request.RequireEach = true

	positions := make(map[int]bool)
	for attempt := 0; attempt < 60; attempt++ {
		result, err := GeneratePassword(rand.Reader, request)
		if err != nil {
			t.Fatalf("GeneratePassword: %v", err)
		}
		positions[strings.IndexAny(result.Passwords[0], poolDigits)] = true
	}

	if len(positions) < 3 {
		t.Errorf("the first digit landed in only %d distinct positions; "+
			"required characters look unshuffled", len(positions))
	}
}

func TestGeneratePasswordExcludesCharacters(t *testing.T) {
	request := defaultRequest()
	request.Length = 200
	request.Exclude = "aeiou"

	result, err := GeneratePassword(rand.Reader, request)
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}
	if strings.ContainsAny(result.Passwords[0], "aeiou") {
		t.Errorf("password contains an excluded character: %q", result.Passwords[0])
	}
}

func TestGeneratePasswordExcludesAmbiguous(t *testing.T) {
	request := defaultRequest()
	request.Length = 300
	request.ExcludeAmbiguous = true

	result, err := GeneratePassword(rand.Reader, request)
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}
	if strings.ContainsAny(result.Passwords[0], ambiguous) {
		t.Errorf("password contains an easily misread character: %q", result.Passwords[0])
	}
}

func TestGeneratePasswordCustomSymbolSet(t *testing.T) {
	request := defaultRequest()
	request.Length = 100
	request.Lowercase = false
	request.Uppercase = false
	request.Digits = false
	request.SymbolSet = "@#"

	result, err := GeneratePassword(rand.Reader, request)
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}
	for _, character := range result.Passwords[0] {
		if character != '@' && character != '#' {
			t.Fatalf("password contains %q, outside the custom symbol set", character)
		}
	}
}

// A password that breaks when pasted into a shell gets replaced by a weaker
// one the user picks by hand.
func TestDefaultSymbolsAvoidQuotesAndBackslash(t *testing.T) {
	for _, character := range []string{"'", "\"", "\\", "`"} {
		if strings.Contains(poolSymbols, character) {
			t.Errorf("the default symbol set contains %s, which breaks when pasted", character)
		}
	}
}

func TestGeneratePasswordReportsTheRecipe(t *testing.T) {
	request := defaultRequest()
	request.Length = 16

	result, err := GeneratePassword(rand.Reader, request)
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}

	if len(result.Classes) != 4 {
		t.Errorf("classes = %v, want all four", result.Classes)
	}
	expected := 26 + 26 + 10 + len(poolSymbols)
	if result.Alphabet != expected {
		t.Errorf("Alphabet = %d, want %d", result.Alphabet, expected)
	}
	// 16 characters from a pool of 90 is a little over 103 bits.
	if result.EntropyBits < 100 || result.EntropyBits > 110 {
		t.Errorf("EntropyBits = %v, want about 104", result.EntropyBits)
	}
}

func TestGeneratePasswordRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PasswordRequest)
	}{
		{"too short", func(r *PasswordRequest) { r.Length = 4 }},
		{"absurdly long", func(r *PasswordRequest) { r.Length = 100000 }},
		{"too many at once", func(r *PasswordRequest) { r.Count = 10000 }},
		{"no classes", func(r *PasswordRequest) {
			r.Lowercase, r.Uppercase, r.Digits, r.Symbols = false, false, false, false
		}},
		{"everything excluded", func(r *PasswordRequest) {
			r.Lowercase, r.Uppercase, r.Digits = false, false, false
			r.SymbolSet = "@#"
			r.Exclude = "@#"
		}},
		{"require each with no room", func(r *PasswordRequest) {
			r.Length = 8
			r.RequireEach = true
			r.SymbolSet = "!@#$%^&*()_+-=[]{}|;:,.<>?/~"
			// Four classes fit in eight characters, so make the length the
			// problem instead by asking for more classes than characters.
			r.Length = 3
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := defaultRequest()
			test.mutate(&request)

			_, err := GeneratePassword(rand.Reader, request)
			assertCode(t, err, errors.CodeInvalidInput)
		})
	}
}

func TestGeneratePasswordDefaultsToOne(t *testing.T) {
	request := defaultRequest()
	request.Count = 0

	result, err := GeneratePassword(rand.Reader, request)
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}
	if len(result.Passwords) != 1 {
		t.Errorf("got %d passwords, want 1", len(result.Passwords))
	}
}

// Every character in the output must come from the resolved pool. A generator
// that reaches outside it has an indexing bug.
func TestGeneratePasswordDrawsOnlyFromThePool(t *testing.T) {
	request := defaultRequest()
	request.Length = 500

	result, err := GeneratePassword(newFixedReader(), request)
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}

	pool := poolLowercase + poolUppercase + poolDigits + poolSymbols
	for _, character := range result.Passwords[0] {
		if !strings.ContainsRune(pool, character) {
			t.Fatalf("password contains %q, which is not in the pool", character)
		}
	}
}

// Selection has to be uniform. Taking a random byte modulo the alphabet size
// would favour the start of the pool; this checks the distribution is not
// obviously skewed.
func TestGeneratePasswordDistributionIsNotSkewed(t *testing.T) {
	request := PasswordRequest{
		Length: 4000, Count: 1, Lowercase: true,
	}

	result, err := GeneratePassword(rand.Reader, request)
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}

	counts := make(map[rune]int, 26)
	for _, character := range result.Passwords[0] {
		counts[character]++
	}

	if len(counts) < 24 {
		t.Fatalf("only %d of 26 letters appeared in 4000 characters", len(counts))
	}

	// Expected is about 154 per letter. A modulo bias would show up as the
	// early letters running far ahead of the late ones.
	expected := 4000 / 26
	for character, count := range counts {
		if count < expected/2 || count > expected*2 {
			t.Errorf("%q appeared %d times, expected about %d", character, count, expected)
		}
	}
}

func TestRandomIndexRejectsAnEmptySet(t *testing.T) {
	_, err := randomIndex(rand.Reader, 0)
	assertCode(t, err, errors.CodeInternal)
}

func TestRandomIndexStaysInRange(t *testing.T) {
	for size := 1; size <= 40; size++ {
		for attempt := 0; attempt < 20; attempt++ {
			index, err := randomIndex(rand.Reader, size)
			if err != nil {
				t.Fatalf("randomIndex(%d): %v", size, err)
			}
			if index < 0 || index >= size {
				t.Fatalf("randomIndex(%d) = %d, out of range", size, index)
			}
		}
	}
}
