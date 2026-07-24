package env

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestVarsListsTheRelevantOnesByDefault(t *testing.T) {
	machine := newFakeMachine().
		withEnv("GOPATH", "/home/me/go").
		withEnv("EDITOR", "vim").
		withEnv("XDG_SESSION_CLASS", "user").
		withEnv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/bus")

	result, err := Vars(context.Background(), machine, VarsRequest{})
	if err != nil {
		t.Fatalf("Vars: %v", err)
	}

	if result.Total != 2 {
		t.Errorf("total = %d, want the two development variables: %+v",
			result.Total, result.Variables)
	}
	// Sorted by name, so two runs on one machine produce identical output.
	if result.Variables[0].Name != "EDITOR" {
		t.Errorf("first = %q, want EDITOR", result.Variables[0].Name)
	}

	all, err := Vars(context.Background(), machine, VarsRequest{All: true})
	if err != nil {
		t.Fatalf("Vars: %v", err)
	}
	if all.Total != 4 {
		t.Errorf("total with --all = %d, want 4", all.Total)
	}
}

func TestVarsFiltersByPattern(t *testing.T) {
	machine := newFakeMachine().
		withEnv("GOPATH", "/home/me/go").
		withEnv("GOROOT", "/usr/lib/go").
		withEnv("EDITOR", "vim")

	result, err := Vars(context.Background(), machine, VarsRequest{Pattern: "go"})
	if err != nil {
		t.Fatalf("Vars: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("total = %d, want the two GO variables", result.Total)
	}

	glob, err := Vars(context.Background(), machine, VarsRequest{Pattern: "go*"})
	if err != nil {
		t.Fatalf("Vars: %v", err)
	}
	if glob.Total != 2 {
		t.Errorf("total = %d, want the glob to match both", glob.Total)
	}
}

// The masking is a property of the result, not of one rendering of it. A
// listing gets redirected to a file and attached to a ticket, and masking the
// table while leaving the JSON readable would be a leak with a delay on it.
func TestVarsMasksCredentialsEverywhere(t *testing.T) {
	const token = "ghp_verysecretvalue" // devnest:allow-secret

	machine := newFakeMachine().
		withEnv("GITHUB_TOKEN", token).
		withEnv("AWS_SECRET_ACCESS_KEY", "abc123").
		withEnv("GOPATH", "/home/me/go")

	result, err := Vars(context.Background(), machine, VarsRequest{})
	if err != nil {
		t.Fatalf("Vars: %v", err)
	}
	if result.Masked != 2 {
		t.Errorf("masked = %d, want 2", result.Masked)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(encoded), token) {
		t.Fatalf("the serialised result contains the secret: %s", encoded)
	}
	// Not even a prefix. A prefix is enough to identify which key it is and,
	// for some formats, enough to start guessing the rest.
	if strings.Contains(string(encoded), "ghp_") {
		t.Errorf("the serialised result contains a prefix of the secret: %s", encoded)
	}
}

func TestVarsRevealsOnRequest(t *testing.T) {
	machine := newFakeMachine().withEnv("GITHUB_TOKEN", "ghp_verysecretvalue")

	result, err := Vars(context.Background(), machine, VarsRequest{Reveal: true})
	if err != nil {
		t.Fatalf("Vars: %v", err)
	}
	if result.Masked != 0 {
		t.Errorf("masked = %d, want none under --reveal", result.Masked)
	}
	if result.Variables[0].Value != "ghp_verysecretvalue" {
		t.Errorf("value = %q, want the real one", result.Variables[0].Value)
	}
}

// PATH is reported by its length rather than printed: two thousand characters
// in the middle of a table helps nobody.
func TestVarsCountsEntriesInAPathVariable(t *testing.T) {
	machine := newFakeMachine().withEnv("PATH", strings.Join([]string{
		"/usr/local/bin", "/usr/bin", "/bin",
	}, string(os.PathListSeparator)))

	result, err := Vars(context.Background(), machine, VarsRequest{})
	if err != nil {
		t.Fatalf("Vars: %v", err)
	}
	if result.Variables[0].Entries != 3 {
		t.Errorf("entries = %d, want 3", result.Variables[0].Entries)
	}
}
