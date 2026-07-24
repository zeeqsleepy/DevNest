package cli

import (
	"bufio"
	"io"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// newSecurityCommand builds the "security" group.
func newSecurityCommand() *Command {
	return &Command{
		Name:    "security",
		Summary: "Generate passwords, check strength, hash, verify, encode",
		Usage:   "devnest security <command> [input] [flags]",
		Description: "Defensive security utilities: generate a strong password, judge one, " +
			"compute a digest, verify a file against a published checksum, and encode " +
			"or decode Base64.\n\n" +
			"Everything here happens on this machine. Nothing is sent anywhere, nothing " +
			"is written to a file unless you redirect it yourself, and no password is " +
			"ever logged or included in a result.\n\n" +
			"Commands that take a secret accept --stdin, which is the safer way to " +
			"supply one: an argument on the command line is visible in your shell " +
			"history and, while the command runs, to every other process on the machine.",
		Commands: []*Command{
			newSecurityPasswordCommand(),
			newSecurityPasswordCheckCommand(),
			newSecurityHashCommand(),
			newSecurityChecksumCommand(),
			newSecurityEncodeCommand(),
			newSecurityDecodeCommand(),
		},
	}
}

// readSecret takes a value from an argument or from stdin.
//
// When it comes from an argument, the user is warned. That is not pedantry: an
// argument is recorded in shell history and is readable from the process table
// by anything running as the same user, so a password passed that way should be
// treated as disclosed. The warning goes to stderr, so it never contaminates
// piped output.
//
// The warning names the flag rather than only stating the problem, because a
// warning that does not say what to do instead is noise people learn to skip.
func readSecret(env *Env, args []string, useStdin bool, subject string) (string, error) {
	if useStdin {
		if len(args) > 0 {
			return "", errors.New(errors.CodeInvalidInput,
				"--stdin was given along with %s on the command line", subject).
				WithHint("pass it one way or the other")
		}
		return readAllStdin(env)
	}

	if len(args) == 0 {
		return "", errors.New(errors.CodeInvalidInput, "no %s was given", subject).
			WithHint("pass it as an argument, or use --stdin to read it from a pipe")
	}
	if len(args) > 1 {
		return "", errors.New(errors.CodeInvalidInput,
			"expected one %s, found %d", subject, len(args)).
			WithHint("quote it if it contains spaces")
	}

	env.Warn(errors.CodeInvalidInput,
		"a "+subject+" given on the command line is stored in your shell history "+
			"and is visible to other processes; prefer --stdin")

	return args[0], nil
}

// readAllStdin reads the whole of standard input.
//
// A trailing newline is stripped, because "echo secret | devnest ..." adds one
// and almost nobody means it to be part of the value. Anything else is left
// alone: a password may legitimately contain spaces.
func readAllStdin(env *Env) (string, error) {
	if env.Stdin == nil {
		return "", errors.New(errors.CodeInvalidInput, "there is nothing on standard input")
	}

	contents, err := io.ReadAll(bufio.NewReader(env.Stdin))
	if err != nil {
		return "", errors.Wrap(err, errors.CodeIO, "cannot read standard input")
	}

	value := strings.TrimRight(string(contents), "\r\n")
	if value == "" {
		return "", errors.New(errors.CodeInvalidInput, "standard input was empty").
			WithHint("pipe the value in, for example: echo secret | devnest security ...")
	}
	return value, nil
}
