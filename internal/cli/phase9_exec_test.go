package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

// These run whole invocations in process, which is what covers the handlers
// themselves: the argument checking, the flag translation, and the exit code a
// gate returns. Anything that would touch the network or end a process is not
// here; that belongs in the end-to-end suite, where it can be done to a real
// listener the test owns.

func TestNewGroupsRejectBadArgumentsBeforeDoingAnything(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"two repositories", []string{"git", "a", "b"}},
		{"two projects to clean", []string{"clean", "a", "b"}},
		{"two directories to scan", []string{"secret", "scan", "a", "b"}},
		{"port with no number", []string{"port", "check"}},
		{"port out of range", []string{"port", "check", "70000"}},
		{"port that is not a number", []string{"port", "free", "http"}},
		{"arguments to a listing", []string{"port", "list", "3000"}},
		{"arguments to a rule listing", []string{"secret", "rules", "extra"}},
		{"arguments to a clean rule listing", []string{"clean", "rules", "extra"}},
		{"an invented severity", []string{"secret", "scan", ".", "--fail-on", "shouty"}},
		{"an invented clean pattern", []string{"clean", ".", "--pattern", "node_modlues"}},
		{"an invented secret rule", []string{"secret", "scan", ".", "--rule", "not-a-rule"}},
		{"nothing to test", []string{"secret", "test"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := exec(t, nil, testCase.args...)
			if got.err == nil {
				t.Fatalf("%v was accepted", testCase.args)
			}
			if code := errors.CodeOf(got.err); code != errors.CodeInvalidInput {
				t.Errorf("code = %q, want %q", code, errors.CodeInvalidInput)
			}
			if got.stdout != "" {
				t.Errorf("stdout = %q, want it empty when the input was rejected", got.stdout)
			}
		})
	}
}

func TestRuleListingsRun(t *testing.T) {
	for _, args := range [][]string{
		{"secret", "rules"},
		{"clean", "rules"},
	} {
		got := exec(t, nil, args...)
		if got.err != nil {
			t.Fatalf("%v: %v", args, got.err)
		}
		if strings.TrimSpace(got.stdout) == "" {
			t.Errorf("%v printed nothing", args)
		}
	}
}

func TestSecretScanRunsAndGatesOnSeverity(t *testing.T) {
	// A credential-shaped value that is famous for being fake.
	const key = "AKIA" + "IOSFODNN7EXAMPLE"

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "config.yml"), "aws_access_key_id: "+key+"\n")

	found := exec(t, nil, "secret", "scan", root)
	if found.err != nil {
		t.Fatalf("secret scan: %v", found.err)
	}
	if !strings.Contains(found.stdout, "aws-access-key-id") {
		t.Errorf("stdout = %q, want the finding", found.stdout)
	}
	if strings.Contains(found.stdout, key) {
		t.Error("the credential was printed in full")
	}

	gated := exec(t, nil, "secret", "scan", root, "--fail-on", "high")
	if gated.err == nil {
		t.Fatal("--fail-on high did not fail")
	}
	if code := errors.CodeOf(gated.err); code != errors.CodeCheckFailed {
		t.Errorf("code = %q, want %q", code, errors.CodeCheckFailed)
	}
}

func TestSecretTestRunsWithoutEchoingTheValue(t *testing.T) {
	const key = "ghp_" + "16C7e42F292c6912E7710c838347Ae178B4a"

	got := exec(t, nil, "secret", "test", key)
	if got.err != nil {
		t.Fatalf("secret test: %v", got.err)
	}
	if strings.Contains(got.stdout, key) {
		t.Error("the value was echoed back")
	}
	if !strings.Contains(got.stdout, "github-token") {
		t.Errorf("stdout = %q, want the matching rule named", got.stdout)
	}
}

// The dry run is the default and it has to stay that way. This asserts it from
// the outside of the handler: the tree is intact afterwards.
func TestCleanWithoutApplyLeavesTheTreeAlone(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{"name":"x"}`)
	writeFile(t, filepath.Join(root, "dist", "bundle.js"), "1\n")

	got := exec(t, nil, "clean", root)
	if got.err != nil {
		t.Fatalf("clean: %v", got.err)
	}
	if !strings.Contains(got.stdout, "Nothing has been deleted") {
		t.Errorf("stdout = %q, want the dry run stated", got.stdout)
	}
	if !exists(t, filepath.Join(root, "dist", "bundle.js")) {
		t.Error("a dry run removed a file")
	}
}

// writeFile creates a file and the directories above it.
func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create a directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func exists(t *testing.T, path string) bool {
	t.Helper()

	_, err := os.Stat(path)
	return err == nil
}

func TestGitReportsSomethingThatIsNotARepository(t *testing.T) {
	got := exec(t, nil, "git", t.TempDir())

	if got.err == nil {
		t.Fatal("a directory that is not a repository was accepted")
	}
	if code := errors.CodeOf(got.err); code != errors.CodeInvalidInput {
		t.Errorf("code = %q, want %q", code, errors.CodeInvalidInput)
	}
}
