package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

const tagline = "DevNest: a command line toolkit for routine development work."

// writeHelp renders help for a command. The root gets an index of groups; a
// command gets its full reference with examples.
func writeHelp(w io.Writer, command *Command) error {
	buffered := bufio.NewWriter(w)

	if command.parent == nil {
		writeRootHelp(buffered, command)
	} else {
		writeCommandHelp(buffered, command)
	}

	if err := buffered.Flush(); err != nil {
		return errors.Wrap(err, errors.CodeIO, "cannot write help output")
	}
	return nil
}

func writeRootHelp(w io.Writer, root *Command) {
	fmt.Fprintf(w, "%s\n\n", tagline)
	fmt.Fprintf(w, "Usage:\n  devnest <command> [arguments] [flags]\n\n")

	writeCommandList(w, root.Commands)
	writeFlagList(w, "Global flags", globalFlagHelp())

	fmt.Fprintf(w, "Run \"devnest help <command>\" for more information about a command.\n")
}

func writeCommandHelp(w io.Writer, command *Command) {
	if command.Description != "" {
		fmt.Fprintf(w, "%s\n\n", command.Description)
	} else if command.Summary != "" {
		fmt.Fprintf(w, "%s\n\n", command.Summary)
	}

	usage := command.Usage
	if usage == "" {
		usage = command.path() + " [flags]"
	}
	fmt.Fprintf(w, "Usage:\n  %s\n\n", usage)

	if len(command.Commands) > 0 {
		writeCommandList(w, command.Commands)
	}
	if len(command.Examples) > 0 {
		fmt.Fprintf(w, "Examples:\n")
		for _, example := range command.Examples {
			fmt.Fprintf(w, "  %s\n", example.Command)
			if example.Description != "" {
				fmt.Fprintf(w, "      %s\n", example.Description)
			}
		}
		fmt.Fprintln(w)
	}

	writeFlagList(w, "Global flags", globalFlagHelp())
}

func writeCommandList(w io.Writer, commands []*Command) {
	if len(commands) == 0 {
		return
	}
	fmt.Fprintf(w, "Commands:\n")
	width := 0
	for _, command := range commands {
		if len(command.Name) > width {
			width = len(command.Name)
		}
	}
	for _, command := range commands {
		fmt.Fprintf(w, "  %s%s%s\n", command.Name,
			strings.Repeat(" ", width-len(command.Name)+2), command.Summary)
	}
	fmt.Fprintln(w)
}

func writeFlagList(w io.Writer, title string, flags []flagHelp) {
	if len(flags) == 0 {
		return
	}
	fmt.Fprintf(w, "%s:\n", title)
	width := 0
	for _, item := range flags {
		if len(item.Name) > width {
			width = len(item.Name)
		}
	}
	for _, item := range flags {
		fmt.Fprintf(w, "  %s%s%s\n", item.Name,
			strings.Repeat(" ", width-len(item.Name)+2), item.Description)
	}
	fmt.Fprintln(w)
}
