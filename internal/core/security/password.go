package security

import (
	"crypto/rand"
	"io"
	"math"
	"math/big"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// Password length bounds.
//
// The floor is not a suggestion about what is secure (eight characters is not):
// it is the point below which generating a password at all is theatre. The
// ceiling exists so a typo in a script cannot ask for a megabyte of output.
const (
	MinPasswordLength = 8
	MaxPasswordLength = 4096
	MaxPasswordCount  = 100
)

// PasswordRequest describes one generation.
type PasswordRequest struct {
	// Length of each password.
	Length int
	// Count is how many to generate.
	Count int
	// Which character classes to draw from.
	Lowercase bool
	Uppercase bool
	Digits    bool
	Symbols   bool
	// SymbolSet replaces the built-in symbol characters.
	SymbolSet string
	// Exclude removes specific characters from the pool.
	Exclude string
	// ExcludeAmbiguous removes the characters people misread, such as O and 0.
	ExcludeAmbiguous bool
	// RequireEach guarantees at least one character from every enabled class.
	RequireEach bool
}

// PasswordResult is the generated material and how it was produced.
//
// Passwords is the only field carrying secret data. Everything else describes
// the recipe, which is safe to log, export, or paste into a ticket.
type PasswordResult struct {
	Passwords   []string `json:"passwords"`
	Length      int      `json:"length"`
	Count       int      `json:"count"`
	Classes     []string `json:"classes"`
	Alphabet    int      `json:"alphabetSize"`
	EntropyBits float64  `json:"entropyBits"`
	RequireEach bool     `json:"requireEachClass"`
}

// GeneratePassword produces cryptographically random passwords.
//
// The randomness comes from the caller: crypto/rand.Reader in production, a
// deterministic stream in tests. There is no path through this function that
// reaches for math/rand, and no seeding: a password generator that can be
// reproduced is not a password generator.
//
// Selection is uniform. crypto/rand.Int does the rejection sampling that keeps
// it that way; taking a random byte modulo the alphabet size would quietly
// favour the first few characters of the pool, which is the classic way this
// gets written wrong.
func GeneratePassword(random io.Reader, request PasswordRequest) (PasswordResult, error) {
	if random == nil {
		random = rand.Reader
	}

	pools, err := buildPools(request)
	if err != nil {
		return PasswordResult{}, err
	}

	combined := strings.Join(pools.byClass, "")
	if request.Length < MinPasswordLength {
		return PasswordResult{}, errors.New(errors.CodeInvalidInput,
			"a length of %d is too short", request.Length).
			WithHint("use at least %d characters", MinPasswordLength)
	}
	if request.Length > MaxPasswordLength {
		return PasswordResult{}, errors.New(errors.CodeInvalidInput,
			"a length of %d is more than this command will generate", request.Length).
			WithHint("the maximum is %d", MaxPasswordLength)
	}
	if request.RequireEach && request.Length < len(pools.byClass) {
		return PasswordResult{}, errors.New(errors.CodeInvalidInput,
			"a length of %d cannot hold one character from each of %d classes",
			request.Length, len(pools.byClass)).
			WithHint("shorten the class list or raise the length")
	}

	count := request.Count
	if count <= 0 {
		count = 1
	}
	if count > MaxPasswordCount {
		return PasswordResult{}, errors.New(errors.CodeInvalidInput,
			"%d passwords is more than this command will generate at once", count).
			WithHint("the maximum is %d", MaxPasswordCount)
	}

	passwords := make([]string, 0, count)
	for index := 0; index < count; index++ {
		password, err := generateOne(random, request, pools, combined)
		if err != nil {
			return PasswordResult{}, err
		}
		passwords = append(passwords, password)
	}

	return PasswordResult{
		Passwords:   passwords,
		Length:      request.Length,
		Count:       count,
		Classes:     pools.classes,
		Alphabet:    len([]rune(combined)),
		EntropyBits: round2(float64(request.Length) * math.Log2(float64(len([]rune(combined))))),
		RequireEach: request.RequireEach,
	}, nil
}

// pools holds the character sets a request resolved to.
type pools struct {
	classes []string
	byClass []string
}

func buildPools(request PasswordRequest) (pools, error) {
	symbols := poolSymbols
	if request.SymbolSet != "" {
		symbols = request.SymbolSet
	}

	exclude := request.Exclude
	if request.ExcludeAmbiguous {
		exclude += ambiguous
	}

	candidates := []struct {
		name    string
		enabled bool
		pool    string
	}{
		{ClassLowercase, request.Lowercase, poolLowercase},
		{ClassUppercase, request.Uppercase, poolUppercase},
		{ClassDigits, request.Digits, poolDigits},
		{ClassSymbols, request.Symbols, symbols},
	}

	var result pools
	for _, candidate := range candidates {
		if !candidate.enabled {
			continue
		}
		pool := removeRunes(candidate.pool, exclude)
		if pool == "" {
			return pools{}, errors.New(errors.CodeInvalidInput,
				"every %s character was excluded", candidate.name).
				WithHint("relax --exclude, or turn that class off")
		}
		result.classes = append(result.classes, candidate.name)
		result.byClass = append(result.byClass, pool)
	}

	if len(result.byClass) == 0 {
		return pools{}, errors.New(errors.CodeInvalidInput,
			"no character classes were enabled").
			WithHint("enable at least one of --lowercase, --uppercase, --digits, or --symbols")
	}

	return result, nil
}

// generateOne builds a single password.
//
// When each class is required, one character is drawn from each and the
// remainder from the combined pool; the whole thing is then shuffled. Placing
// the required characters at fixed positions (first four, say) would leak
// their classes to anyone who knows how the generator works, which is
// everyone, because this code is public.
func generateOne(random io.Reader, request PasswordRequest, sets pools, combined string) (string, error) {
	alphabet := []rune(combined)
	buffer := make([]rune, 0, request.Length)

	if request.RequireEach {
		for _, pool := range sets.byClass {
			character, err := pick(random, []rune(pool))
			if err != nil {
				return "", err
			}
			buffer = append(buffer, character)
		}
	}

	for len(buffer) < request.Length {
		character, err := pick(random, alphabet)
		if err != nil {
			return "", err
		}
		buffer = append(buffer, character)
	}

	if err := shuffle(random, buffer); err != nil {
		return "", err
	}

	password := string(buffer)

	// The rune buffer is the one copy this function controls. See the package
	// comment for what clearing it does and does not achieve.
	for index := range buffer {
		buffer[index] = 0
	}

	return password, nil
}

func pick(random io.Reader, pool []rune) (rune, error) {
	index, err := randomIndex(random, len(pool))
	if err != nil {
		return 0, err
	}
	return pool[index], nil
}

// shuffle is a Fisher-Yates shuffle driven by the same random source.
func shuffle(random io.Reader, buffer []rune) error {
	for index := len(buffer) - 1; index > 0; index-- {
		swap, err := randomIndex(random, index+1)
		if err != nil {
			return err
		}
		buffer[index], buffer[swap] = buffer[swap], buffer[index]
	}
	return nil
}

// randomIndex returns a uniform value in [0, n).
func randomIndex(random io.Reader, n int) (int, error) {
	if n <= 0 {
		return 0, errors.New(errors.CodeInternal, "cannot choose from an empty set")
	}

	value, err := rand.Int(random, big.NewInt(int64(n)))
	if err != nil {
		return 0, errors.Wrap(err, errors.CodeInternal,
			"the system random source failed").
			WithHint("this is unusual; the operating system could not supply randomness")
	}
	return int(value.Int64()), nil
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}
