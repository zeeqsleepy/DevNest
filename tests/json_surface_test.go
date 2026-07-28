//go:build e2e

// The JSON surface of every command, recorded field by field.
//
// docs/release-process.md counts JSON output field names, types, and structure
// as public surface, which means renaming a field after 1.0 is a major release.
// A promise nothing checks is a promise somebody breaks by accident, so this
// test runs each command against a fixed fixture, reduces its JSON to a sorted
// list of "path: type" lines, and compares that against testdata/json-surface.txt.
//
// Values are deliberately not recorded. A duration, a temporary path, and a
// generated password differ on every run and none of them are the contract;
// the shape is. An empty list records the list and not its elements, so the few
// arrays no fixture fills — "problems", and the envelope's own "warnings" on a
// clean run — are frozen by whichever case does produce them.
//
// A deliberate change is reviewed as a diff of the golden file:
//
//	go test -tags=e2e ./tests -run TestJSONSurface -update-surface
//
// Not covered here, each for a reason: the network group, which would need an
// outbound connection; the commands that print a document rather than an
// envelope (json format, minify, query, the converters), whose shape is the
// user's document; anything that removes data; secret history, which reads the
// whole repository history and belongs in a test that can be slow; and the
// commands whose result is a description of the machine rather than of an input
// (env list, env vars), whose field set depends on what happens to be installed.
//
// Run with: make test-e2e
package tests

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var updateSurface = flag.Bool("update-surface", false, "rewrite testdata/json-surface.txt from this run")

const surfaceGolden = "json-surface.txt"

// surfaceCases are the invocations whose JSON shape is frozen. Placeholders are
// replaced with fixture paths: {tree} a small project, {log} an access log,
// {json} and {yaml} documents, {file} one file inside the tree, {repo} this
// repository, {digest} the SHA-256 of {file}.
var surfaceCases = []struct {
	name string
	args []string
}{
	{"error envelope", []string{"security", "checksum", "{file}", "not-a-digest"}},

	{"version", []string{"version"}},
	{"doctor", []string{"doctor"}},

	{"clean", []string{"clean", "{tree}"}},
	{"clean rules", []string{"clean", "rules"}},

	{"config list", []string{"config", "list"}},
	{"config get", []string{"config", "get", "general.output"}},
	{"config path", []string{"config", "path"}},
	{"config validate", []string{"config", "validate"}},

	{"encode hex", []string{"encode", "hex", "devnest"}},
	{"encode url", []string{"encode", "url", "a b"}},
	{"decode hex", []string{"decode", "hex", "6465766e657374"}},
	{"decode url", []string{"decode", "url", "a%20b"}},
	{"decode jwt", []string{"decode", "jwt", "{jwt}"}},

	{"env path", []string{"env", "path"}},
	{"env which", []string{"env", "which", "go"}},

	{"file duplicate", []string{"file", "duplicate", "{tree}"}},
	{"file filter", []string{"file", "filter", "{tree}", "--extension", "go"}},
	{"file size", []string{"file", "size", "{tree}"}},
	{"file hash", []string{"file", "hash", "{file}"}},

	{"git", []string{"git", "{repo}"}},
	{"git branches", []string{"git", "branches", "{repo}"}},
	{"git stale", []string{"git", "stale", "{repo}", "--days", "0"}},
	{"git contributors", []string{"git", "contributors", "{repo}"}},
	{"git large", []string{"git", "large", "{repo}"}},

	{"log analyze", []string{"log", "analyze", "{log}"}},
	{"log http", []string{"log", "http", "{log}"}},
	{"log errors", []string{"log", "errors", "{log}"}},
	{"log status", []string{"log", "status", "{log}"}},
	{"log top", []string{"log", "top", "{log}"}},
	{"log search", []string{"log", "search", "{log}", "GET"}},
	{"log stats", []string{"log", "stats", "{log}"}},

	{"port check", []string{"port", "check", "59321"}},

	{"scan", []string{"scan", "{tree}"}},
	{"scan types", []string{"scan", "types", "{tree}"}},
	{"scan lines", []string{"scan", "lines", "{tree}"}},
	{"scan tree", []string{"scan", "tree", "{tree}"}},

	{"secret scan", []string{"secret", "scan", "{tree}"}},
	{"secret rules", []string{"secret", "rules"}},
	{"secret test", []string{"secret", "test", "{credential}"}},

	{"security password", []string{"security", "password"}},
	{"security password-check", []string{"security", "password-check", "hunter2"}},
	{"security hash", []string{"security", "hash", "devnest"}},
	{"security checksum", []string{"security", "checksum", "{file}", "{digest}"}},
	{"security encode", []string{"security", "encode", "devnest"}},
	{"security decode", []string{"security", "decode", "ZGV2bmVzdA=="}},
}

// surfaceFailures are the cases meant to fail, so that the shape of a failure
// is frozen too: a script branching on error.code depends on it.
var surfaceFailures = map[string]bool{"error envelope": true}

func TestJSONSurface(t *testing.T) {
	replacements := surfaceFixture(t)
	golden := readSurfaceGolden(t)
	recorded := make(map[string]string, len(surfaceCases))

	for _, testCase := range surfaceCases {
		args := make([]string, 0, len(testCase.args)+2)
		for _, argument := range testCase.args {
			args = append(args, expandPlaceholders(argument, replacements))
		}
		args = append(args, "--output", "json")

		run := runBinary(t, args...)
		var decoded any
		if err := json.Unmarshal([]byte(run.stdout), &decoded); err != nil {
			t.Errorf("%s: stdout is not JSON (exit %d): %v\n%s", testCase.name, run.exitCode, err, run.stdout)
			continue
		}

		// A failed command has a shape too, and it is the same one for every
		// command. Recording it would freeze nothing and hide a broken fixture.
		if envelope, ok := decoded.(map[string]any); ok && (envelope["status"] == "error") != surfaceFailures[testCase.name] {
			t.Errorf("%s: status %q, wanted failing=%v:\n%s", testCase.name, envelope["status"], surfaceFailures[testCase.name], run.stdout)
			continue
		}

		paths := map[string]bool{}
		collectShape(decoded, "", paths)
		surface := renderShape(paths)
		recorded[testCase.name] = surface

		if *updateSurface {
			continue
		}
		expected, ok := golden[testCase.name]
		if !ok {
			t.Errorf("%s: not in %s; rerun with -update-surface if the command is new", testCase.name, surfaceGolden)
			continue
		}
		if surface != expected {
			t.Errorf("%s: JSON surface changed.\nwant:\n%s\ngot:\n%s\n\nIf this is deliberate it is a MAJOR change before removal or a rename; see docs/release-process.md. Rerun with -update-surface to record it.",
				testCase.name, expected, surface)
		}
	}

	if *updateSurface {
		writeSurfaceGolden(t, recorded)
		return
	}
	for name := range golden {
		if _, ok := recorded[name]; !ok {
			t.Errorf("%s is recorded in %s but no longer covered; a removed command is a MAJOR change", name, surfaceGolden)
		}
	}
}

// collectShape reduces a decoded document to the set of "path: type" lines that
// describe it. Array elements collapse onto one path, so a listing's shape does
// not depend on how many rows the fixture happened to produce.
func collectShape(value any, path string, into map[string]bool) {
	record := func(kind string) {
		if path == "" {
			path = "$" // the envelope itself, named the way a path expression would
		}
		into[path+": "+kind] = true
	}

	switch typed := value.(type) {
	case map[string]any:
		record("object")
		for key, element := range typed {
			collectShape(element, join(path, key), into)
		}
	case []any:
		record("array")
		for _, element := range typed {
			collectShape(element, path+"[]", into)
		}
	case string:
		record("string")
	case float64:
		record("number")
	case bool:
		record("boolean")
	case nil:
		record("null")
	}
}

func join(path, key string) string {
	if path == "" || path == "$" {
		return key
	}
	return path + "." + key
}

func renderShape(paths map[string]bool) string {
	lines := make([]string, 0, len(paths))
	for line := range paths {
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// surfaceFixture builds the tree, the log, and the documents every case runs
// against, and returns the placeholder replacements for them.
func surfaceFixture(t *testing.T) map[string]string {
	t.Helper()

	root := t.TempDir()
	tree := filepath.Join(root, "tree")
	write := func(relative, contents string) string {
		path := filepath.Join(tree, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", relative, err)
		}
		return path
	}

	// A credential the scanner recognises: the AWS key prefix and enough entropy
	// to clear the rule's floor, assembled at run time so that this source file
	// does not itself read as a leak to any scanner, including ours.
	credential := "AKIA" + "J5X2QW" + "7T4NPL" + "9BVR"

	file := write("src/app.go", "package main\n\n// entry point\nfunc main() {\n\tprintln(\"hello\")\n}\n")
	write("src/util.go", "package main\n\nfunc helper() int {\n\treturn 1\n}\n")
	write("docs/readme.md", "# Fixture\n\nA tree with a known shape.\n")
	write("copy-a.txt", "identical contents\n")
	write("copy-b.txt", "identical contents\n")
	write("package.json", "{\"name\":\"fixture\",\"version\":\"1.0.0\"}\n")
	write("build/output.js", "console.log(1);\n")
	write("config/deploy.env", "AWS_ACCESS_KEY_ID="+credential+"\n")

	log := filepath.Join(root, "access.log")
	var lines strings.Builder
	for index := 0; index < 40; index++ {
		status := []string{"200", "200", "301", "404", "500"}[index%5]
		path := []string{"/", "/api/users", "/api/orders", "/static/app.css"}[index%4]
		fmt.Fprintf(&lines, "10.0.0.%d - - [28/Jul/2026:10:%02d:00 +0000] \"GET %s HTTP/1.1\" %s 512 \"-\" \"fixture/1.0\"\n",
			index%8, index, path, status)
	}
	if err := os.WriteFile(log, []byte(lines.String()), 0o600); err != nil {
		t.Fatalf("write fixture log: %v", err)
	}

	document := filepath.Join(root, "doc.json")
	if err := os.WriteFile(document, []byte(`{"name":"fixture","tags":["a","b"],"count":2}`), 0o600); err != nil {
		t.Fatalf("write fixture document: %v", err)
	}

	// The SHA-256 of the file above, so security checksum verifies rather than
	// reports a mismatch. Both are results; the successful one has more fields.
	digest := runBinary(t, "file", "hash", file, "--output", "json")
	var hashed struct {
		Data struct {
			Files []struct {
				Checksums []struct {
					Algorithm string `json:"algorithm"`
					Value     string `json:"value"`
				} `json:"checksums"`
			} `json:"files"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(digest.stdout), &hashed); err != nil || len(hashed.Data.Files) == 0 {
		t.Fatalf("read the fixture digest: %v\n%s", err, digest.stdout)
	}
	sum := ""
	for _, checksum := range hashed.Data.Files[0].Checksums {
		if strings.EqualFold(checksum.Algorithm, "sha256") {
			sum = checksum.Value
		}
	}
	if sum == "" {
		t.Fatalf("no sha256 checksum in:\n%s", digest.stdout)
	}

	return map[string]string{
		"{tree}":       tree,
		"{file}":       file,
		"{log}":        log,
		"{json}":       document,
		"{repo}":       surfaceRepository(t, root),
		"{digest}":     sum,
		"{credential}": credential,
		// The JWT from RFC 7519's example, which carries no signature anyone
		// can use and decodes to a header and three claims.
		"{jwt}": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
			"eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ." +
			"SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
	}
}

// surfaceRepository builds a repository with two commits on two branches. This
// checkout would have served, except that its clone depth is CI's decision and
// a shallow one has no contributors, no branches, and no large objects to
// report: the shape would then depend on how the runner fetched the code.
func surfaceRepository(t *testing.T, root string) string {
	t.Helper()

	repository := filepath.Join(root, "repository")
	if err := os.MkdirAll(repository, 0o755); err != nil {
		t.Fatalf("create fixture repository: %v", err)
	}

	// Dated in the past so that "git stale" has something to report: a branch
	// committed a moment ago is stale to no threshold worth testing against.
	const committed = "2024-01-02T03:04:05+00:00"

	git := func(args ...string) {
		command := exec.Command("git", args...)
		command.Dir = repository
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
			"GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
			"GIT_AUTHOR_DATE="+committed, "GIT_COMMITTER_DATE="+committed,
			"GIT_CONFIG_GLOBAL="+filepath.Join(root, "gitconfig-that-does-not-exist"),
			"GIT_CONFIG_SYSTEM="+filepath.Join(root, "gitconfig-that-does-not-exist"),
		)
		if output, err := command.CombinedOutput(); err != nil {
			t.Skipf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}

	commit := func(name, contents string) {
		if err := os.WriteFile(filepath.Join(repository, name), []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		git("add", name)
		git("commit", "-m", "add "+name)
	}

	git("init", "--initial-branch=main")
	commit("README.md", "# Fixture repository\n")
	commit("main.go", "package main\n\nfunc main() {}\n")
	git("branch", "quiet")

	return repository
}

func expandPlaceholders(argument string, replacements map[string]string) string {
	for placeholder, value := range replacements {
		argument = strings.ReplaceAll(argument, placeholder, value)
	}
	return argument
}

func readSurfaceGolden(t *testing.T) map[string]string {
	t.Helper()

	contents, err := os.ReadFile(surfaceGoldenPath(t))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}
		}
		t.Fatalf("read %s: %v", surfaceGolden, err)
	}

	sections := map[string]string{}
	name := ""
	var body []string
	flush := func() {
		if name != "" {
			sections[name] = strings.Join(body, "\n")
		}
		body = nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "## "):
			flush()
			name = strings.TrimPrefix(line, "## ")
		case strings.TrimSpace(line) == "" || strings.HasPrefix(line, "# "):
		default:
			body = append(body, line)
		}
	}
	flush()
	return sections
}

func writeSurfaceGolden(t *testing.T, recorded map[string]string) {
	t.Helper()

	names := make([]string, 0, len(recorded))
	for name := range recorded {
		names = append(names, name)
	}
	sort.Strings(names)

	var out strings.Builder
	out.WriteString("# The JSON surface of every covered command: one \"path: type\" line per field.\n")
	out.WriteString("# Generated by tests/json_surface_test.go. A diff here is a change to the\n")
	out.WriteString("# public surface described in docs/release-process.md.\n")
	for _, name := range names {
		fmt.Fprintf(&out, "\n## %s\n%s\n", name, recorded[name])
	}

	if err := os.WriteFile(surfaceGoldenPath(t), []byte(out.String()), 0o600); err != nil {
		t.Fatalf("write %s: %v", surfaceGolden, err)
	}
	t.Logf("wrote %s with %d commands", surfaceGolden, len(names))
}

func surfaceGoldenPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(moduleRoot(t), "testdata", surfaceGolden)
}
