package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/data"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/fs"
)

// newJSONCommand builds the "json" group. It is runnable itself: "devnest json
// config.json" validates the file, which is the question people have most
// often and should not need a subcommand.
func newJSONCommand() *Command {
	var useStdin bool

	return &Command{
		Name:    "json",
		Summary: "Validate, format, minify, query, and convert JSON",
		Usage:   "devnest json [command] <file> [flags]",
		Description: "Read a JSON document and report what is in it, print it with " +
			"consistent indentation, strip its whitespace, select part of it, or " +
			"convert it to YAML or CSV.\n\n" +
			"With no subcommand this validates: it parses the document and reports its " +
			"shape and size. A document that does not parse is an error naming the line " +
			"and the column and quoting the text around it, because \"invalid JSON\" on " +
			"its own tells you what you already knew.\n\n" +
			"Nothing here writes to the file it read. Formatting and conversion print to " +
			"standard output, so a redirect does the writing where you can see it.\n\n" +
			"Every command reads standard input with --stdin.",
		Examples: []Example{
			{
				Command:     "devnest json package.json",
				Description: "Check that a file is valid JSON and see what it holds.",
			},
			{
				Command:     "curl -s https://example.com/api | devnest json --stdin",
				Description: "Check a response without saving it first.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&useStdin, "stdin", false, "read the document from standard input")
		},
		Run: validateRun(data.FormatJSON, &useStdin),
		Commands: []*Command{
			newJSONFormatCommand(),
			newJSONMinifyCommand(),
			newJSONQueryCommand(),
			newJSONToYAMLCommand(),
			newJSONToCSVCommand(),
		},
	}
}

// newYAMLCommand builds the "yaml" group.
func newYAMLCommand() *Command {
	var useStdin bool

	return &Command{
		Name:    "yaml",
		Summary: "Validate YAML and convert it to JSON",
		Usage:   "devnest yaml [command] <file> [flags]",
		Description: "Read a YAML document and report what is in it, or convert it to " +
			"JSON.\n\n" +
			"With no subcommand this validates. A multi-document file is normal in " +
			"YAML and is handled: every document is parsed and the count is reported.\n\n" +
			"There is no yaml format command, and that is deliberate. Reprinting YAML " +
			"means decoding and re-emitting it, which drops every comment in the file. " +
			"A formatter that silently deletes the comments from a configuration file " +
			"is not a formatter anybody should run.\n\n" +
			"Nothing here writes to the file it read.",
		Examples: []Example{
			{
				Command:     "devnest yaml docker-compose.yml",
				Description: "Check that a file is valid YAML and see what it holds.",
			},
			{
				Command:     "devnest yaml manifest.yaml --output json",
				Description: "The same check for a script, including the document count.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&useStdin, "stdin", false, "read the document from standard input")
		},
		Run: validateRun(data.FormatYAML, &useStdin),
		Commands: []*Command{
			newYAMLToJSONCommand(),
		},
	}
}

// dataReader is the real filesystem, which is what every data command gets in
// production. Tests call the module directly with a fake.
func dataReader() data.Reader { return fs.System{} }

// dataRequest resolves where the document comes from: a path, or a pipe.
//
// Unlike the log commands there is a sensible alternative to a file here, so
// the absence of one is only an error when --stdin was not given either.
func dataRequest(env *Env, args []string, useStdin bool) (data.Request, error) {
	if useStdin {
		if len(args) > 0 {
			return data.Request{}, errors.New(errors.CodeInvalidInput,
				"--stdin was given along with a file").
				WithHint("pass a path or pipe the document in, not both")
		}
		if env.Stdin == nil {
			return data.Request{}, errors.New(errors.CodeInvalidInput,
				"there is nothing on standard input")
		}
		return data.Request{Input: env.Stdin}, nil
	}

	switch len(args) {
	case 1:
		return data.Request{Path: args[0]}, nil
	case 0:
		return data.Request{}, errors.New(errors.CodeInvalidInput, "no file was given").
			WithHint("pass the path of the document, or use --stdin to read a pipe")
	default:
		return data.Request{}, errors.New(errors.CodeInvalidInput,
			"expected one file, found %d arguments", len(args)).
			WithHint("run one command per file, or quote a path containing spaces")
	}
}

// validateRun is the handler both groups use when run without a subcommand.
func validateRun(format string, useStdin *bool) func(context.Context, *Env, []string) error {
	return func(_ context.Context, env *Env, args []string) error {
		request, err := dataRequest(env, args, *useStdin)
		if err != nil {
			return err
		}

		result, err := data.Validate(dataReader(), data.ValidateRequest{
			Request: request,
			Format:  format,
		})
		if err != nil {
			return err
		}

		return env.Emit(result, validateText(result))
	}
}

func validateText(result data.ValidateResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "file", Value: result.Path},
			{Label: "format", Value: result.Format},
			{Label: "valid", Value: "yes"},
			{Label: "size", Value: output.Bytes(int64(result.Bytes))},
			{Label: "lines", Value: output.Count(result.Lines)},
		}

		if result.Documents > 1 {
			fields = append(fields, output.Field{
				Label: "documents",
				Value: output.Count(result.Documents),
			})
		}

		shape := result.Kind
		switch result.Kind {
		case data.KindObject:
			shape = fmt.Sprintf("%s (%s keys)", result.Kind, output.Count(result.Entries))
		case data.KindArray:
			shape = fmt.Sprintf("%s (%s entries)", result.Kind, output.Count(result.Entries))
		}
		fields = append(fields, output.Field{Label: "top level", Value: shape})

		return output.WriteFields(w, fields)
	}
}

// writeDocument prints a converted or reformatted document as it is.
//
// Nothing is added around it: the output of these commands is meant to be
// redirected into a file or piped onward, and a heading would end up in it.
func writeDocument(text string) output.TextFunc {
	return func(w io.Writer) error {
		if _, err := io.WriteString(w, text); err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write output")
		}
		return nil
	}
}
