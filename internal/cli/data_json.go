package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/data"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newJSONFormatCommand() *Command {
	var (
		indent   int
		useStdin bool
	)

	return &Command{
		Name:    "format",
		Summary: "Reprint JSON with consistent indentation",
		Usage:   "devnest json format <file> [flags]",
		Description: "Print a JSON document with one indentation width throughout.\n\n" +
			"The document is reprinted from its own bytes rather than decoded and " +
			"re-encoded, so keys stay in the order they were written and numbers keep " +
			"the digits they were written with. The only thing that changes is the " +
			"whitespace, which is what makes the diff reviewable.\n\n" +
			"The file is not modified. Redirect the output to write it: " +
			"devnest json format in.json > out.json, never into the same file.",
		Examples: []Example{
			{
				Command:     "devnest json format package.json",
				Description: "Print the document with two-space indentation.",
			},
			{
				Command:     "devnest json format api.json --indent 4 > formatted.json",
				Description: "Write a reformatted copy at four spaces.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.IntVar(&indent, "indent", 0, "spaces per level (default 2)")
			set.BoolVar(&useStdin, "stdin", false, "read the document from standard input")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			request, err := dataRequest(env, args, useStdin)
			if err != nil {
				return err
			}

			result, err := data.Format(dataReader(), data.FormatRequest{
				Request: request,
				Indent:  indent,
			})
			if err != nil {
				return err
			}

			return env.Emit(result, writeDocument(result.Output))
		},
	}
}

func newJSONMinifyCommand() *Command {
	var useStdin bool

	return &Command{
		Name:    "minify",
		Summary: "Strip the whitespace from JSON",
		Usage:   "devnest json minify <file> [flags]",
		Description: "Print a JSON document with every byte of optional whitespace " +
			"removed.\n\n" +
			"How much that saved is in the structured output rather than on the " +
			"terminal, because the document itself is what standard output carries and " +
			"a note printed alongside it would end up in whatever the output was " +
			"redirected into.",
		Examples: []Example{
			{
				Command:     "devnest json minify config.json > config.min.json",
				Description: "Write a compact copy.",
			},
			{
				Command:     "devnest json minify config.json --output json",
				Description: "See how many bytes it saved.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&useStdin, "stdin", false, "read the document from standard input")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			request, err := dataRequest(env, args, useStdin)
			if err != nil {
				return err
			}

			result, err := data.Minify(dataReader(), data.MinifyRequest{Request: request})
			if err != nil {
				return err
			}

			return env.Emit(result, writeDocument(result.Output+"\n"))
		},
	}
}

func newJSONQueryCommand() *Command {
	var (
		rawValue bool
		useStdin bool
	)

	return &Command{
		Name:    "query",
		Summary: "Select part of a JSON document",
		Usage:   "devnest json query <file> <expression> [flags]",
		Description: "Select one value out of a document with a path expression.\n\n" +
			"Keys are separated by dots and array elements by an index in brackets: " +
			"users[0].name. A leading dot or $ is optional, so an expression copied " +
			"from another tool usually works, and a key holding a dot or a space goes " +
			"in quoted brackets: [\"my.key\"].\n\n" +
			"That is the whole syntax. There are no filters, no wildcards, and no " +
			"functions, which is a decision rather than an unfinished feature: a query " +
			"language is a product of its own, and a half-built one is a worse jq that " +
			"nobody has documented. Selecting a subtree is what a person at a terminal " +
			"needs; anything past it is what jq is for.\n\n" +
			"--raw prints a selected string without its quotes, which is what a shell " +
			"variable wants. An expression selecting nothing exits 3, so a script can " +
			"branch on whether a key exists.\n\n" +
			"Object keys in the selected value come back sorted, because the value is " +
			"re-encoded from what was parsed. Use \"devnest json format\" when the " +
			"original order matters.",
		Examples: []Example{
			{
				Command:     "devnest json query package.json dependencies",
				Description: "Print one subtree.",
			},
			{
				Command:     "devnest json query api.json 'users[0].email' --raw",
				Description: "Print one string without quotes, ready for a shell variable.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&rawValue, "raw", false, "print a selected string without its quotes")
			set.BoolVar(&useStdin, "stdin", false, "read the document from standard input")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			path, expression, err := queryArguments(args, useStdin)
			if err != nil {
				return err
			}

			request, err := dataRequest(env, path, useStdin)
			if err != nil {
				return err
			}

			result, err := data.Query(dataReader(), data.QueryRequest{
				Request:    request,
				Expression: expression,
			})
			if err != nil {
				return err
			}

			return env.Emit(result, queryText(result, rawValue))
		},
	}
}

// queryArguments splits the positional arguments into the file, if there is
// one, and the expression, which there always is.
func queryArguments(args []string, useStdin bool) (path []string, expression string, err error) {
	wanted := 2
	if useStdin {
		wanted = 1
	}

	if len(args) != wanted {
		hint := "pass the file and then the expression, " +
			"for example: devnest json query api.json users[0].name"
		if useStdin {
			hint = "with --stdin, pass the expression alone"
		}
		return nil, "", errors.New(errors.CodeInvalidInput,
			"expected %d argument(s), found %d", wanted, len(args)).WithHint("%s", hint)
	}

	if useStdin {
		return nil, args[0], nil
	}
	return args[:1], args[1], nil
}

func queryText(result data.QueryResult, raw bool) output.TextFunc {
	return func(w io.Writer) error {
		if raw && result.Kind == data.KindString {
			var text string
			if err := json.Unmarshal(result.Value, &text); err != nil {
				return errors.Wrap(err, errors.CodeInternal, "cannot read the selected value")
			}
			_, err := fmt.Fprintln(w, text)
			return err
		}

		var indented bytes.Buffer
		if err := json.Indent(&indented, result.Value, "", "  "); err != nil {
			return errors.Wrap(err, errors.CodeInternal, "cannot print the selected value")
		}

		_, err := fmt.Fprintln(w, indented.String())
		return err
	}
}
