package cli

import (
	"context"
	"flag"
	"io"
	"sort"

	"github.com/devnest/devnest/internal/errors"
)

// newCompletionCommand builds the "completion" group: one subcommand per
// supported shell, each printing a script to stdout for the user to redirect
// into their profile. Installation instructions are in docs/installation.md.
func newCompletionCommand() *Command {
	return &Command{
		Name:    "completion",
		Summary: "Print a shell completion script",
		Usage:   "devnest completion <shell>",
		Description: "Print the completion script for a shell. The script is written to " +
			"stdout: nothing is installed, and no file on the machine is touched.\n\n" +
			"The script is generated from the command tree in this binary, so it " +
			"completes exactly the commands and flags this version has. Regenerate it " +
			"after an upgrade.",
		Examples: []Example{
			{
				Command:     "devnest completion bash > /etc/bash_completion.d/devnest",
				Description: "Install completion for every bash user on the machine.",
			},
			{
				Command:     "devnest completion powershell | Out-String | Invoke-Expression",
				Description: "Load completion into the current PowerShell session.",
			},
		},
		Commands: []*Command{
			newCompletionShellCommand("powershell", "PowerShell", powershellCompletion),
			newCompletionShellCommand("bash", "bash", bashCompletion),
			newCompletionShellCommand("zsh", "zsh", zshCompletion),
			newCompletionShellCommand("fish", "fish", fishCompletion),
		},
	}
}

// completionScript is what the command returns: the shell it is for, and the
// script itself. A machine reading JSON gets the script as a field; a person
// gets the script on its own, which is what a redirect into a profile needs.
type completionScript struct {
	Shell  string `json:"shell"`
	Script string `json:"script"`
}

func newCompletionShellCommand(name, label string, generate func([]completionNode, []string) string) *Command {
	return &Command{
		Name:        name,
		Summary:     "Print the " + label + " completion script",
		Usage:       "devnest completion " + name + " [flags]",
		Description: "Print the " + label + " completion script to stdout.",
		Examples: []Example{
			{
				Command:     "devnest completion " + name,
				Description: "Print the script, to read it before installing it.",
			},
			{
				Command:     "devnest completion " + name + " --output json",
				Description: "The same script as a JSON field, for a packaging step.",
			},
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			if len(args) > 0 {
				return errors.New(errors.CodeInvalidInput,
					"completion %s takes no arguments, found %q", name, args[0]).
					WithHint("run \"devnest completion --help\" for usage")
			}

			script := generate(completionNodes(NewRoot()), globalFlagNames())
			return env.Emit(completionScript{Shell: name, Script: script},
				func(w io.Writer) error {
					if _, err := io.WriteString(w, script); err != nil {
						return errors.Wrap(err, errors.CodeIO, "cannot write completion script")
					}
					return nil
				})
		},
	}
}

// completionNode is one command flattened to what a shell needs: the words that
// select it, what may follow them, and its own flags.
type completionNode struct {
	Path     string
	Children []completionChild
	Flags    []string
}

type completionChild struct {
	Name    string
	Summary string
}

// names returns the child command names, which is all the shells other than
// fish have anywhere to put.
func (n completionNode) names() []string {
	names := make([]string, 0, len(n.Children))
	for _, child := range n.Children {
		names = append(names, child.Name)
	}
	return names
}

// completionNodes flattens the command tree, parents before children, so a
// generated script can be read alongside "devnest help".
func completionNodes(root *Command) []completionNode {
	var nodes []completionNode

	var walk func(command *Command)
	walk = func(command *Command) {
		node := completionNode{Path: command.path(), Flags: commandFlagNames(command)}
		for _, sub := range command.Commands {
			node.Children = append(node.Children, completionChild{Name: sub.Name, Summary: sub.Summary})
		}
		nodes = append(nodes, node)

		for _, sub := range command.Commands {
			walk(sub)
		}
	}
	walk(root)

	return nodes
}

// commandFlagNames returns a command's own flags. The global ones are left out
// because they apply everywhere and each script carries them once.
func commandFlagNames(command *Command) []string {
	if command.SetFlags == nil {
		return nil
	}

	set := flag.NewFlagSet(command.Name, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	command.SetFlags(set)
	return flagNames(set)
}

func globalFlagNames() []string {
	set := flag.NewFlagSet("devnest", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	(&globalFlags{}).register(set)
	return flagNames(set)
}

func flagNames(set *flag.FlagSet) []string {
	var names []string
	set.VisitAll(func(f *flag.Flag) {
		dashes := "--"
		if len(f.Name) == 1 {
			dashes = "-"
		}
		names = append(names, dashes+f.Name)
	})
	sort.Strings(names)
	return names
}
