package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/security"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newSecurityPasswordCheckCommand() *Command {
	var (
		useStdin bool
		minimum  int
	)

	return &Command{
		Name:    "password-check",
		Summary: "Judge how strong a password is",
		Usage:   "devnest security password-check <password> [flags]",
		Description: "Analyse a password for length, character variety, repeated runs, " +
			"repeated blocks, sequences, keyboard walks, and the choices that appear at " +
			"the top of every breach corpus.\n\n" +
			"The password is never stored, never logged, and never appears in the " +
			"result: not the password, not a substring of it, not a quoted example of " +
			"the weak pattern that was found. A result is rendered, exported, and pasted " +
			"into tickets; describing the shape of a weakness is enough to fix it.\n\n" +
			"Prefer --stdin. A password given as an argument is written to your shell " +
			"history and is visible to every other process running as you.\n\n" +
			"With --min-score the command exits non-zero when the password falls short, " +
			"which makes it usable as a check in a setup script.",
		Examples: []Example{
			{
				Command:     "echo 'correct horse battery staple' | devnest security password-check --stdin",
				Description: "Check a password without putting it in your shell history.",
			},
			{
				Command:     "devnest security password-check 'Password123!'",
				Description: "Check a password directly, accepting that it lands in history.",
			},
			{
				Command:     "devnest security password-check --stdin --min-score 60 --output json",
				Description: "Use it as a gate: exits non-zero below the score you set.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&useStdin, "stdin", false, "read the password from standard input")
			set.IntVar(&minimum, "min-score", 0, "exit non-zero below this score (0 to 100)")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			if minimum < 0 || minimum > 100 {
				return errors.New(errors.CodeInvalidInput,
					"--min-score must be between 0 and 100").
					WithHint("a score of 60 or more is reported as strong")
			}

			password, err := readSecret(env, args, useStdin, "password")
			if err != nil {
				return err
			}

			result, err := security.CheckStrength(password)
			if err != nil {
				return err
			}

			if err := env.Emit(result, strengthText(result)); err != nil {
				return err
			}

			if minimum > 0 && result.Score < minimum {
				return errors.New(errors.CodeCheckFailed,
					"the password scores %d, below the required %d", result.Score, minimum).
					WithHint("the findings above say what to change")
			}
			return nil
		},
	}
}

func strengthText(result security.StrengthResult) output.TextFunc {
	return func(w io.Writer) error {
		err := output.WriteFields(w, []output.Field{
			{Label: "rating", Value: result.Rating},
			{Label: "score", Value: fmt.Sprintf("%d / 100", result.Score)},
			{Label: "length", Value: fmt.Sprintf("%d characters", result.Length)},
			{Label: "classes", Value: joinClasses(result.Classes)},
			{Label: "entropy", Value: fmt.Sprintf("%.0f bits", result.EntropyBits)},
		})
		if err != nil {
			return err
		}

		if len(result.Findings) == 0 {
			fmt.Fprintln(w, "\nNo weaknesses found.")
			fmt.Fprintln(w, "This is not a guarantee: the built-in list of known-bad "+
				"passwords is short by design.")
			return nil
		}

		fmt.Fprintf(w, "\n%s:\n", pluralWeaknesses(len(result.Findings)))
		for _, finding := range result.Findings {
			fmt.Fprintf(w, "  %s\n", finding.Message)
			if finding.Suggestion != "" {
				fmt.Fprintf(w, "      %s\n", finding.Suggestion)
			}
		}
		return nil
	}
}

func pluralWeaknesses(count int) string {
	if count == 1 {
		return "1 weakness"
	}
	return fmt.Sprintf("%d weaknesses", count)
}
