package cli

import (
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/core/security"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

func TestPasswordTextPrintsEachPasswordOnItsOwnLine(t *testing.T) {
	result := security.PasswordResult{
		Passwords:   []string{"aaaa", "bbbb", "cccc"},
		Length:      4,
		Count:       3,
		Classes:     []string{security.ClassLowercase},
		Alphabet:    26,
		EntropyBits: 18.8,
	}

	got := render(t, passwordText(result))
	lines := strings.Split(strings.TrimSpace(got), "\n")

	for index, password := range result.Passwords {
		if lines[index] != password {
			t.Errorf("line %d = %q, want %q", index, lines[index], password)
		}
	}
	for _, want := range []string{"length", "alphabet", "entropy", "lowercase"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestStrengthTextShowsTheVerdictAndFindings(t *testing.T) {
	result := security.StrengthResult{
		Length:      8,
		Score:       15,
		Rating:      security.RatingVeryWeak,
		EntropyBits: 37.6,
		Classes:     []string{security.ClassLowercase},
		Findings: []security.Finding{
			{
				Code:       security.FindingCommon,
				Message:    "it is one of the most commonly used passwords",
				Suggestion: "choose something that is not on every guessing list",
			},
		},
	}

	got := render(t, strengthText(result))
	for _, want := range []string{"very weak", "15 / 100", "8 characters", "commonly used"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// A clean result must not read as a guarantee; the built-in list is short.
func TestStrengthTextQualifiesACleanResult(t *testing.T) {
	result := security.StrengthResult{
		Length: 24, Score: 92, Rating: security.RatingVeryStrong,
		Classes: []string{security.ClassLowercase, security.ClassDigits},
		Strong:  true,
	}

	got := render(t, strengthText(result))
	if !strings.Contains(got, "No weaknesses found") {
		t.Errorf("output = %q", got)
	}
	if !strings.Contains(got, "not a guarantee") {
		t.Errorf("output = %q, want the limitation stated", got)
	}
}

func TestSecurityHashTextPrintsASingleDigestBare(t *testing.T) {
	result := security.HashResult{
		Source:    security.SourceText,
		Bytes:     3,
		Checksums: []fs.Checksum{{Algorithm: "sha256", Value: "abc123"}},
	}

	got := strings.TrimSpace(render(t, securityHashText(result)))
	if got != "abc123" {
		t.Errorf("output = %q, want the digest alone so it can be piped", got)
	}
}

func TestSecurityHashTextTabulatesSeveralDigests(t *testing.T) {
	result := security.HashResult{
		Source: security.SourceFile,
		Path:   "/downloads/installer.exe",
		Bytes:  2048,
		Checksums: []fs.Checksum{
			{Algorithm: "sha256", Value: "aaa"},
			{Algorithm: "md5", Value: "bbb"},
		},
	}

	got := render(t, securityHashText(result))
	for _, want := range []string{"algorithm", "sha256", "md5", "installer.exe", "2.0 KB"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// Both digests are printed either way, so the user has something to check when
// they start doubting the tool.
func TestChecksumTextShowsBothDigests(t *testing.T) {
	match := security.ChecksumResult{
		Path: "/downloads/devnest.zip", Algorithm: "sha256",
		Expected: "aaa", Actual: "aaa", Match: true, Bytes: 1024,
	}

	got := render(t, checksumText(match))
	for _, want := range []string{"expected", "actual", "match"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}

	mismatch := match
	mismatch.Actual = "bbb"
	mismatch.Match = false

	got = render(t, checksumText(mismatch))
	if !strings.Contains(got, "no match") {
		t.Errorf("output = %q, want the verdict", got)
	}
	if !strings.Contains(got, "bbb") {
		t.Errorf("output = %q, want the actual digest", got)
	}
}

func TestDecodeTextPrintsPrintableValues(t *testing.T) {
	result := security.DecodeResult{
		Decoded: "hello world", Bytes: 11, Printable: true, Alphabet: "standard",
	}

	got := strings.TrimSpace(render(t, decodeText(result, false)))
	if got != "hello world" {
		t.Errorf("output = %q", got)
	}
}

// Writing arbitrary bytes to a terminal can change how it behaves, and a
// decode command is exactly where untrusted bytes arrive.
func TestDecodeTextWithholdsBinaryAndExplainsWhy(t *testing.T) {
	result := security.DecodeResult{
		Hex: "001b5b33316d", Bytes: 6, Printable: false, Alphabet: "standard",
	}

	got := render(t, decodeText(result, false))
	if !strings.Contains(got, "001b5b33316d") {
		t.Errorf("output = %q, want the hex", got)
	}
	if !strings.Contains(got, "not printable text") {
		t.Errorf("output = %q, want an explanation", got)
	}

	raw := strings.TrimSpace(render(t, decodeText(result, true)))
	if raw != "001b5b33316d" {
		t.Errorf("--raw output = %q, want just the bytes", raw)
	}
}

func TestReadSecretWarnsAboutTheCommandLine(t *testing.T) {
	env, _, _ := newTestEnv(t, "table")

	value, err := readSecret(env, []string{"hunter2"}, false, "password")
	if err != nil {
		t.Fatalf("readSecret: %v", err)
	}
	if value != "hunter2" {
		t.Errorf("value = %q", value)
	}

	if len(env.warnings) != 1 {
		t.Fatalf("warnings = %v, want one", env.warnings)
	}
	warning := env.warnings[0].Message
	if !strings.Contains(warning, "--stdin") {
		t.Errorf("warning = %q, want it to name the safer flag", warning)
	}
	// The warning itself must not repeat the secret.
	if strings.Contains(warning, "hunter2") {
		t.Errorf("the warning contains the password: %q", warning)
	}
}

func TestReadSecretFromStdin(t *testing.T) {
	env, _, _ := newTestEnv(t, "table")
	env.Stdin = strings.NewReader("from a pipe\n")

	value, err := readSecret(env, nil, true, "password")
	if err != nil {
		t.Fatalf("readSecret: %v", err)
	}
	if value != "from a pipe" {
		t.Errorf("value = %q, want the trailing newline stripped", value)
	}
	if len(env.warnings) != 0 {
		t.Errorf("warnings = %v, want none for --stdin", env.warnings)
	}
}

// A password may legitimately contain spaces, so only the trailing newline is
// removed.
func TestReadSecretKeepsInternalSpacing(t *testing.T) {
	env, _, _ := newTestEnv(t, "table")
	env.Stdin = strings.NewReader("correct horse battery staple\n")

	value, err := readSecret(env, nil, true, "password")
	if err != nil {
		t.Fatalf("readSecret: %v", err)
	}
	if value != "correct horse battery staple" {
		t.Errorf("value = %q", value)
	}
}

func TestReadSecretRejectsBothSources(t *testing.T) {
	env, _, _ := newTestEnv(t, "table")
	env.Stdin = strings.NewReader("piped\n")

	_, err := readSecret(env, []string{"argument"}, true, "password")
	if errors.CodeOf(err) != errors.CodeInvalidInput {
		t.Errorf("code = %q, want %q", errors.CodeOf(err), errors.CodeInvalidInput)
	}
}

func TestReadSecretRejectsEmptyStdin(t *testing.T) {
	env, _, _ := newTestEnv(t, "table")
	env.Stdin = strings.NewReader("\n")

	_, err := readSecret(env, nil, true, "password")
	if errors.CodeOf(err) != errors.CodeInvalidInput {
		t.Errorf("code = %q, want %q", errors.CodeOf(err), errors.CodeInvalidInput)
	}
}

func TestReadSecretRequiresSomething(t *testing.T) {
	env, _, _ := newTestEnv(t, "table")

	_, err := readSecret(env, nil, false, "password")
	if errors.CodeOf(err) != errors.CodeInvalidInput {
		t.Errorf("code = %q, want %q", errors.CodeOf(err), errors.CodeInvalidInput)
	}
}

func TestHashInputResolvesOneSource(t *testing.T) {
	env, _, _ := newTestEnv(t, "table")

	fromText, err := hashInput(env, []string{"hello"}, "", false)
	if err != nil {
		t.Fatalf("hashInput: %v", err)
	}
	if fromText.Source != security.SourceText || fromText.Text != "hello" {
		t.Errorf("request = %+v", fromText)
	}

	fromFile, err := hashInput(env, nil, "/payload.bin", false)
	if err != nil {
		t.Fatalf("hashInput: %v", err)
	}
	if fromFile.Source != security.SourceFile || fromFile.Path != "/payload.bin" {
		t.Errorf("request = %+v", fromFile)
	}

	env.Stdin = strings.NewReader("piped content\n")
	fromStdin, err := hashInput(env, nil, "", true)
	if err != nil {
		t.Fatalf("hashInput: %v", err)
	}
	if fromStdin.Source != security.SourceText || fromStdin.Text != "piped content" {
		t.Errorf("request = %+v", fromStdin)
	}
}

// Guessing which input was meant is worse than asking.
func TestHashInputRejectsAmbiguity(t *testing.T) {
	env, _, _ := newTestEnv(t, "table")

	if _, err := hashInput(env, nil, "", false); errors.CodeOf(err) != errors.CodeInvalidInput {
		t.Errorf("no source: code = %q", errors.CodeOf(err))
	}
	if _, err := hashInput(env, []string{"text"}, "/file", false); errors.CodeOf(err) != errors.CodeInvalidInput {
		t.Errorf("two sources: code = %q", errors.CodeOf(err))
	}
	if _, err := hashInput(env, []string{"text"}, "", true); errors.CodeOf(err) != errors.CodeInvalidInput {
		t.Errorf("text and stdin: code = %q", errors.CodeOf(err))
	}
}

func TestEncodingInputDoesNotWarn(t *testing.T) {
	env, _, _ := newTestEnv(t, "table")

	value, err := encodingInput(env, []string{"hello"}, false, "text")
	if err != nil {
		t.Fatalf("encodingInput: %v", err)
	}
	if value != "hello" {
		t.Errorf("value = %q", value)
	}
	// Encoded text is not a secret by virtue of being encoded; warning every
	// time would train people to stop reading warnings that matter.
	if len(env.warnings) != 0 {
		t.Errorf("warnings = %v, want none", env.warnings)
	}
}

func TestPluralWeaknesses(t *testing.T) {
	if got := pluralWeaknesses(1); got != "1 weakness" {
		t.Errorf("pluralWeaknesses(1) = %q", got)
	}
	if got := pluralWeaknesses(3); got != "3 weaknesses" {
		t.Errorf("pluralWeaknesses(3) = %q", got)
	}
}

func TestJoinClasses(t *testing.T) {
	if got := joinClasses(nil); got != "none" {
		t.Errorf("joinClasses(nil) = %q", got)
	}
	if got := joinClasses([]string{"a", "b"}); got != "a, b" {
		t.Errorf("joinClasses = %q", got)
	}
}

func TestAlgorithmNames(t *testing.T) {
	names := algorithmNames()
	if len(names) != len(fs.Algorithms()) {
		t.Errorf("names = %v, want one per supported algorithm", names)
	}
	if !strings.Contains(strings.Join(names, ","), "sha256") {
		t.Errorf("names = %v", names)
	}
}
