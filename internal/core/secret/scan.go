package secret

import (
	"bufio"
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// Limits. A credential is a short string on a short line in a small file, and
// every one of these bounds exists so that a scan of a large tree finishes.
const (
	// maxFileBytes skips files no human wrote. A minified bundle or a
	// database dump is where a scanner spends its afternoon and finds nothing.
	maxFileBytes = 2 << 20
	// maxLineBytes is the longest line examined. Past this the line is
	// counted as skipped rather than truncated and half-matched.
	maxLineBytes = 8 << 10
	// maxFindingsPerFile stops one generated file producing ten thousand
	// findings and burying everything else.
	maxFindingsPerFile = 50
)

// excludedNames are skipped wherever they appear. Lock files are enormous and
// full of hashes that look like keys; the rest are directories no credential
// of the user's lives in.
var excludedNames = []string{
	".git", ".hg", ".svn", "node_modules", "vendor", "dist", "build", "target",
	"__pycache__", ".venv", "venv", ".terraform",
	"package-lock.json", "yarn.lock", "pnpm-lock.yaml", "Cargo.lock",
	"go.sum", "poetry.lock", "composer.lock", "Gemfile.lock",
}

// ScanRequest describes one working-tree scan.
type ScanRequest struct {
	// Root is the directory to scan. Empty means the current one.
	Root string
	// Rules narrows the run to named rules. Empty means every rule.
	Rules []string
	// Exclude are glob patterns matched against entry names, added to the
	// built-in list rather than replacing it.
	Exclude []string
	// Entropy overrides the floor every rule that has one uses. Zero means
	// each rule's own threshold.
	Entropy float64
	// IncludeTests scans the directories that hold test fixtures. They are
	// skipped by default because a fixture full of fake credentials is where
	// this kind of tool produces its worst noise.
	IncludeTests bool
}

// ScanResult is what a scan found.
type ScanResult struct {
	Root     string    `json:"root"`
	Findings []Finding `json:"findings"`
	Count    int       `json:"count"`
	// BySeverity counts the findings at each level, so a summary line needs no
	// second pass over the list.
	BySeverity map[string]int `json:"bySeverity"`
	// FilesScanned and FilesSkipped say how much of the tree was actually
	// looked at. A clean result over four files is not the same claim as a
	// clean result over four thousand.
	FilesScanned int `json:"filesScanned"`
	FilesSkipped int `json:"filesSkipped"`
	// Suppressed counts the lines that matched but carried the inline marker.
	Suppressed int `json:"suppressed"`
	// RulesUsed is how many detectors ran.
	RulesUsed int `json:"rulesUsed"`
}

// Scan searches a working tree for credential-shaped strings.
//
// Every file is read once, line by line, through a reused buffer. A credential
// lives on one line, so nothing here needs to hold a file, and a tree of any
// size costs the same memory as a small one.
//
// Binary files are skipped by looking at the first bytes rather than by
// extension: a file with a null byte in its head is not something a person
// typed a password into, and the extension list would be endless.
func Scan(ctx context.Context, reader Reader, request ScanRequest) (ScanResult, error) {
	root := strings.TrimSpace(request.Root)
	if root == "" {
		root = "."
	}

	resolved, err := reader.Resolve(root)
	if err != nil {
		return ScanResult{}, err
	}

	entry, err := reader.Stat(resolved)
	if err != nil {
		return ScanResult{}, err
	}
	if !entry.IsDir {
		return ScanResult{}, errors.New(errors.CodeInvalidInput,
			"%s is not a directory", resolved).
			WithHint("point this at a project directory")
	}

	active, missing := selected(request.Rules)
	if len(missing) > 0 {
		return ScanResult{}, errors.New(errors.CodeInvalidInput,
			"no rule named %s", strings.Join(missing, ", ")).
			WithHint("run \"devnest secret rules\" to see the names")
	}

	result := ScanResult{
		Root:       resolved,
		Findings:   []Finding{},
		BySeverity: map[string]int{},
		RulesUsed:  len(active),
	}

	exclude := append(append([]string{}, excludedNames...), request.Exclude...)
	if !request.IncludeTests {
		exclude = append(exclude, "testdata", "fixtures", "__fixtures__", "*.snap")
	}

	walk := fs.WalkOptions{
		Root:           resolved,
		IncludeHidden:  true,
		FollowSymlinks: false,
		Exclude:        exclude,
	}

	// One line buffer for the whole walk. A buffer per file cost 64 KiB of
	// allocation for every file scanned, which over ten thousand files was
	// more than half a gigabyte to read a few megabytes.
	buffer := make([]byte, maxLineBytes)

	err = reader.Walk(ctx, walk, func(file fs.Entry) error {
		if file.IsDir || file.IsSymlink {
			return nil
		}
		if file.Bytes > maxFileBytes {
			result.FilesSkipped++
			return nil
		}

		findings, suppressedHere, scanned, err := scanFile(ctx, reader, resolved, file, active, request.Entropy, buffer)
		if err != nil {
			return err
		}
		if !scanned {
			result.FilesSkipped++
			return nil
		}

		result.FilesScanned++
		result.Suppressed += suppressedHere
		result.Findings = append(result.Findings, findings...)
		return nil
	})
	if err != nil {
		return ScanResult{}, err
	}

	finish(&result)
	return result, nil
}

// finish sorts and counts, so every caller gets the same order.
func finish(result *ScanResult) {
	sort.Slice(result.Findings, func(first, second int) bool {
		left, right := result.Findings[first], result.Findings[second]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.Rule < right.Rule
	})

	for _, finding := range result.Findings {
		result.BySeverity[finding.Severity]++
	}
	result.Count = len(result.Findings)
}

// scanFile reads one file and reports what matched in it.
//
// The third return value says whether the file was examined at all: a binary
// file is skipped, and a skipped file is not a clean file.
func scanFile(ctx context.Context, reader Reader, root string, file fs.Entry, active []Rule,
	floor float64, buffer []byte) ([]Finding, int, bool, error) {
	handle, err := reader.Open(file.Path)
	if err != nil {
		// A file that cannot be read is not a failed scan. Permissions vary,
		// files are removed while a walk is running, and stopping the whole
		// run over one of them helps nobody.
		return nil, 0, false, nil
	}
	defer func() { _ = handle.Close() }()

	relative := file.Path
	if trimmed, err := filepath.Rel(root, file.Path); err == nil {
		relative = filepath.ToSlash(trimmed)
	}

	scanner := bufio.NewScanner(handle)
	scanner.Buffer(buffer, maxLineBytes)

	findings := make([]Finding, 0, 4)
	suppressedCount := 0
	previous := ""
	number := 0
	first := true

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, 0, false, errors.Wrap(err, errors.CodeCancelled, "cancelled")
		}

		line := scanner.Text()
		number++

		if first {
			first = false
			if binary(line) {
				return nil, 0, false, nil
			}
		}

		if len(findings) >= maxFindingsPerFile {
			break
		}

		matches := matchLine(line, active, floor)
		if len(matches) > 0 && suppressed(line, previous) {
			suppressedCount += len(matches)
			previous = line
			continue
		}

		for _, match := range matches {
			match.Path = relative
			match.Line = number
			findings = append(findings, match)
		}
		previous = line
	}

	// A line longer than the buffer ends the scan of this file. It is reported
	// as skipped rather than as clean, because the rest was never looked at.
	if err := scanner.Err(); err != nil {
		if len(findings) == 0 {
			return nil, suppressedCount, false, nil
		}
	}

	return findings, suppressedCount, true, nil
}

// matchLine runs the active rules against one line.
func matchLine(line string, active []Rule, floor float64) []Finding {
	findings := make([]Finding, 0, 2)

	for _, rule := range active {
		if rule.Keyword != "" && !strings.Contains(line, rule.Keyword) {
			continue
		}

		located := rule.compiled.FindStringSubmatchIndex(line)
		if located == nil {
			continue
		}

		value, column := captured(line, located, rule.group)
		if value == "" {
			continue
		}

		score := entropy(value)
		threshold := rule.Entropy
		if floor > 0 {
			threshold = floor
		}
		if threshold > 0 && score < threshold {
			continue
		}

		findings = append(findings, Finding{
			Rule:        rule.Name,
			Description: rule.Description,
			Severity:    rule.Severity,
			Column:      column,
			Redacted:    redact(value),
			Entropy:     score,
		})
	}

	return findings
}

// captured pulls the credential out of a match, using the rule's group when it
// has one.
func captured(line string, located []int, group int) (string, int) {
	start, end := located[0], located[1]

	if group > 0 && len(located) > 2*group+1 && located[2*group] >= 0 {
		start, end = located[2*group], located[2*group+1]
	}
	if start < 0 || end > len(line) || start >= end {
		return "", 0
	}

	return line[start:end], start + 1
}

// binary reports whether a file's first line looks like something no person
// typed. A null byte is the giveaway, and it is what every other tool uses.
func binary(head string) bool {
	return strings.IndexByte(head, 0) >= 0
}
