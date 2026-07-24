package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/encoding"
	"github.com/devnest/devnest/internal/output"
)

// newEncodeCommand builds the "encode" group.
//
// Base64 lives in the security group, because it arrived with the commands
// that hash and verify. Duplicating it here would give the same operation two
// names, so this group covers what Base64 does not: hex and percent-encoding.
func newEncodeCommand() *Command {
	return &Command{
		Name:    "encode",
		Summary: "Hex and URL percent-encoding",
		Usage:   "devnest encode <command> <text> [flags]",
		Description: "Encode text as hex or as a URL-safe value, entirely on this " +
			"machine.\n\n" +
			"Input is treated as bytes and Go strings are already UTF-8, so text in any " +
			"language encodes and round-trips without a character set to configure.\n\n" +
			"Base64 is \"devnest security encode\", alongside the commands that hash and " +
			"verify.\n\n" +
			"Encoding is not encryption. None of this hides anything from anybody.",
		Commands: []*Command{
			newEncodeHexCommand(),
			newEncodeURLCommand(),
		},
	}
}

// newDecodeCommand builds the "decode" group.
func newDecodeCommand() *Command {
	return &Command{
		Name:    "decode",
		Summary: "Hex, URL percent-decoding, and JWT inspection",
		Usage:   "devnest decode <command> <value> [flags]",
		Description: "Decode a hex or percent-encoded value, or read what is inside a " +
			"JSON Web Token.\n\n" +
			"Everything happens on this machine and nothing is transmitted anywhere. " +
			"That is the entire point of \"decode jwt\": a token pasted into a web page " +
			"has been handed to whoever runs that page, and there is no taking it " +
			"back.\n\n" +
			"When decoded bytes are not printable text they are shown as Base64 rather " +
			"than written to the terminal, because arbitrary bytes can carry escape " +
			"sequences that change how a terminal behaves.\n\n" +
			"Base64 is \"devnest security decode\".",
		Commands: []*Command{
			newDecodeHexCommand(),
			newDecodeURLCommand(),
			newDecodeJWTCommand(),
		},
	}
}

func newEncodeHexCommand() *Command {
	var (
		upper    bool
		useStdin bool
	)

	return &Command{
		Name:    "hex",
		Summary: "Hex-encode text",
		Usage:   "devnest encode hex <text> [flags]",
		Description: "Encode text as hexadecimal.\n\n" +
			"--upper prints A-F instead of a-f. Both decode identically; matching the " +
			"case of a value from somewhere else makes the two easier to compare by eye.",
		Examples: []Example{
			{
				Command:     "devnest encode hex 'hello'",
				Description: "Encode a string.",
			},
			{
				Command:     "cat key.bin | devnest encode hex --stdin --upper",
				Description: "Encode content arriving on a pipe, in uppercase.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&upper, "upper", false, "print A-F instead of a-f")
			set.BoolVar(&useStdin, "stdin", false, "read the text from standard input")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			text, err := encodingInput(env, args, useStdin, "text")
			if err != nil {
				return err
			}

			result, err := encoding.EncodeHex(encoding.HexEncodeRequest{Text: text, Upper: upper})
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

func newDecodeHexCommand() *Command {
	var useStdin bool

	return &Command{
		Name:    "hex",
		Summary: "Decode a hex value",
		Usage:   "devnest decode hex <value> [flags]",
		Description: "Decode a hexadecimal value.\n\n" +
			"Either case is accepted, as is a leading 0x and the separators a value " +
			"picks up on its way through a dump, a log line, or a wrapped email: " +
			"spaces, colons, dashes, and line breaks are all ignored.\n\n" +
			"Bytes that are not printable text come back as Base64 instead of being " +
			"written to the terminal.",
		Examples: []Example{
			{
				Command:     "devnest decode hex 68656c6c6f",
				Description: "Decode a hex string.",
			},
			{
				Command:     "devnest decode hex '68:65:6c:6c:6f' --output json",
				Description: "Decode a value copied out of a packet dump.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&useStdin, "stdin", false, "read the value from standard input")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			value, err := encodingInput(env, args, useStdin, "value")
			if err != nil {
				return err
			}

			result, err := encoding.DecodeHex(encoding.HexDecodeRequest{Value: value})
			if err != nil {
				return err
			}

			return env.Emit(result, hexDecodeText(result))
		},
	}
}

func hexDecodeText(result encoding.HexDecodeResult) output.TextFunc {
	return func(w io.Writer) error {
		if result.Printable {
			_, err := fmt.Fprintln(w, result.Decoded)
			return err
		}

		fmt.Fprintf(w, "%s\n\n", result.Base64)
		fmt.Fprintf(w, "The value decoded to %s that is not printable text, "+
			"shown here as Base64.\n", output.Bytes(int64(result.Bytes)))
		fmt.Fprintln(w, "Writing arbitrary bytes to a terminal can change how it behaves.")
		return nil
	}
}

func newEncodeURLCommand() *Command {
	var (
		asPath   bool
		useStdin bool
	)

	return &Command{
		Name:    "url",
		Summary: "Percent-encode text",
		Usage:   "devnest encode url <text> [flags]",
		Description: "Percent-encode a value for use inside a URL.\n\n" +
			"The whole input is treated as one value and never as a URL. Encoding a " +
			"complete URL would escape the colons and slashes that make it one; this " +
			"escapes a value that is about to be put inside one.\n\n" +
			"By default the value is encoded for a query string, where a space becomes " +
			"a plus sign. --path encodes for a path segment, where it becomes %20.",
		Examples: []Example{
			{
				Command:     "devnest encode url 'a b&c=d'",
				Description: "Encode a query parameter value.",
			},
			{
				Command:     "devnest encode url 'my file.txt' --path",
				Description: "Encode a value going into a path segment.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&asPath, "path", false, "encode for a path segment rather than a query value")
			set.BoolVar(&useStdin, "stdin", false, "read the text from standard input")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			text, err := encodingInput(env, args, useStdin, "text")
			if err != nil {
				return err
			}

			result, err := encoding.EncodeURL(encoding.URLEncodeRequest{Text: text, Path: asPath})
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

func newDecodeURLCommand() *Command {
	var (
		asPath   bool
		useStdin bool
	)

	return &Command{
		Name:    "url",
		Summary: "Percent-decode a value",
		Usage:   "devnest decode url <value> [flags]",
		Description: "Decode a percent-encoded value.\n\n" +
			"By default a plus sign reads as a space, which is what a query value " +
			"means by it. --path leaves it alone, which is what a path segment means " +
			"by it; decoding a path value the other way silently turns a plus in a " +
			"filename into a space.",
		Examples: []Example{
			{
				Command:     "devnest decode url 'a+b%26c%3Dd'",
				Description: "Decode a query parameter value.",
			},
			{
				Command:     "devnest decode url 'my%20file.txt' --path",
				Description: "Decode a path segment, keeping any plus signs.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&asPath, "path", false, "treat + as a literal plus rather than a space")
			set.BoolVar(&useStdin, "stdin", false, "read the value from standard input")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			value, err := encodingInput(env, args, useStdin, "value")
			if err != nil {
				return err
			}

			result, err := encoding.DecodeURL(encoding.URLDecodeRequest{Value: value, Path: asPath})
			if err != nil {
				return err
			}

			return env.Emit(result, func(w io.Writer) error {
				_, err := fmt.Fprintln(w, result.Decoded)
				return err
			})
		},
	}
}
