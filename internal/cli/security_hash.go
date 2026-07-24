package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/security"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/fs"
)

func newSecurityHashCommand() *Command {
	var (
		algorithms repeatable
		all        bool
		file       string
		useStdin   bool
	)

	return &Command{
		Name:    "hash",
		Summary: "Compute a digest of text or a file",
		Usage:   "devnest security hash <text> [flags]",
		Description: "Compute a cryptographic digest of a string, a file, or standard " +
			"input. SHA-256 by default; SHA-512 and MD5 are also available.\n\n" +
			"Asking for several algorithms reads the input once and feeds every digest " +
			"from that single pass. Files are streamed through a fixed buffer, so " +
			"hashing a very large file costs the same memory as hashing a small one.\n\n" +
			"MD5 is offered because published checksums still use it, not because it is " +
			"a reasonable choice for anything new. It is broken for any purpose that " +
			"depends on an attacker being unable to construct a collision.\n\n" +
			"This shares its implementation with \"devnest file hash\"; that command is " +
			"the one for hashing several files at once, and this one adds text and " +
			"standard input.",
		Examples: []Example{
			{
				Command:     "devnest security hash 'hello world'",
				Description: "Compute the SHA-256 of a string.",
			},
			{
				Command:     "devnest security hash --file installer.exe --algorithm sha256 --algorithm md5",
				Description: "Compute two digests of a file in a single read.",
			},
			{
				Command:     "cat secret.txt | devnest security hash --stdin",
				Description: "Hash content without it appearing in your shell history.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.Var(&algorithms, "algorithm", "digest to compute: sha256, sha512, or md5 (repeatable)")
			set.Var(&algorithms, "a", "digest to compute (shorthand, repeatable)")
			set.BoolVar(&all, "all", false, "compute every supported digest in one pass")
			set.StringVar(&file, "file", "", "hash the contents of this file")
			set.StringVar(&file, "f", "", "hash the contents of this file (shorthand)")
			set.BoolVar(&useStdin, "stdin", false, "hash whatever arrives on standard input")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			chosen, err := chooseAlgorithms(algorithms, all)
			if err != nil {
				return err
			}

			request, err := hashInput(env, args, file, useStdin)
			if err != nil {
				return err
			}
			request.Algorithms = chosen

			result, err := security.Hash(ctx, filesystem(), request)
			if err != nil {
				return err
			}

			return env.Emit(result, securityHashText(result))
		},
	}
}

// hashInput works out where the content is coming from, refusing combinations
// that would make DevNest guess.
func hashInput(env *Env, args []string, file string, useStdin bool) (security.HashRequest, error) {
	sources := 0
	if file != "" {
		sources++
	}
	if useStdin {
		sources++
	}
	if len(args) > 0 {
		sources++
	}

	switch {
	case sources == 0:
		return security.HashRequest{}, errors.New(errors.CodeInvalidInput,
			"no input was given").
			WithHint("pass text to hash, --file to hash a file, or --stdin to read a pipe")
	case sources > 1:
		return security.HashRequest{}, errors.New(errors.CodeInvalidInput,
			"more than one input was given").
			WithHint("pass text, --file, or --stdin, but only one of them")
	}

	switch {
	case file != "":
		return security.HashRequest{Source: security.SourceFile, Path: file}, nil

	case useStdin:
		text, err := readAllStdin(env)
		if err != nil {
			return security.HashRequest{}, err
		}
		return security.HashRequest{Source: security.SourceText, Text: text}, nil

	case len(args) > 1:
		return security.HashRequest{}, errors.New(errors.CodeInvalidInput,
			"expected one string, found %d", len(args)).
			WithHint("quote it if it contains spaces")

	default:
		return security.HashRequest{Source: security.SourceText, Text: args[0]}, nil
	}
}

func securityHashText(result security.HashResult) output.TextFunc {
	return func(w io.Writer) error {
		// One digest of one input is the common case, and a table around a
		// single value is noise. Print it bare so it can be piped straight
		// into a comparison.
		if len(result.Checksums) == 1 {
			fmt.Fprintln(w, result.Checksums[0].Value)
			return nil
		}

		rows := make([][]string, 0, len(result.Checksums))
		for _, checksum := range result.Checksums {
			rows = append(rows, []string{checksum.Algorithm, checksum.Value})
		}

		if err := output.WriteTable(w, []output.Column{
			{Title: "algorithm"},
			{Title: "hash"},
		}, rows); err != nil {
			return err
		}

		fmt.Fprintf(w, "\n%s, %s\n", sourceLabel(result), output.Bytes(result.Bytes))
		return nil
	}
}

func sourceLabel(result security.HashResult) string {
	if result.Source == security.SourceFile {
		return result.Path
	}
	return "text input"
}

// algorithmNames lists the supported digests for help and error text.
func algorithmNames() []string {
	names := make([]string, 0, len(fs.Algorithms()))
	for _, algorithm := range fs.Algorithms() {
		names = append(names, string(algorithm))
	}
	return names
}
