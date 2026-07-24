package cli

import (
	"context"

	"github.com/devnest/devnest/internal/errors"
)

func newHelpCommand() *Command {
	return &Command{
		Name:    "help",
		Summary: "Show help for a command",
		Usage:   "devnest help [command...]",
		Description: "Show help for DevNest or for a specific command. " +
			"The same output is available as \"devnest <command> --help\".",
		Examples: []Example{
			{
				Command:     "devnest help",
				Description: "List the available commands.",
			},
			{
				Command:     "devnest help version",
				Description: "Show usage, flags, and examples for the version command.",
			},
		},
		Run: runHelp,
	}
}

func runHelp(_ context.Context, env *Env, args []string) error {
	command := NewRoot()

	for _, name := range args {
		sub := command.find(name)
		if sub == nil {
			return errors.New(errors.CodeInvalidInput,
				"unknown command %q", name).
				WithHint("run \"devnest help\" to see the available commands")
		}
		command = sub
	}

	return writeHelp(env.Stdout, command)
}
