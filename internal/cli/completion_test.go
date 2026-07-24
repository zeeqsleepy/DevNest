package cli

import (
	"strings"
	"testing"
)

// The scripts are generated from the command tree, so the test that matters is
// that the tree arrives intact: a nested command and its own flags, not just
// the top level.
func TestCompletionNodesCoverNestedCommands(t *testing.T) {
	nodes := completionNodes(NewRoot())

	paths := make(map[string]completionNode, len(nodes))
	for _, node := range nodes {
		paths[node.Path] = node
	}

	for _, want := range []string{"devnest", "devnest secret", "devnest secret scan"} {
		if _, ok := paths[want]; !ok {
			t.Fatalf("no node for %q", want)
		}
	}

	if got := strings.Join(paths["devnest secret"].names(), " "); !strings.Contains(got, "history") {
		t.Errorf("subcommands of secret = %q, want it to contain %q", got, "history")
	}
	if got := strings.Join(paths["devnest secret scan"].Flags, " "); !strings.Contains(got, "--fail-on") {
		t.Errorf("flags of secret scan = %q, want it to contain %q", got, "--fail-on")
	}
}

// Global flags belong to every command and are carried once per script rather
// than repeated in each entry of the table.
func TestGlobalFlagsAreNotRepeatedPerCommand(t *testing.T) {
	globals := globalFlagNames()

	if len(globals) == 0 {
		t.Fatal("no global flags")
	}
	for _, node := range completionNodes(NewRoot()) {
		for _, flag := range node.Flags {
			if flag == "--output" {
				t.Errorf("%s repeats the global flag %s", node.Path, flag)
			}
		}
	}
}

func TestCompletionScriptsCarryTheirShellHook(t *testing.T) {
	nodes := completionNodes(NewRoot())
	globals := globalFlagNames()

	cases := []struct {
		shell    string
		generate func([]completionNode, []string) string
		wants    []string
	}{
		{"bash", bashCompletion, []string{
			"complete -F _devnest devnest", "secret", "--fail-on", "--output",
		}},
		{"zsh", zshCompletion, []string{
			"#compdef devnest", "secret", "--fail-on", "--output",
		}},
		{"fish", fishCompletion, []string{
			"complete -c devnest -n '__fish_use_subcommand' -a 'secret'", "-l fail-on", "-l output",
		}},
		{"powershell", powershellCompletion, []string{
			"Register-ArgumentCompleter -Native -CommandName devnest", "'secret'", "'--fail-on'", "'--output'",
		}},
	}

	for _, testCase := range cases {
		t.Run(testCase.shell, func(t *testing.T) {
			script := testCase.generate(nodes, globals)

			for _, want := range testCase.wants {
				if !strings.Contains(script, want) {
					t.Errorf("%s script does not contain %q", testCase.shell, want)
				}
			}
		})
	}
}

// "scan" is a group of its own and a subcommand of "secret", so a condition
// testing only the last word would offer secret's flags inside "devnest scan".
func TestFishConditionTestsEveryWordOfThePath(t *testing.T) {
	got := fishCondition("devnest secret scan")
	want := "__fish_seen_subcommand_from secret; and __fish_seen_subcommand_from scan"

	if got != want {
		t.Errorf("fishCondition = %q, want %q", got, want)
	}
}

// A quote in a summary would otherwise end the string early and leave the rest
// of the line as shell.
func TestQuotingClosesNothingEarly(t *testing.T) {
	if got, want := fishQuote("don't"), `'don\'t'`; got != want {
		t.Errorf("fishQuote = %q, want %q", got, want)
	}
	if got, want := powershellQuote("don't"), "'don''t'"; got != want {
		t.Errorf("powershellQuote = %q, want %q", got, want)
	}
}
