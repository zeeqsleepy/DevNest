package secret

import (
	"context"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/proc"
)

// History scanning defaults.
const (
	// defaultDepth is how many commits back the scan reaches when the caller
	// does not say. Five hundred covers the recent history where a leaked
	// credential is still live; the whole history of an old repository is a
	// different question, asked with --depth 0 and answered slowly.
	defaultDepth = 500
	// historyLimit caps the patch text read back. Past this the scan reports
	// itself as truncated rather than quietly covering less than it claims.
	historyLimit = 64 << 20
	// historyTimeout bounds the git invocation.
	historyTimeout = 5 * time.Minute
)

// HistoryRequest describes a scan of a repository's history.
type HistoryRequest struct {
	// Root is any path inside the repository. Empty means the current
	// directory.
	Root string
	// Depth is how many commits to examine, newest first. Zero means
	// defaultDepth; a negative value means the whole history.
	Depth int
	// Rules and Entropy behave as they do for a working-tree scan.
	Rules   []string
	Entropy float64
}

// HistoryFinding is a finding plus where in the history it came from.
//
// A credential in history is a different problem from one in the working tree:
// removing it means rewriting history, and the first thing anybody needs to
// know is which commit introduced it and when.
type HistoryFinding struct {
	Finding
	Commit string    `json:"commit"`
	Author string    `json:"author"`
	Date   time.Time `json:"date"`
}

// HistoryResult is what a history scan found.
type HistoryResult struct {
	Root     string           `json:"root"`
	Findings []HistoryFinding `json:"findings"`
	Count    int              `json:"count"`

	BySeverity map[string]int `json:"bySeverity"`
	// Commits is how many commits were examined.
	Commits int `json:"commits"`
	// Truncated says the patch text hit the size limit, so the scan covered
	// less than the depth asked for. A partial scan reported as complete is
	// the worst outcome this command has.
	Truncated bool `json:"truncated"`
	RulesUsed int  `json:"rulesUsed"`
}

// History scans the patches in a repository's history for credentials.
//
// Added lines only. A credential that was removed in a later commit is still in
// the history and still leaked, which is the entire reason this command exists;
// what is not interesting is the removal line, which would otherwise report the
// same credential a second time.
//
// This is slower than the working-tree scan by a wide margin, which is why it
// is a separate command rather than a flag: a pre-commit hook wants the tree,
// and someone auditing a repository wants this.
func History(ctx context.Context, runner Runner, request HistoryRequest) (HistoryResult, error) {
	if len(runner.Lookup("git")) == 0 {
		return HistoryResult{}, errors.New(errors.CodeNotFound, "git is not on PATH").
			WithHint("scanning history means reading the patches git stores; " +
				"the working tree scan needs no git and is \"devnest secret scan\"")
	}

	active, missing := selected(request.Rules)
	if len(missing) > 0 {
		return HistoryResult{}, errors.New(errors.CodeInvalidInput,
			"no rule named %s", strings.Join(missing, ", ")).
			WithHint("run \"devnest secret rules\" to see the names")
	}

	root := strings.TrimSpace(request.Root)
	if root == "" {
		root = "."
	}

	args := []string{
		"-c", "color.ui=false", "--no-pager", "log", "--all", "--no-merges",
		"--patch", "--unified=0", "--no-color",
		"--format=" + commitMarker + "%H\x1f%an\x1f%cI",
	}
	if depth := request.Depth; depth >= 0 {
		if depth == 0 {
			depth = defaultDepth
		}
		args = append(args, "--max-count="+itoa(depth))
	}

	output, err := runner.Run(ctx, proc.Command{
		Name:    "git",
		Args:    args,
		Dir:     root,
		Timeout: historyTimeout,
		Limit:   historyLimit,
	})
	if err != nil {
		return HistoryResult{}, err
	}
	if output.ExitCode != 0 {
		message := strings.TrimSpace(output.Stderr)
		if message == "" {
			message = "git log failed"
		}
		return HistoryResult{}, errors.New(errors.CodeInvalidInput, "%s", firstLine(message)).
			WithHint("this command reads a repository's history; run it inside one")
	}

	return scanPatch(output.Stdout, request, active, root)
}

// commitMarker introduces a commit header in the patch stream. It has to be
// something no line of a diff can begin with, and a null byte cannot be passed
// in an argument, so this is a marker chosen for being absurd rather than for
// being elegant.
const commitMarker = "\x1fdevnest-commit\x1f"

// scanPatch reads the patch stream one line at a time.
//
// The stream is a sequence of commit headers, file headers, and diff lines.
// Everything needed is on the line in hand: which commit, which file, and
// whether this is an added line. Nothing is held except the current headers.
func scanPatch(patch string, request HistoryRequest, active []Rule, root string) (HistoryResult, error) {
	result := HistoryResult{
		Root:       root,
		Findings:   []HistoryFinding{},
		BySeverity: map[string]int{},
		RulesUsed:  len(active),
		Truncated:  len(patch) >= historyLimit,
	}

	var commit, author, path string
	var when time.Time
	previous := ""
	seen := make(map[string]bool, 64)

	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimRight(line, "\r")

		switch {
		case strings.HasPrefix(line, commitMarker):
			commit, author, when = parseCommitHeader(strings.TrimPrefix(line, commitMarker))
			result.Commits++
			path = ""
			previous = ""
			continue

		case strings.HasPrefix(line, "+++ b/"):
			path = strings.TrimPrefix(line, "+++ b/")
			previous = ""
			continue

		case !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++"):
			continue
		}

		content := line[1:]
		matches := matchLine(content, active, request.Entropy)
		if len(matches) == 0 {
			previous = content
			continue
		}
		if suppressed(content, previous) {
			previous = content
			continue
		}

		for _, match := range matches {
			// One credential committed once and reverted twice appears in
			// three patches. Reporting it three times turns a two-line result
			// into a wall, and the first commit is the one that matters.
			key := match.Rule + "\x00" + match.Redacted + "\x00" + path
			if seen[key] {
				continue
			}
			seen[key] = true

			match.Path = path
			result.Findings = append(result.Findings, HistoryFinding{
				Finding: match,
				Commit:  commit,
				Author:  author,
				Date:    when,
			})
		}
		previous = content
	}

	for _, finding := range result.Findings {
		result.BySeverity[finding.Severity]++
	}
	result.Count = len(result.Findings)

	return result, nil
}

// parseCommitHeader reads the three fields the format string asked for.
func parseCommitHeader(header string) (commit, author string, when time.Time) {
	parts := strings.Split(header, "\x1f")
	if len(parts) > 0 {
		commit = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		author = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[2])); err == nil {
			when = parsed
		}
	}
	return commit, author, when
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return strings.TrimSpace(text[:index])
	}
	return strings.TrimSpace(text)
}
