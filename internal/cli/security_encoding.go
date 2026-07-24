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

func newSecurityEncodeCommand() *Command {
	var (
		urlSafe   bool
		noPadding bool
		useStdin  bool
	)

	return &Command{
		Name:    "encode",
		Summary: "Base64-encode text",
		Usage:   "devnest security encode <text> [flags]",
		Description: "Encode text as Base64, entirely on this machine.\n\n" +
			"Input is treated as bytes. Go strings are already UTF-8, so text in any " +
			"language encodes and round-trips without anything to configure.\n\n" +
			"--url-safe uses the alphabet with - and _ in place of + and /, which is " +
			"what a URL or a JWT needs. --no-padding leaves off the trailing = " +
			"characters.\n\n" +
			"Base64 is an encoding, not encryption. It hides nothing from anybody.",
		Examples: []Example{
			{
				Command:     "devnest security encode 'hello world'",
				Description: "Encode a string.",
			},
			{
				Command:     "devnest security encode 'a+b/c' --url-safe --no-padding",
				Description: "Produce a value safe to put in a URL or a JWT segment.",
			},
			{
				Command:     "cat token.txt | devnest security encode --stdin",
				Description: "Encode content without it appearing in your shell history.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&urlSafe, "url-safe", false, "use the URL and filename safe alphabet")
			set.BoolVar(&noPadding, "no-padding", false, "leave off the trailing = characters")
			set.BoolVar(&useStdin, "stdin", false, "read the text from standard input")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			text, err := encodingInput(env, args, useStdin, "text")
			if err != nil {
				return err
			}

			result, err := security.Encode(security.EncodeRequest{
				Text:      text,
				URLSafe:   urlSafe,
				NoPadding: noPadding,
			})
			if err != nil {
				return err
			}

			return env.Emit(result, func(w io.Writer) error {
				_, err := fmt.Fprintln(w, result.Encoded)
				return err
			})
		},
	}
}

func newSecurityDecodeCommand() *Command {
	var (
		useStdin bool
		raw      bool
	)

	return &Command{
		Name:    "decode",
		Summary: "Base64-decode a value",
		Usage:   "devnest security decode <value> [flags]",
		Description: "Decode a Base64 value, entirely on this machine.\n\n" +
			"Both alphabets are accepted, with or without padding, and whitespace from " +
			"a value that was wrapped across lines is ignored. There is nothing to tell " +
			"the command about the shape of the input, because it can work it out.\n\n" +
			"When the decoded bytes are not printable text, the result is shown as hex " +
			"rather than written to the terminal. Arbitrary bytes can carry escape " +
			"sequences that move the cursor or change how the terminal behaves, and a " +
			"decode command is exactly where untrusted bytes arrive. Pass --raw to " +
			"write them anyway.",
		Examples: []Example{
			{
				Command:     "devnest security decode aGVsbG8gd29ybGQ=",
				Description: "Decode a Base64 string.",
			},
			{
				Command:     "devnest security decode eyJhbGciOiJIUzI1NiJ9 --output json",
				Description: "Decode a JWT header segment, unpadded and URL-safe.",
			},
			{
				Command:     "cat encoded.txt | devnest security decode --stdin",
				Description: "Decode content arriving on a pipe.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&useStdin, "stdin", false, "read the value from standard input")
			set.BoolVar(&raw, "raw", false, "write non-printable bytes to the terminal anyway")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			value, err := encodingInput(env, args, useStdin, "value")
			if err != nil {
				return err
			}

			result, err := security.Decode(security.DecodeRequest{Value: value})
			if err != nil {
				return err
			}

			return env.Emit(result, decodeText(result, raw))
		},
	}
}

// encodingInput resolves an argument or standard input.
//
// Unlike readSecret this does not warn: encoded text is not a secret by virtue
// of being encoded, and warning on every invocation would train people to stop
// reading warnings that do matter.
func encodingInput(env *Env, args []string, useStdin bool, subject string) (string, error) {
	if useStdin {
		if len(args) > 0 {
			return "", errors.New(errors.CodeInvalidInput,
				"--stdin was given along with %s on the command line", subject).
				WithHint("pass it one way or the other")
		}
		return readAllStdin(env)
	}

	switch len(args) {
	case 0:
		return "", errors.New(errors.CodeInvalidInput, "no %s was given", subject).
			WithHint("pass it as an argument, or use --stdin to read it from a pipe")
	case 1:
		return args[0], nil
	default:
		return "", errors.New(errors.CodeInvalidInput,
			"expected one %s, found %d", subject, len(args)).
			WithHint("quote it if it contains spaces")
	}
}

func decodeText(result security.DecodeResult, raw bool) output.TextFunc {
	return func(w io.Writer) error {
		if result.Printable {
			_, err := fmt.Fprintln(w, result.Decoded)
			return err
		}

		if raw {
			// The user asked for the bytes. The hex is still in the structured
			// output for anything reading that instead.
			_, err := fmt.Fprintln(w, result.Hex)
			return err
		}

		fmt.Fprintf(w, "%s\n\n", result.Hex)
		fmt.Fprintf(w, "The decoded value is %s of data that is not printable text.\n",
			output.Bytes(int64(result.Bytes)))
		fmt.Fprintln(w, "It is shown as hex because writing arbitrary bytes to a terminal "+
			"can change how it behaves.")
		return nil
	}
}
