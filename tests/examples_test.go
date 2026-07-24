//go:build e2e

// Every "devnest ..." line in the documentation is checked against the real
// binary here, so a renamed command or flag cannot survive in an example
// somebody is going to copy.
//
// The check runs each example with --help appended. That resolves the command
// path and parses every flag, and stops before the command does anything, so a
// destructive example, one that would open a socket, and one whose paths do not
// exist on this machine are all checked the same way as the rest.
//
// Run with: make test-e2e
package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// example is one invocation found in the documentation.
type example struct {
	file string
	line int
	args []string
	text string
}

func TestDocumentedExamplesAreReal(t *testing.T) {
	examples := collectExamples(t)
	if len(examples) < 50 {
		t.Fatalf("found only %d examples; the harvester is probably broken", len(examples))
	}

	for _, found := range examples {
		name := strings.ReplaceAll(found.text, " ", "_")
		t.Run(name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(config, nil, 0o600); err != nil {
				t.Fatalf("write empty configuration: %v", err)
			}

			args := append(append([]string{}, found.args...), "--help", "--config", config)
			command := exec.Command(binaryPath(t), args...)
			output, err := command.CombinedOutput()

			if err != nil {
				t.Errorf("%s:%d: %s\n%s", found.file, found.line, found.text, output)
			}
		})
	}
}

// collectExamples reads every documented invocation out of the fenced code
// blocks in the documentation.
func collectExamples(t *testing.T) []example {
	t.Helper()

	root := moduleRoot(t)
	files := []string{filepath.Join(root, "README.md")}

	entries, err := os.ReadDir(filepath.Join(root, "docs"))
	if err != nil {
		t.Fatalf("read docs: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".md") {
			files = append(files, filepath.Join(root, "docs", entry.Name()))
		}
	}

	var examples []example
	for _, path := range files {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		fenced := false
		for index, line := range strings.Split(string(contents), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				fenced = !fenced
				continue
			}
			if !fenced {
				continue
			}
			args, ok := parseExample(line)
			if !ok {
				continue
			}
			examples = append(examples, example{
				file: filepath.Base(path),
				line: index + 1,
				args: args,
				text: strings.Join(args, " "),
			})
		}
	}
	return examples
}

// parseExample turns one documented line into an argument vector, or reports
// that it is not an invocation this test can check.
//
// A line carrying a placeholder is skipped: "devnest <command> --help" is a
// description of the grammar rather than something anybody can run.
func parseExample(line string) ([]string, bool) {
	text := strings.TrimSpace(line)

	// Prompts, pipelines, and redirects: keep the segment that runs devnest.
	text = strings.TrimPrefix(text, "$ ")
	text = strings.TrimPrefix(text, "PS> ")
	if index := strings.Index(text, " > "); index >= 0 {
		text = text[:index]
	}
	if index := strings.Index(text, " | "); index >= 0 {
		for _, segment := range strings.Split(text, " | ") {
			if strings.Contains(segment, "devnest ") {
				text = segment
				break
			}
		}
	}
	text = strings.TrimSpace(text)

	if !strings.HasPrefix(text, "devnest ") {
		return nil, false
	}
	if strings.ContainsAny(text, "<>[]{}$") || strings.Contains(text, "...") {
		return nil, false
	}
	if index := strings.Index(text, " # "); index >= 0 {
		text = strings.TrimSpace(text[:index])
	}

	fields := split(text)
	if len(fields) < 2 {
		return nil, false
	}
	return fields[1:], true
}

// split honours double quotes, so an argument with a space in it stays one
// argument the way a shell would pass it.
func split(text string) []string {
	var fields []string
	var current strings.Builder
	quoted := false

	for _, character := range text {
		switch {
		case character == '"':
			quoted = !quoted
		case character == ' ' && !quoted:
			if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(character)
		}
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	return fields
}
