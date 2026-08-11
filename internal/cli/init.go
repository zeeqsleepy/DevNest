package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/scaffold"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

// newInitCommand builds "init", which scaffolds a new project from a committed
// template. It is a command of its own rather than a group member because it
// is the first thing a project does: it does not belong under any existing
// group, and a group of one command is a group.
func newInitCommand() *Command {
	var (
		template string
		list     bool
	)

	return &Command{
		Name:    "init",
		Summary: "Scaffold a new project from a template",
		Usage:   "devnest init <directory> [flags]",
		Description: "Create a new project from a template into a directory.\n\n" +
			"The templates are embedded in the binary, so an installation " +
			"downloaded from a release page scaffolds exactly what a build from " +
			"source does. --list shows them.\n\n" +
			"A scaffold never overwrites: the target directory is created, and " +
			"one that already contains files is refused. A template is the start " +
			"of a project, not a merge, and the safe action is a fresh, empty " +
			"directory.",
		Examples: []Example{
			{
				Command:     "devnest init my-project",
				Description: "Scaffold the default blank project into ./my-project.",
			},
			{
				Command:     "devnest init --template go-cli api",
				Description: "Scaffold the Go CLI starter into ./api.",
			},
			{
				Command:     "devnest init --list",
				Description: "List the templates that can be scaffolded.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.StringVar(&template, "template", "", "which template to copy (default blank)")
			set.BoolVar(&list, "list", false, "list the available templates")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			if list {
				return env.Emit(scaffold.Templates(), initListText(scaffold.Templates()))
			}

			target, err := initTarget(args, template)
			if err != nil {
				return err
			}

			scaffoldTemplate := template
			if scaffoldTemplate == "" {
				scaffoldTemplate = "blank"
			}

			result, err := scaffold.Create(ctx, scaffold.Request{
				Template: scaffoldTemplate,
				Target:   target,
			})
			if err != nil {
				return err
			}
			return env.Emit(result, initResultText(result))
		},
	}
}

// initTarget takes the directory name.
func initTarget(args []string, template string) (string, error) {
	if len(args) == 0 {
		return "", errors.New(errors.CodeInvalidInput, "no target directory was given").
			WithHint("pass the directory to create, for example: devnest init my-project")
	}
	if len(args) > 1 {
		return "", errors.New(errors.CodeInvalidInput,
			"expected one target directory, found %d arguments", len(args)).
			WithHint("run one scaffold per directory")
	}
	return args[0], nil
}

func initListText(names []string) output.TextFunc {
	return func(w io.Writer) error {
		if len(names) == 0 {
			_, err := fmt.Fprintln(w, "No templates are available.")
			return err
		}
		for _, name := range names {
			if _, err := fmt.Fprintln(w, name); err != nil {
				return err
			}
		}
		return nil
	}
}

func initResultText(result scaffold.Result) output.TextFunc {
	return func(w io.Writer) error {
		if _, err := fmt.Fprintf(w, "scaffolded %s into %s\n", result.Template, result.Target); err != nil {
			return err
		}
		if len(result.Files) == 0 {
			_, err := fmt.Fprintln(w, "No files were written.")
			return err
		}

		rows := make([][]string, 0, len(result.Files))
		for _, file := range result.Files {
			rows = append(rows, []string{file})
		}
		if err := output.WriteTable(w, []output.Column{{Title: "file"}}, rows); err != nil {
			return err
		}

		_, err := fmt.Fprintf(w, "\n%s file(s) written\n", output.Count(len(result.Files)))
		return err
	}
}
