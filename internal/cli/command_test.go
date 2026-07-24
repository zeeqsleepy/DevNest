package cli

import (
	"context"
	"flag"
	"strings"
	"testing"
)

// testTree exercises grouping and nesting without depending on which feature
// commands happen to exist.
func testTree() *Command {
	noop := func(context.Context, *Env, []string) error { return nil }

	root := &Command{
		Name: "devnest",
		Commands: []*Command{
			{
				Name: "config",
				Commands: []*Command{
					{Name: "list", Run: noop},
					{
						Name: "set",
						Run:  noop,
						SetFlags: func(set *flag.FlagSet) {
							set.String("scope", "user", "where to write")
							set.Bool("force", false, "overwrite")
						},
					},
				},
			},
			{Name: "version", Run: noop},
		},
	}
	root.link()
	return root
}

func TestSplitDescendsIntoGroups(t *testing.T) {
	var globals globalFlags
	got := split(testTree(), []string{"config", "set", "general.output", "json"}, &globals)

	if got.command.path() != "devnest config set" {
		t.Errorf("command = %q, want \"devnest config set\"", got.command.path())
	}
	if len(got.positional) != 2 || got.positional[0] != "general.output" {
		t.Errorf("positional = %v, want the two arguments", got.positional)
	}
}

func TestSplitSeparatesFlagsFromPositionals(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantCommand    string
		wantFlags      []string
		wantPositional []string
	}{
		{
			name:        "flags after the command",
			args:        []string{"version", "--output", "json"},
			wantCommand: "devnest version",
			wantFlags:   []string{"--output", "json"},
		},
		{
			name:        "flags before the command",
			args:        []string{"--output", "json", "version"},
			wantCommand: "devnest version",
			wantFlags:   []string{"--output", "json"},
		},
		{
			name:        "inline value",
			args:        []string{"version", "--output=json"},
			wantCommand: "devnest version",
			wantFlags:   []string{"--output=json"},
		},
		{
			name:        "boolean flag does not consume the next argument",
			args:        []string{"--verbose", "version"},
			wantCommand: "devnest version",
			wantFlags:   []string{"--verbose"},
		},
		{
			name:           "command flag consumes its value",
			args:           []string{"config", "set", "--scope", "user", "key"},
			wantCommand:    "devnest config set",
			wantFlags:      []string{"--scope", "user"},
			wantPositional: []string{"key"},
		},
		{
			name:           "double dash ends flag parsing",
			args:           []string{"version", "--", "--not-a-flag"},
			wantCommand:    "devnest version",
			wantPositional: []string{"--not-a-flag"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var globals globalFlags
			got := split(testTree(), test.args, &globals)

			if got.command.path() != test.wantCommand {
				t.Errorf("command = %q, want %q", got.command.path(), test.wantCommand)
			}
			if strings.Join(got.flagArgs, " ") != strings.Join(test.wantFlags, " ") {
				t.Errorf("flags = %v, want %v", got.flagArgs, test.wantFlags)
			}
			if strings.Join(got.positional, " ") != strings.Join(test.wantPositional, " ") {
				t.Errorf("positional = %v, want %v", got.positional, test.wantPositional)
			}
		})
	}
}

func TestSplitStopsDescendingAfterAPositional(t *testing.T) {
	// "version" here is an argument to config list, not a command to descend
	// into, because a positional has already been taken.
	var globals globalFlags
	got := split(testTree(), []string{"config", "list", "version"}, &globals)

	if got.command.path() != "devnest config list" {
		t.Errorf("command = %q, want \"devnest config list\"", got.command.path())
	}
	if len(got.positional) != 1 || got.positional[0] != "version" {
		t.Errorf("positional = %v, want [version]", got.positional)
	}
}

func TestSplitLeavesUnknownCommandsAsPositional(t *testing.T) {
	var globals globalFlags
	got := split(testTree(), []string{"frobnicate"}, &globals)

	if got.command.path() != "devnest" {
		t.Errorf("command = %q, want the root", got.command.path())
	}
	if len(got.positional) != 1 || got.positional[0] != "frobnicate" {
		t.Errorf("positional = %v, want [frobnicate]", got.positional)
	}
}

func TestUnknownCommandDetection(t *testing.T) {
	root := testTree()

	if err := unknownCommand(root, []string{"frobnicate"}); err == nil {
		t.Error("a leftover positional on a group must be a usage error")
	}
	if err := unknownCommand(root.find("version"), []string{"extra"}); err != nil {
		t.Errorf("a runnable command handles its own arguments: %v", err)
	}
	if err := unknownCommand(root, nil); err != nil {
		t.Errorf("a bare group invocation is not an error: %v", err)
	}
}

func TestPathIncludesEveryAncestor(t *testing.T) {
	root := testTree()
	set := root.find("config").find("set")

	if set.path() != "devnest config set" {
		t.Errorf("path = %q, want \"devnest config set\"", set.path())
	}
}

// Help text is the interface. A command without a description or without
// realistic examples is a command nobody can use without reading the source.
func TestEveryRunnableCommandIsDocumented(t *testing.T) {
	walk(t, NewRoot())
}

func walk(t *testing.T, command *Command) {
	t.Helper()

	if command.parent != nil {
		if command.Summary == "" {
			t.Errorf("%s: Summary is empty", command.path())
		}
		if command.Run != nil {
			if command.Description == "" {
				t.Errorf("%s: Description is empty", command.path())
			}
			if command.Usage == "" {
				t.Errorf("%s: Usage is empty", command.path())
			}
			if len(command.Examples) < 2 {
				t.Errorf("%s: has %d examples, want at least 2",
					command.path(), len(command.Examples))
			}
			for _, example := range command.Examples {
				// Contains rather than HasPrefix: the best example for a
				// command that reads standard input is a pipeline, and
				// "echo secret | devnest ..." is a full invocation with
				// something in front of it.
				if !strings.Contains(example.Command, "devnest ") {
					t.Errorf("%s: example %q should be a real invocation",
						command.path(), example.Command)
				}
				if example.Description == "" {
					t.Errorf("%s: example %q has no description",
						command.path(), example.Command)
				}
			}
		}
	}

	for _, sub := range command.Commands {
		walk(t, sub)
	}
}

// Registering the same flag name twice panics inside the flag package, and it
// only happens when the command runs. Building every flag set here turns that
// into a build failure instead of a crash in front of a user.
func TestEveryCommandBuildsItsFlagSet(t *testing.T) {
	var walkTree func(*Command)
	walkTree = func(command *Command) {
		t.Run(command.path(), func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("%s: %v", command.path(), recovered)
				}
			}()
			var globals globalFlags
			command.flagSet(&globals)
		})
		for _, sub := range command.Commands {
			walkTree(sub)
		}
	}
	walkTree(NewRoot())
}

// A shorthand has to mean the same thing as its long form, so a command cannot
// quietly reuse a letter the global flags already own.
func TestCommandFlagsDoNotShadowGlobalOnes(t *testing.T) {
	reserved := map[string]bool{
		"o": true, "output": true, "q": true, "quiet": true,
		"v": true, "verbose": true, "h": true, "help": true,
		"config": true, "no-color": true, "version": true,
	}

	var walkTree func(*Command)
	walkTree = func(command *Command) {
		if command.SetFlags != nil {
			set := flag.NewFlagSet(command.path(), flag.ContinueOnError)
			command.SetFlags(set)
			set.VisitAll(func(f *flag.Flag) {
				if reserved[f.Name] {
					t.Errorf("%s redefines the global flag --%s", command.path(), f.Name)
				}
			})
		}
		for _, sub := range command.Commands {
			walkTree(sub)
		}
	}
	walkTree(NewRoot())
}

func TestCommandNamesAreUniqueAndSpelledOut(t *testing.T) {
	root := NewRoot()
	seen := make(map[string]bool, len(root.Commands))

	for _, command := range root.Commands {
		if seen[command.Name] {
			t.Errorf("duplicate command name %q", command.Name)
		}
		seen[command.Name] = true

		if command.Name != strings.ToLower(command.Name) {
			t.Errorf("command %q should be lowercase", command.Name)
		}
		if strings.ContainsAny(command.Name, " _-") {
			t.Errorf("command %q should be a single plain word", command.Name)
		}
	}
}
