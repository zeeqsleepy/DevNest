package secret

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/proc"
)

// fakeGit returns a scripted patch stream.
type fakeGit struct {
	patch   string
	exit    int
	stderr  string
	missing bool

	args []string
}

func (f *fakeGit) Run(_ context.Context, command proc.Command) (proc.Output, error) {
	f.args = command.Args
	return proc.Output{Stdout: f.patch, Stderr: f.stderr, ExitCode: f.exit}, nil
}

func (f *fakeGit) Lookup(name string) []string {
	if f.missing || name != "git" {
		return nil
	}
	return []string{"/usr/bin/git"}
}

// patchStream builds the output the module asks git for: commit headers in the
// agreed format, file headers, and diff lines.
func patchStream(entries ...string) string {
	return strings.Join(entries, "\n") + "\n"
}

func commitHeader(hash, author, date string) string {
	return commitMarker + hash + "\x1f" + author + "\x1f" + date
}

func TestHistoryFindsCredentialsInAddedLines(t *testing.T) {
	system := &fakeGit{patch: patchStream(
		commitHeader("abc123", "Ana", "2026-03-01T09:00:00Z"),
		"--- a/config.yml",
		"+++ b/config.yml",
		"+aws_key: "+awsKeyID,
		commitHeader("def456", "Budi", "2026-04-01T09:00:00Z"),
		"--- a/config.yml",
		"+++ b/config.yml",
		"-aws_key: "+awsKeyID,
	)}

	result, err := History(context.Background(), system, HistoryRequest{})
	if err != nil {
		t.Fatalf("History: %v", err)
	}

	if result.Count != 1 {
		t.Fatalf("count = %d, want the one added line", result.Count)
	}
	finding := result.Findings[0]
	if finding.Commit != "abc123" || finding.Author != "Ana" {
		t.Errorf("finding = %+v, want the commit that introduced it", finding)
	}
	if finding.Path != "config.yml" {
		t.Errorf("path = %q, want the file from the diff header", finding.Path)
	}
	if result.Commits != 2 {
		t.Errorf("commits = %d, want both examined", result.Commits)
	}
}

// A removal line carries the same credential and is not a second leak. It is
// the same one, being taken out.
func TestHistoryIgnoresRemovedLines(t *testing.T) {
	system := &fakeGit{patch: patchStream(
		commitHeader("abc123", "Ana", "2026-03-01T09:00:00Z"),
		"+++ b/config.yml",
		"-token: "+githubToken,
	)}

	result, err := History(context.Background(), system, HistoryRequest{})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if result.Count != 0 {
		t.Errorf("findings = %+v, want removals ignored", result.Findings)
	}
}

// One credential added, reverted, and re-added appears in three patches. It is
// one problem, and reporting it three times buries everything else.
func TestHistoryReportsOneCredentialOnce(t *testing.T) {
	system := &fakeGit{patch: patchStream(
		commitHeader("aaa", "Ana", "2026-01-01T09:00:00Z"),
		"+++ b/config.yml",
		"+key: "+awsKeyID,
		commitHeader("bbb", "Ana", "2026-02-01T09:00:00Z"),
		"+++ b/config.yml",
		"+key: "+awsKeyID,
	)}

	result, err := History(context.Background(), system, HistoryRequest{})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if result.Count != 1 {
		t.Errorf("count = %d, want the same credential reported once", result.Count)
	}
}

func TestHistoryNeverCarriesTheCredential(t *testing.T) {
	system := &fakeGit{patch: patchStream(
		commitHeader("abc123", "Ana", "2026-03-01T09:00:00Z"),
		"+++ b/deploy.sh",
		"+export GITHUB_TOKEN="+githubToken,
	)}

	result, err := History(context.Background(), system, HistoryRequest{})
	if err != nil {
		t.Fatalf("History: %v", err)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), githubToken) {
		t.Error("the history result carries a credential in full")
	}
}

func TestHistoryHonoursInlineSuppression(t *testing.T) {
	system := &fakeGit{patch: patchStream(
		commitHeader("abc123", "Ana", "2026-03-01T09:00:00Z"),
		"+++ b/sample.go",
		`+key := "`+awsKeyID+`" // devnest:allow-secret`,
	)}

	result, err := History(context.Background(), system, HistoryRequest{})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if result.Count != 0 {
		t.Errorf("findings = %+v, want the marked line left alone", result.Findings)
	}
}

func TestHistoryBoundsItsDepthByDefault(t *testing.T) {
	system := &fakeGit{patch: ""}

	if _, err := History(context.Background(), system, HistoryRequest{}); err != nil {
		t.Fatalf("History: %v", err)
	}

	joined := strings.Join(system.args, " ")
	if !strings.Contains(joined, "--max-count=500") {
		t.Errorf("args = %q, want the default depth applied", joined)
	}

	// A negative depth means the whole history, which has to mean no cap at
	// all rather than a cap of zero commits.
	if _, err := History(context.Background(), system, HistoryRequest{Depth: -1}); err != nil {
		t.Fatalf("History: %v", err)
	}
	if joined := strings.Join(system.args, " "); strings.Contains(joined, "--max-count") {
		t.Errorf("args = %q, want no cap when the whole history was asked for", joined)
	}
}

func TestHistoryReportsAMissingGit(t *testing.T) {
	system := &fakeGit{missing: true}

	_, err := History(context.Background(), system, HistoryRequest{})
	assertCode(t, err, errors.CodeNotFound)
}

func TestHistoryReportsSomethingThatIsNotARepository(t *testing.T) {
	system := &fakeGit{exit: 128, stderr: "fatal: not a git repository\n"}

	_, err := History(context.Background(), system, HistoryRequest{})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestHistoryNarrowsToSelectedRules(t *testing.T) {
	system := &fakeGit{patch: patchStream(
		commitHeader("abc", "Ana", "2026-03-01T09:00:00Z"),
		"+++ b/a.txt",
		"+"+awsKeyID,
		"+"+githubToken,
	)}

	result, err := History(context.Background(), system, HistoryRequest{
		Rules: []string{"github-token"},
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}

	for _, finding := range result.Findings {
		if finding.Rule != "github-token" {
			t.Errorf("finding = %+v, want only the selected rule", finding)
		}
	}
}

func TestHistoryRejectsARuleThatDoesNotExist(t *testing.T) {
	_, err := History(context.Background(), &fakeGit{}, HistoryRequest{
		Rules: []string{"not-a-rule"},
	})
	assertCode(t, err, errors.CodeInvalidInput)
}
