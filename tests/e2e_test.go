//go:build e2e

// End-to-end tests run the built binary as a real process. They cover the
// contracts that only a process can demonstrate: exit codes, and the promise
// that stdout carries results and stderr carries everything else.
//
// Run with: make test-e2e
package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func binaryPath(t *testing.T) string {
	t.Helper()

	if path := os.Getenv("DEVNEST_BINARY"); path != "" {
		return path
	}

	name := "devnest"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(moduleRoot(t), "dist", name)

	if _, err := os.Stat(path); err != nil {
		t.Skipf("binary not found at %s; run \"make build\" first", path)
	}
	return path
}

type result struct {
	stdout   string
	stderr   string
	exitCode int
}

// runBinary invokes DevNest with an empty configuration file, so the machine's
// own settings cannot change what the tests observe.
func runBinary(t *testing.T, args ...string) result {
	t.Helper()

	config := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(config, nil, 0o600); err != nil {
		t.Fatalf("write empty configuration: %v", err)
	}

	command := exec.Command(binaryPath(t), append(args, "--config", config)...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	code := 0
	if err := command.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %s: %v", command.Path, err)
		}
		code = exitErr.ExitCode()
	}

	return result{stdout: stdout.String(), stderr: stderr.String(), exitCode: code}
}

func TestVersionExitsZero(t *testing.T) {
	got := runBinary(t, "version")

	if got.exitCode != 0 {
		t.Errorf("exit code = %d, want 0\nstderr: %s", got.exitCode, got.stderr)
	}
	if !strings.Contains(got.stdout, "platform") {
		t.Errorf("stdout = %q, want the version listing", got.stdout)
	}
	if got.stderr != "" {
		t.Errorf("stderr = %q, want it empty on success", got.stderr)
	}
}

func TestExitCodeContract(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"success", []string{"version"}, 0},
		{"help", []string{"help"}, 0},
		{"unknown command", []string{"frobnicate"}, 2},
		{"unknown flag", []string{"version", "--nonsense"}, 2},
		{"unexpected argument", []string{"version", "extra"}, 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := runBinary(t, test.args...)
			if got.exitCode != test.want {
				t.Errorf("exit code = %d, want %d\nstdout: %s\nstderr: %s",
					got.exitCode, test.want, got.stdout, got.stderr)
			}
		})
	}
}

// A configuration file the user named must exist. The exit code says so
// without anyone parsing the message.
func TestMissingConfigFileExitsNotFound(t *testing.T) {
	command := exec.Command(binaryPath(t), "version", "--config", "no-such-file.toml")
	var stderr bytes.Buffer
	command.Stderr = &stderr

	err := command.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected a non-zero exit, got %v", err)
	}
	if exitErr.ExitCode() != 3 {
		t.Errorf("exit code = %d, want 3\nstderr: %s", exitErr.ExitCode(), stderr.String())
	}
}

// The split between the streams is what makes piping work. It has to hold at
// every verbosity level, not only the default one.
func TestStdoutCarriesOnlyResults(t *testing.T) {
	for _, verbosity := range []string{"", "--quiet", "--verbose"} {
		args := []string{"version", "--output", "json"}
		if verbosity != "" {
			args = append(args, verbosity)
		}

		got := runBinary(t, args...)
		if got.exitCode != 0 {
			t.Fatalf("exit code = %d at %q\nstderr: %s", got.exitCode, verbosity, got.stderr)
		}

		var envelope map[string]any
		if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
			t.Errorf("stdout at %q is not pure JSON: %v\n%s", verbosity, err, got.stdout)
		}
	}
}

func TestFailureInJSONModeStillProducesJSON(t *testing.T) {
	got := runBinary(t, "frobnicate", "--output", "json")

	if got.exitCode == 0 {
		t.Fatal("exit code = 0 for an unknown command")
	}

	var envelope struct {
		Status string `json:"status"`
		Error  struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, got.stdout)
	}
	if envelope.Status != "error" {
		t.Errorf("status = %q, want \"error\"", envelope.Status)
	}
	if envelope.Error.Code != "INVALID_INPUT" {
		t.Errorf("code = %q, want \"INVALID_INPUT\"", envelope.Error.Code)
	}
}

// The network commands are the only ones that open a socket, so the end-to-end
// suite exercises the paths that reject bad input before anything is sent.
// Nothing here touches the network.
func TestNetworkRejectsBadInputWithoutConnecting(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"unsupported scheme", []string{"network", "monitor", "file:///etc/passwd"}},
		{"no target", []string{"network", "ping"}},
		{"two targets", []string{"network", "ssl", "a.example", "b.example"}},
		{"bad method", []string{"network", "http", "example.com", "--method", "BREW"}},
		{"bad record type", []string{"network", "dns", "example.com", "--type", "SOA"}},
		{"bad header", []string{"network", "http", "example.com", "--header", "no-colon"}},
		{"empty label", []string{"network", "dns", "example..com"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := runBinary(t, test.args...)
			if got.exitCode != 2 {
				t.Errorf("exit code = %d, want 2\nstdout: %s\nstderr: %s",
					got.exitCode, got.stdout, got.stderr)
			}
			if got.stdout != "" {
				t.Errorf("stdout = %q, want it empty on a usage error", got.stdout)
			}
		})
	}
}

// The security commands are the ones handling secrets, so the end-to-end suite
// checks the properties that only a real process can demonstrate.
func TestSecurityRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no password to check", []string{"security", "password-check"}},
		{"no value to decode", []string{"security", "decode"}},
		{"invalid base64", []string{"security", "decode", "not base64!!!"}},
		{"checksum without a digest", []string{"security", "checksum", "somefile"}},
		{"malformed digest", []string{"security", "checksum", "somefile", "xyz"}},
		{"password too short", []string{"security", "password", "--length", "4"}},
		{"no character classes", []string{
			"security", "password",
			"--no-uppercase", "--no-lowercase", "--no-digits", "--no-symbols",
		}},
		{"hash with no input", []string{"security", "hash"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := runBinary(t, test.args...)
			if got.exitCode != 2 {
				t.Errorf("exit code = %d, want 2\nstdout: %s\nstderr: %s",
					got.exitCode, got.stdout, got.stderr)
			}
		})
	}
}

// Two runs of the generator must not agree. A reproducible password generator
// is not a password generator.
func TestSecurityPasswordsDifferBetweenRuns(t *testing.T) {
	first := runBinary(t, "security", "password", "--length", "32")
	second := runBinary(t, "security", "password", "--length", "32")

	if first.exitCode != 0 || second.exitCode != 0 {
		t.Fatalf("exit codes = %d and %d", first.exitCode, second.exitCode)
	}

	firstPassword := strings.SplitN(strings.TrimSpace(first.stdout), "\n", 2)[0]
	secondPassword := strings.SplitN(strings.TrimSpace(second.stdout), "\n", 2)[0]

	if firstPassword == "" || len([]rune(firstPassword)) != 32 {
		t.Fatalf("first password = %q, want 32 characters", firstPassword)
	}
	if firstPassword == secondPassword {
		t.Fatal("two runs produced the same password")
	}
}

// The strength checker's whole privacy promise is that the password does not
// come back out. Checked here against the real process, both streams.
func TestSecurityPasswordCheckDoesNotEchoTheInput(t *testing.T) {
	const password = "Zq7xKp3mWn8vTr4b"

	got := runBinary(t, "security", "password-check", password, "--output", "json")
	if got.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
	}

	if strings.Contains(got.stdout, password) {
		t.Errorf("stdout contains the password:\n%s", got.stdout)
	}
	if strings.Contains(got.stderr, password) {
		t.Errorf("stderr contains the password:\n%s", got.stderr)
	}
}

// Giving a secret on the command line is a real disclosure, and the warning
// has to name the safer flag rather than only stating the problem.
func TestSecurityPasswordCheckWarnsAboutTheCommandLine(t *testing.T) {
	got := runBinary(t, "security", "password-check", "Zq7xKp3mWn8vTr4b")

	if !strings.Contains(got.stderr, "--stdin") {
		t.Errorf("stderr = %q, want a warning naming --stdin", got.stderr)
	}
	// The warning belongs on stderr; stdout carries the result.
	if strings.Contains(got.stdout, "--stdin") {
		t.Errorf("the warning reached stdout:\n%s", got.stdout)
	}
}

func TestSecurityChecksumExitCodes(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "payload.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	const correct = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	const wrong = "0000000000000000000000000000000000000000000000000000000000000000"

	if got := runBinary(t, "security", "checksum", path, correct); got.exitCode != 0 {
		t.Errorf("a matching digest exited %d\nstderr: %s", got.exitCode, got.stderr)
	}
	if got := runBinary(t, "security", "checksum", path, wrong); got.exitCode != 1 {
		t.Errorf("a mismatched digest exited %d, want 1", got.exitCode)
	}
}

// Hashing text and hashing a file of the same content must agree, or the two
// paths are answering different questions under one name.
func TestSecurityHashTextAndFileAgree(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "payload.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	fromText := runBinary(t, "security", "hash", "abc")
	fromFile := runBinary(t, "security", "hash", "--file", path)

	if fromText.exitCode != 0 || fromFile.exitCode != 0 {
		t.Fatalf("exit codes = %d and %d", fromText.exitCode, fromFile.exitCode)
	}
	if strings.TrimSpace(fromText.stdout) != strings.TrimSpace(fromFile.stdout) {
		t.Errorf("text gave %q, file gave %q",
			strings.TrimSpace(fromText.stdout), strings.TrimSpace(fromFile.stdout))
	}
}

func TestSecurityEncodeDecodeRoundTrip(t *testing.T) {
	const plain = "selamat pagi, dunia"

	encoded := runBinary(t, "security", "encode", plain)
	if encoded.exitCode != 0 {
		t.Fatalf("encode exited %d\nstderr: %s", encoded.exitCode, encoded.stderr)
	}

	decoded := runBinary(t, "security", "decode", strings.TrimSpace(encoded.stdout))
	if decoded.exitCode != 0 {
		t.Fatalf("decode exited %d\nstderr: %s", decoded.exitCode, decoded.stderr)
	}
	if strings.TrimSpace(decoded.stdout) != plain {
		t.Errorf("round trip gave %q, want %q", strings.TrimSpace(decoded.stdout), plain)
	}
}

func TestSecurityGroupHelp(t *testing.T) {
	got := runBinary(t, "security", "help")

	if got.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
	}
	for _, name := range []string{
		"password", "password-check", "hash", "checksum", "encode", "decode",
	} {
		if !strings.Contains(got.stdout, name) {
			t.Errorf("group help does not list %q:\n%s", name, got.stdout)
		}
	}
}

func TestNetworkGroupHelp(t *testing.T) {
	got := runBinary(t, "network", "help")

	if got.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
	}
	for _, name := range []string{"monitor", "http", "latency", "ping", "dns", "ssl"} {
		if !strings.Contains(got.stdout, name) {
			t.Errorf("group help does not list %q:\n%s", name, got.stdout)
		}
	}
}

func TestHelpIsAvailableForEveryCommand(t *testing.T) {
	for _, name := range []string{"help", "version"} {
		got := runBinary(t, name, "--help")

		if got.exitCode != 0 {
			t.Errorf("%s --help exited %d", name, got.exitCode)
		}
		if !strings.Contains(got.stdout, "Usage:") {
			t.Errorf("%s --help produced no usage line", name)
		}
		if !strings.Contains(got.stdout, "Examples:") {
			t.Errorf("%s --help produced no examples", name)
		}
	}
}

// The log commands are the ones that read a whole file, so the end-to-end
// suite covers what only a real process shows: a real file on disk, the exit
// code a script branches on, and csv arriving on stdout as csv.
func writeAccessLog(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "access.log")
	content := `10.0.0.1 - - [24/Jul/2026:09:15:01 +0000] "GET /a HTTP/1.1" 200 1024
10.0.0.2 - - [24/Jul/2026:09:15:02 +0000] "GET /a?page=2 HTTP/1.1" 200 2048
10.0.0.1 - - [24/Jul/2026:09:15:03 +0000] "POST /b HTTP/1.1" 500 128
this line is not an access log entry
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestLogSummarisesAnAccessLog(t *testing.T) {
	path := writeAccessLog(t)

	got := runBinary(t, "log", "http", path)
	if got.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
	}
	for _, want := range []string{"requests", "GET", "/a", "500"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("stdout does not mention %q:\n%s", want, got.stdout)
		}
	}
}

// csv output has to be csv and nothing else: no envelope, no preamble, header
// first. Anything else breaks the tool it is being piped into.
func TestLogCSVOutputIsRows(t *testing.T) {
	path := writeAccessLog(t)

	got := runBinary(t, "log", "top", path, "--output", "csv")
	if got.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
	}

	lines := strings.Split(strings.TrimSpace(got.stdout), "\n")
	if len(lines) < 2 {
		t.Fatalf("stdout = %q, want a header and at least one row", got.stdout)
	}
	if strings.TrimSpace(lines[0]) != "endpoint,count,percent" {
		t.Errorf("header = %q, want the column names", lines[0])
	}
	if strings.Contains(got.stdout, "devnest") {
		t.Errorf("stdout carries envelope metadata:\n%s", got.stdout)
	}
}

// Finding nothing is an answer a script branches on, so it has its own exit
// code rather than a zero that hides it.
func TestLogSearchExitCodes(t *testing.T) {
	path := writeAccessLog(t)

	if got := runBinary(t, "log", "search", path, "POST"); got.exitCode != 0 {
		t.Errorf("a match exited %d, want 0\nstderr: %s", got.exitCode, got.stderr)
	}
	if got := runBinary(t, "log", "search", path, "no-such-text"); got.exitCode != 3 {
		t.Errorf("no match exited %d, want 3", got.exitCode)
	}
}

// A binary file has no lines to summarise, and saying so beats a page of
// nonsense.
func TestLogRefusesABinaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binary.log")
	if err := os.WriteFile(path, []byte{0x00, 0x01, 0x02, 0x03}, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got := runBinary(t, "log", "analyze", path)
	if got.exitCode != 2 {
		t.Errorf("exit code = %d, want 2\nstderr: %s", got.exitCode, got.stderr)
	}
	if !strings.Contains(got.stderr, "not a text file") {
		t.Errorf("stderr = %q, want it to say why", got.stderr)
	}
}

func TestLogGroupHelp(t *testing.T) {
	got := runBinary(t, "log", "help")

	if got.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
	}
	for _, name := range []string{"analyze", "http", "errors", "status", "top", "search", "stats"} {
		if !strings.Contains(got.stdout, name) {
			t.Errorf("group help does not list %q:\n%s", name, got.stdout)
		}
	}
}

// The env commands are the only ones that start other programs, so the
// end-to-end suite covers what only a real process shows: that the summary
// runs at all on this machine, and that a lookup for something absent exits
// the way a script expects.
func TestEnvSummaryRuns(t *testing.T) {
	got := runBinary(t, "env")

	if got.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
	}
	for _, want := range []string{"os", "path entries", "tools found"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("stdout does not mention %q:\n%s", want, got.stdout)
		}
	}
}

// Go is running this test, so it is installed by definition. That makes it the
// one toolchain an end-to-end test can assert on.
func TestEnvWhichFindsGoAndExitsOnAMiss(t *testing.T) {
	found := runBinary(t, "env", "which", "go")
	if found.exitCode != 0 {
		t.Errorf("looking up go exited %d\nstderr: %s", found.exitCode, found.stderr)
	}
	if !strings.Contains(found.stdout, "runs from") {
		t.Errorf("stdout = %q, want the winning path", found.stdout)
	}

	missing := runBinary(t, "env", "which", "devnest-no-such-tool")
	if missing.exitCode != 3 {
		t.Errorf("looking up an absent tool exited %d, want 3", missing.exitCode)
	}
}

// Credential-shaped values are hidden in every output format, because a
// listing gets redirected to a file and attached to a ticket.
func TestEnvVarsHidesCredentials(t *testing.T) {
	const secret = "devnest-e2e-secret-value"

	config := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(config, nil, 0o600); err != nil {
		t.Fatalf("write empty configuration: %v", err)
	}

	command := exec.Command(binaryPath(t), "env", "vars", "DEVNEST_E2E",
		"--output", "json", "--config", config)
	command.Env = append(os.Environ(), "DEVNEST_E2E_TOKEN="+secret)

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}

	if !strings.Contains(stdout.String(), "DEVNEST_E2E_TOKEN") {
		t.Fatalf("stdout does not list the variable:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), secret) {
		t.Errorf("the json output contains the secret:\n%s", stdout.String())
	}
}

// scan runs against the repository it is built from, which is the most
// realistic tree available and the one whose shape a maintainer can check.
func TestScanReportsThisRepository(t *testing.T) {
	root := moduleRoot(t)

	got := runBinary(t, "scan", root)
	if got.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
	}
	for _, want := range []string{"files", "By category", "source", "Go"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("stdout does not mention %q:\n%s", want, got.stdout)
		}
	}
	// The ignore rules keep the built binary and the coverage output out of
	// the count, so a scan of a repository somebody has built is stable.
	if strings.Contains(got.stdout, "dist") {
		t.Errorf("stdout mentions build output:\n%s", got.stdout)
	}
}

func TestScanSubcommandsRun(t *testing.T) {
	root := moduleRoot(t)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"types", []string{"scan", "types", root, "--limit", "5"}, ".go"},
		{"lines", []string{"scan", "lines", root, "--limit", "3"}, "By language"},
		{"tree", []string{"scan", "tree", root, "--depth", "1"}, "internal/"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := runBinary(t, test.args...)
			if got.exitCode != 0 {
				t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
			}
			if !strings.Contains(got.stdout, test.want) {
				t.Errorf("stdout does not mention %q:\n%s", test.want, got.stdout)
			}
		})
	}
}

func TestScanRefusesAFile(t *testing.T) {
	got := runBinary(t, "scan", filepath.Join(moduleRoot(t), "go.mod"))

	if got.exitCode != 2 {
		t.Errorf("exit code = %d, want 2\nstderr: %s", got.exitCode, got.stderr)
	}
	if !strings.Contains(got.stderr, "not a directory") {
		t.Errorf("stderr = %q, want it to say why", got.stderr)
	}
}

func TestEnvAndScanGroupHelp(t *testing.T) {
	tests := map[string][]string{
		"env":  {"list", "path", "which", "vars"},
		"scan": {"types", "lines", "tree"},
	}

	for group, commands := range tests {
		got := runBinary(t, group, "help")
		if got.exitCode != 0 {
			t.Fatalf("%s help exited %d\nstderr: %s", group, got.exitCode, got.stderr)
		}
		for _, name := range commands {
			if !strings.Contains(got.stdout, name) {
				t.Errorf("%s help does not list %q:\n%s", group, name, got.stdout)
			}
		}
	}
}

// The encoding and data commands are the ones a script pipes into something
// else, so what the end-to-end suite covers here is the shape of stdout and
// the exit codes, which only a real process demonstrates.
func TestEncodeAndDecodeRoundTripOnTheCommandLine(t *testing.T) {
	encoded := runBinary(t, "encode", "hex", "hello")
	if encoded.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", encoded.exitCode, encoded.stderr)
	}
	if strings.TrimSpace(encoded.stdout) != "68656c6c6f" {
		t.Fatalf("stdout = %q, want the hex alone", encoded.stdout)
	}

	decoded := runBinary(t, "decode", "hex", strings.TrimSpace(encoded.stdout))
	if strings.TrimSpace(decoded.stdout) != "hello" {
		t.Errorf("stdout = %q, want the original text", decoded.stdout)
	}

	escaped := runBinary(t, "encode", "url", "a b&c=d")
	if strings.TrimSpace(escaped.stdout) != "a+b%26c%3Dd" {
		t.Errorf("stdout = %q, want the percent-encoded value", escaped.stdout)
	}
	if back := runBinary(t, "decode", "url", "a+b%26c%3Dd"); strings.TrimSpace(back.stdout) != "a b&c=d" {
		t.Errorf("stdout = %q, want the original value", back.stdout)
	}
}

// A token is decoded, never verified, and an expired one is still a successful
// run: the expiry is a fact about the input, not a failure of the command.
func TestDecodeJWTReportsExpiryWithoutVerifying(t *testing.T) {
	// {"alg":"HS256","typ":"JWT"} and {"sub":"ana","exp":1750000000}
	const token = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
		"eyJzdWIiOiJhbmEiLCJleHAiOjE3NTAwMDAwMDB9.c2ln"

	got := runBinary(t, "decode", "jwt", token)
	if got.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
	}
	if !strings.Contains(got.stdout, "NOT verified") {
		t.Errorf("stdout does not mark the signature unverified:\n%s", got.stdout)
	}
	if !strings.Contains(got.stdout, "EXPIRED") {
		t.Errorf("stdout does not report the expiry:\n%s", got.stdout)
	}
	if !strings.Contains(got.stderr, "expired") {
		t.Errorf("stderr carries no warning:\n%s", got.stderr)
	}
}

func writeDocuments(t *testing.T) (jsonPath, yamlPath string) {
	t.Helper()

	directory := t.TempDir()
	jsonPath = filepath.Join(directory, "users.json")
	yamlPath = filepath.Join(directory, "manifest.yaml")

	documents := map[string]string{
		jsonPath: `[{"name":"ana","age":31},{"name":"budi","age":24}]`,
		yamlPath: "kind: Service\nname: api\n---\nkind: Deployment\nname: api\n",
	}
	for path, content := range documents {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	return jsonPath, yamlPath
}

func TestDataCommandsRunAgainstRealFiles(t *testing.T) {
	jsonPath, yamlPath := writeDocuments(t)

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"validate json", []string{"json", jsonPath}, "array (2 entries)"},
		{"validate yaml", []string{"yaml", yamlPath}, "documents"},
		{"format", []string{"json", "format", jsonPath, "--indent", "4"}, "    \"name\": \"ana\""},
		{"minify", []string{"json", "minify", jsonPath}, `[{"name":"ana","age":31}`},
		{"query", []string{"json", "query", jsonPath, "[1].name", "--raw"}, "budi"},
		{"to-yaml", []string{"json", "to-yaml", jsonPath}, "- name: ana"},
		{"to-csv", []string{"json", "to-csv", jsonPath}, "age,name"},
		{"to-json", []string{"yaml", "to-json", yamlPath}, `"kind": "Service"`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := runBinary(t, testCase.args...)
			if got.exitCode != 0 {
				t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
			}
			if !strings.Contains(got.stdout, testCase.want) {
				t.Errorf("stdout does not contain %q:\n%s", testCase.want, got.stdout)
			}
		})
	}
}

// Minified output is exactly the document: a note about what was saved would
// end up in whatever the command was redirected into.
func TestJSONMinifyWritesOnlyTheDocument(t *testing.T) {
	jsonPath, _ := writeDocuments(t)

	got := runBinary(t, "json", "minify", jsonPath)
	if strings.Contains(got.stdout, "saved") || strings.Contains(got.stdout, "bytes") {
		t.Errorf("stdout carries commentary:\n%s", got.stdout)
	}
	if strings.Count(strings.TrimSpace(got.stdout), "\n") != 0 {
		t.Errorf("stdout = %q, want one line", got.stdout)
	}
}

// The exit codes are what a pre-commit hook and a script branch on: a document
// that does not parse fails, and an expression that selects nothing is the
// same "not found" every other command reports.
func TestDataExitCodes(t *testing.T) {
	jsonPath, _ := writeDocuments(t)

	broken := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(broken, []byte("{\n  \"a\": 1,\n  \"b\": ,\n}\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	invalid := runBinary(t, "json", broken)
	if invalid.exitCode != 1 {
		t.Errorf("a broken document exited %d, want 1", invalid.exitCode)
	}
	if !strings.Contains(invalid.stderr, "line 3") {
		t.Errorf("stderr does not name the line:\n%s", invalid.stderr)
	}

	missing := runBinary(t, "json", "query", jsonPath, "[0].nickname")
	if missing.exitCode != 3 {
		t.Errorf("a missing key exited %d, want 3\nstderr: %s", missing.exitCode, missing.stderr)
	}

	nested := filepath.Join(t.TempDir(), "nested.json")
	if err := os.WriteFile(nested, []byte(`[{"a":{"b":1}}]`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if got := runBinary(t, "json", "to-csv", nested); got.exitCode != 2 {
		t.Errorf("a nested value exited %d, want 2\nstderr: %s", got.exitCode, got.stderr)
	}
}

// to-csv produces csv whichever renderer is selected, because a command asked
// for csv should not print an aligned table just because that is the default.
func TestJSONToCSVIsCSVInEveryTextMode(t *testing.T) {
	jsonPath, _ := writeDocuments(t)

	for _, args := range [][]string{
		{"json", "to-csv", jsonPath},
		{"json", "to-csv", jsonPath, "--output", "csv"},
	} {
		got := runBinary(t, args...)
		if got.exitCode != 0 {
			t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
		}

		lines := strings.Split(strings.TrimSpace(got.stdout), "\n")
		if strings.TrimSpace(lines[0]) != "age,name" {
			t.Errorf("header = %q, want the column names", lines[0])
		}
		if strings.Contains(got.stdout, "devnest") {
			t.Errorf("stdout carries envelope metadata:\n%s", got.stdout)
		}
	}
}

func TestEncodingAndDataGroupHelp(t *testing.T) {
	tests := map[string][]string{
		"encode": {"hex", "url"},
		"decode": {"hex", "url", "jwt"},
		"json":   {"format", "minify", "query", "to-yaml", "to-csv"},
		"yaml":   {"to-json"},
	}

	for group, commands := range tests {
		got := runBinary(t, group, "help")
		if got.exitCode != 0 {
			t.Fatalf("%s help exited %d\nstderr: %s", group, got.exitCode, got.stderr)
		}
		for _, name := range commands {
			if !strings.Contains(got.stdout, name) {
				t.Errorf("%s help does not list %q:\n%s", group, name, got.stdout)
			}
		}
	}
}

// The port and clean groups are where a mistake costs a process or a
// directory, so the end-to-end suite covers the exit codes a script branches
// on and, for clean, the promise that a run without --apply removes nothing.
func TestPortListRunsAndReportsRows(t *testing.T) {
	got := runBinary(t, "port", "list", "--output", "csv")
	if got.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
	}

	lines := strings.Split(strings.TrimSpace(got.stdout), "\n")
	if strings.TrimSpace(lines[0]) != "proto,port,address,pid,process" {
		t.Errorf("header = %q, want the column names", lines[0])
	}
	if strings.Contains(got.stdout, "devnest") {
		t.Errorf("stdout carries envelope metadata:\n%s", got.stdout)
	}
}

// A port this test is holding must be reported as in use, and a port nothing
// is on must not be. The exit code carries the same answer as the output.
func TestPortCheckExitCodes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("open a listener: %v", err)
	}
	defer func() { _ = listener.Close() }()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("read the listening address: %v", err)
	}

	inUse := runBinary(t, "port", "check", port)
	if inUse.exitCode != 3 {
		t.Errorf("a held port exited %d, want 3\nstdout: %s", inUse.exitCode, inUse.stdout)
	}
	if !strings.Contains(inUse.stdout, "in use") {
		t.Errorf("stdout = %q, want it to say the port is in use", inUse.stdout)
	}

	// Port 1 is not something a test can guarantee is free, so the free case
	// uses the port this test just released.
	_ = listener.Close()
	free := runBinary(t, "port", "check", port)
	if free.exitCode != 0 {
		t.Errorf("a free port exited %d, want 0\nstderr: %s", free.exitCode, free.stderr)
	}
}

func TestPortRejectsBadInput(t *testing.T) {
	for _, args := range [][]string{
		{"port", "check"},
		{"port", "check", "0"},
		{"port", "check", "70000"},
		{"port", "free", "http"},
	} {
		got := runBinary(t, args...)
		if got.exitCode != 2 {
			t.Errorf("%v exited %d, want 2\nstderr: %s", args, got.exitCode, got.stderr)
		}
	}
}

// port free asks before it acts, and a non-interactive run without --yes must
// refuse rather than hang or proceed.
func TestPortFreeRefusesToActWithoutAnAnswer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("open a listener: %v", err)
	}
	defer func() { _ = listener.Close() }()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("read the listening address: %v", err)
	}

	got := runBinary(t, "port", "free", port)
	if got.exitCode == 0 {
		t.Fatalf("a termination went ahead without confirmation\nstdout: %s", got.stdout)
	}

	// The test process is still the one holding the port, and it is still here.
	if _, err := net.DialTimeout("tcp", listener.Addr().String(), 2*time.Second); err != nil {
		t.Errorf("the listener was terminated after all: %v", err)
	}
}

// A project tree with artifacts in it, for the clean tests.
func writeProject(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	files := map[string]string{
		"package.json":                 `{"name":"example"}`,
		"src/index.js":                 "console.log(1)\n",
		"node_modules/left-pad/pad.js": "module.exports = 1\n",
		"dist/bundle.js":               "!function(){}()\n",
	}

	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create a directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write a file: %v", err)
		}
	}
	return root
}

// The default is a dry run, and the tree has to be untouched afterwards. This
// is the single most important assertion in the suite.
func TestCleanWithoutApplyDeletesNothing(t *testing.T) {
	root := writeProject(t)

	got := runBinary(t, "clean", root)
	if got.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
	}
	if !strings.Contains(got.stdout, "Nothing has been deleted") {
		t.Errorf("stdout = %q, want it to say the run changed nothing", got.stdout)
	}

	for _, name := range []string{"node_modules", "dist"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("%s was removed by a run without --apply: %v", name, err)
		}
	}
}

func TestCleanApplyRemovesTheArtifactsAndKeepsTheSource(t *testing.T) {
	root := writeProject(t)

	got := runBinary(t, "clean", root, "--apply", "--yes")
	if got.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
	}

	for _, name := range []string{"node_modules", "dist"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("%s survived --apply: %v", name, err)
		}
	}
	for _, name := range []string{"package.json", filepath.Join("src", "index.js")} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("%s was removed, and it is not build output: %v", name, err)
		}
	}
}

// Removal needs an answer. Without a terminal and without --yes the command
// fails, and nothing is deleted.
func TestCleanApplyRefusesWithoutConfirmation(t *testing.T) {
	root := writeProject(t)

	got := runBinary(t, "clean", "apply", root)
	if got.exitCode == 0 {
		t.Fatalf("a removal went ahead without confirmation\nstdout: %s", got.stdout)
	}
	if _, err := os.Stat(filepath.Join(root, "node_modules")); err != nil {
		t.Errorf("node_modules was removed anyway: %v", err)
	}
}

func TestCleanRefusesAPatternItDoesNotKnow(t *testing.T) {
	root := writeProject(t)

	got := runBinary(t, "clean", root, "--pattern", "node_modlues")
	if got.exitCode != 2 {
		t.Errorf("exit code = %d, want 2\nstderr: %s", got.exitCode, got.stderr)
	}
	if !strings.Contains(got.stderr, "clean rules") {
		t.Errorf("stderr = %q, want it to name the command that lists the rules", got.stderr)
	}
}

func TestCleanRulesListsTheWholeSurface(t *testing.T) {
	got := runBinary(t, "clean", "rules")
	if got.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", got.exitCode, got.stderr)
	}
	for _, want := range []string{"node_modules", "__pycache__", "target"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("stdout does not list %q:\n%s", want, got.stdout)
		}
	}
}

func TestPortAndCleanGroupHelp(t *testing.T) {
	tests := map[string][]string{
		"port":  {"list", "check", "free"},
		"clean": {"apply", "rules"},
	}

	for group, commands := range tests {
		got := runBinary(t, group, "help")
		if got.exitCode != 0 {
			t.Fatalf("%s help exited %d\nstderr: %s", group, got.exitCode, got.stderr)
		}
		for _, name := range commands {
			if !strings.Contains(got.stdout, name) {
				t.Errorf("%s help does not list %q:\n%s", group, name, got.stdout)
			}
		}
	}
}
