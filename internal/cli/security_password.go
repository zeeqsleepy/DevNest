package cli

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/security"
	"github.com/devnest/devnest/internal/output"
)

func newSecurityPasswordCommand() *Command {
	var (
		length     int
		count      int
		noUpper    bool
		noLower    bool
		noDigits   bool
		noSymbols  bool
		useSymbols bool
		symbolSet  string
		exclude    string
		noAmbig    bool
		requireAll bool
	)

	return &Command{
		Name:    "password",
		Summary: "Generate a strong random password",
		Usage:   "devnest security password [flags]",
		Description: "Generate one or more passwords using the operating system's " +
			"cryptographic random source. There is no seed and no way to reproduce a " +
			"result, which is the point.\n\n" +
			"All four character classes are used by default; turn any of them off with " +
			"--no-uppercase, --no-lowercase, --no-digits, or --no-symbols. " +
			"--exclude-ambiguous drops the characters people misread when copying by " +
			"hand, such as O and 0. --require-each guarantees at least one character " +
			"from every enabled class, for systems that insist on it.\n\n" +
			"The default symbol set leaves out quotes and backslashes, because a " +
			"password that breaks when pasted into a shell or a YAML file gets replaced " +
			"by a weaker one the user picks themselves.\n\n" +
			"The password is written to standard output and nowhere else. It is never " +
			"logged, never written to a file, and never kept.",
		Examples: []Example{
			{
				Command:     "devnest security password",
				Description: "Generate one password using the configured defaults.",
			},
			{
				Command:     "devnest security password --length 32 --require-each",
				Description: "Generate a 32-character password with every class guaranteed.",
			},
			{
				Command:     "devnest security password --count 5 --no-symbols --exclude-ambiguous",
				Description: "Generate five alphanumeric passwords safe to read aloud or retype.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.IntVar(&length, "length", 0, "how many characters (default from configuration)")
			set.IntVar(&length, "l", 0, "how many characters (shorthand)")
			set.IntVar(&count, "count", 1, "how many passwords to generate")
			set.BoolVar(&noUpper, "no-uppercase", false, "leave out A-Z")
			set.BoolVar(&noLower, "no-lowercase", false, "leave out a-z")
			set.BoolVar(&noDigits, "no-digits", false, "leave out 0-9")
			set.BoolVar(&noSymbols, "no-symbols", false, "leave out punctuation")
			set.BoolVar(&useSymbols, "symbols", false,
				"include punctuation, even when configuration turns it off")
			set.StringVar(&symbolSet, "symbol-set", "", "use exactly these symbol characters")
			set.StringVar(&exclude, "exclude", "", "never use these characters")
			set.BoolVar(&noAmbig, "exclude-ambiguous", false,
				"leave out characters that are easy to misread, such as O and 0")
			set.BoolVar(&requireAll, "require-each", false,
				"guarantee at least one character from every enabled class")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			if len(args) > 0 {
				return tooManyPaths(args, "devnest security password")
			}

			// Symbols have three sources, so both directions get a flag:
			// configuration sets the default, --symbols turns them on, and
			// --no-symbols turns them off. Offering only the negative would
			// leave someone whose configuration disables symbols with no way
			// to ask for them once.
			symbols := env.Config.Security.PasswordSymbols
			switch {
			case noSymbols:
				symbols = false
			case useSymbols:
				symbols = true
			}

			request := security.PasswordRequest{
				Length:           length,
				Count:            count,
				Lowercase:        !noLower,
				Uppercase:        !noUpper,
				Digits:           !noDigits,
				Symbols:          symbols,
				SymbolSet:        symbolSet,
				Exclude:          exclude,
				ExcludeAmbiguous: noAmbig || env.Config.Security.PasswordExcludeAmbiguous,
				RequireEach:      requireAll,
			}
			if request.Length == 0 {
				request.Length = int(env.Config.Security.PasswordLength)
			}

			result, err := security.GeneratePassword(rand.Reader, request)
			if err != nil {
				return err
			}

			return env.Emit(result, passwordText(result))
		},
	}
}

func passwordText(result security.PasswordResult) output.TextFunc {
	return func(w io.Writer) error {
		for _, password := range result.Passwords {
			fmt.Fprintln(w, password)
		}

		fmt.Fprintln(w)
		return output.WriteFields(w, []output.Field{
			{Label: "length", Value: fmt.Sprintf("%d", result.Length)},
			{Label: "classes", Value: joinClasses(result.Classes)},
			{Label: "alphabet", Value: fmt.Sprintf("%d characters", result.Alphabet)},
			{Label: "entropy", Value: fmt.Sprintf("%.0f bits", result.EntropyBits)},
		})
	}
}

func joinClasses(classes []string) string {
	if len(classes) == 0 {
		return "none"
	}
	joined := classes[0]
	for _, class := range classes[1:] {
		joined += ", " + class
	}
	return joined
}
