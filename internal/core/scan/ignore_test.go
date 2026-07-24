package scan

import (
	"context"
	"strings"
	"testing"
)

func TestIgnoreRulesMatchTheWayGitDoes(t *testing.T) {
	rules := parseIgnore(strings.NewReader(`
# a comment, and the blank line above

build/
*.log
/devnest
docs/api
node_modules
!important.log
temp/**/cache
`))

	tests := []struct {
		path string
		dir  bool
		want bool
		why  string
	}{
		{"build", true, true, "a directory rule matches the directory"},
		{"lib/build", true, true, "an unanchored rule matches at any depth"},
		{"build.go", false, false, "a directory-only rule leaves files alone"},
		{"server.log", false, true, "a glob matches by name"},
		{"logs/server.log", false, true, "a glob matches at any depth"},
		{"important.log", false, false, "a later negation wins"},
		{"devnest", false, true, "an anchored rule matches at the root"},
		{"cmd/devnest", true, false, "an anchored rule does not match deeper"},
		{"docs/api", true, true, "a rule with a separator is anchored to the root"},
		{"other/docs/api", true, false, "and therefore does not match the same name elsewhere"},
		{"other/docs/build", true, true, "while a bare name still matches at any depth"},
		{"node_modules", true, true, "a bare name matches a directory"},
		{"temp/a/b/cache", true, true, "** crosses any number of directories"},
		{"src/main.go", false, false, "an ordinary file is not ignored"},
	}

	for _, test := range tests {
		if got := rules.matches(test.path, test.dir); got != test.want {
			t.Errorf("%s: matches(%q) = %v, want %v", test.why, test.path, got, test.want)
		}
	}
}

// This is the mistake that hides a source directory: "/devnest" means the one
// at the root, and a matcher that also accepted cmd/devnest would drop it out
// of every report without saying anything.
func TestAnchoredRuleDoesNotMatchDeeperPaths(t *testing.T) {
	rules := parseIgnore(strings.NewReader("/devnest\n/dist/\n"))

	if !rules.matches("devnest", false) {
		t.Error("the anchored rule did not match at the root")
	}
	if rules.matches("cmd/devnest", true) {
		t.Error("the anchored rule matched a directory below the root")
	}
	if rules.matches("internal/dist", true) {
		t.Error("the anchored directory rule matched below the root")
	}
}

func TestIgnoreFileIsOptional(t *testing.T) {
	fake := newFakeFS().with("main.go", "package main\n")

	rules := loadIgnore(context.Background(), fake, root())
	if rules == nil {
		t.Fatal("a tree with no .gitignore produced no rule set at all")
	}
	if rules.matches("main.go", false) {
		t.Error("a tree with no .gitignore ignored something")
	}
}

// The rules in a .gitignore are applied to the walk, not only to the report:
// a skipped directory is never descended into, which is where the time goes.
func TestIgnoreFileIsAppliedToTheWalk(t *testing.T) {
	fake := newFakeFS().
		with(".gitignore", "generated/\n*.tmp\n").
		with("main.go", "package main\n").
		with("scratch.tmp", "junk").
		with("generated/api.go", "package api\n")

	result, err := Summarize(context.Background(), fake, SummaryRequest{
		Selection: Selection{Root: root(), IncludeHidden: true},
	})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	// main.go and the .gitignore itself; the ignored file and the ignored
	// directory are gone.
	if result.Files != 2 {
		t.Errorf("files = %d, want 2", result.Files)
	}
}
