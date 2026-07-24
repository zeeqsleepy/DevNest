package secret

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// Test credentials. Every one is syntactically a real key and cryptographically
// nothing: the random-looking parts were typed by hand for this file and were
// never issued by anybody.
//
// Each is assembled from its prefix and its body rather than written out, and
// that is not decoration. A file containing a literal token is a file that
// every credential scanner in the world flags, including this one, including
// the push protection on the repository this lives in. Splitting the constant
// keeps the fixture readable to a person and invisible to a pattern, which is
// the only way a scanner's own test suite can live in its own repository.
const (
	awsKeyID     = "AKIA" + "IOSFODNN7EXAMPLE"
	githubToken  = "ghp_" + "16C7e42F292c6912E7710c838347Ae178B4a"
	stripeKey    = "sk_" + "live_" + "4eC39HqLyjWDarjtT1zdp7dc"
	googleKey    = "AIza" + "SyD-9tSrke72PouQMnMX-a7eZSW0jkFMBWY"
	slackToken   = "xoxb" + "-123456789012-1234567890123-AbCdEfGhIjKlMnOpQrStUvWx"
	placeholder  = "sk_" + "live_" + "XXXXXXXXXXXXXXXXXXXXXXXX"
	realPassword = "hunter2Zx91Qw83Lm44PbTr"
)

// fakeFS is an in-memory tree implementing Reader.
type fakeFS struct {
	files map[string]string
	dirs  map[string]bool
}

func newFakeFS() *fakeFS {
	return &fakeFS{files: map[string]string{}, dirs: map[string]bool{root(): true}}
}

func root(parts ...string) string {
	return filepath.Join(append([]string{filepath.FromSlash("/project")}, parts...)...)
}

func (f *fakeFS) with(path, content string) *fakeFS {
	full := root(strings.Split(path, "/")...)
	f.files[full] = content

	directory := filepath.Dir(full)
	for directory != "" && directory != filepath.Dir(directory) {
		f.dirs[directory] = true
		directory = filepath.Dir(directory)
	}
	return f
}

func (f *fakeFS) Resolve(path string) (string, error) {
	if strings.TrimSpace(path) == "" || path == "." {
		return root(), nil
	}
	return filepath.Clean(path), nil
}

func (f *fakeFS) Stat(path string) (fs.Entry, error) {
	clean := filepath.Clean(path)
	if f.dirs[clean] {
		return fs.Entry{Path: clean, Name: filepath.Base(clean), IsDir: true}, nil
	}
	content, found := f.files[clean]
	if !found {
		return fs.Entry{}, errors.New(errors.CodeNotFound, "cannot read %s", clean)
	}
	return fs.Entry{Path: clean, Name: filepath.Base(clean), Bytes: int64(len(content))}, nil
}

func (f *fakeFS) Open(path string) (io.ReadCloser, error) {
	content, found := f.files[filepath.Clean(path)]
	if !found {
		return nil, errors.New(errors.CodeNotFound, "cannot read %s", path)
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func (f *fakeFS) Walk(ctx context.Context, options fs.WalkOptions, visit func(fs.Entry) error) error {
	base := filepath.Clean(options.Root)

	paths := make([]string, 0, len(f.files))
	for path := range f.files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}

		relative, err := filepath.Rel(base, path)
		if err != nil || strings.HasPrefix(relative, "..") {
			continue
		}
		if excludedPath(relative, options.Exclude) {
			continue
		}

		entry, err := f.Stat(path)
		if err != nil {
			return err
		}
		if err := visit(entry); err != nil {
			return err
		}
	}
	return nil
}

// excludedPath applies the walker's exclusion rule: a glob matched against each
// path segment, so an excluded directory takes its contents with it.
func excludedPath(relative string, patterns []string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(relative), "/") {
		for _, pattern := range patterns {
			if matched, err := filepath.Match(pattern, segment); err == nil && matched {
				return true
			}
		}
	}
	return false
}

func assertCode(t *testing.T, err error, want errors.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with code %q, got nil", want)
	}
	if got := errors.CodeOf(err); got != want {
		t.Fatalf("code = %q, want %q (%v)", got, want, err)
	}
}

func scan(t *testing.T, system *fakeFS, request ScanRequest) ScanResult {
	t.Helper()
	if request.Root == "" {
		request.Root = root()
	}

	result, err := Scan(context.Background(), system, request)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return result
}

func rulesFound(findings []Finding) []string {
	names := make([]string, 0, len(findings))
	for _, finding := range findings {
		names = append(names, finding.Rule)
	}
	sort.Strings(names)
	return names
}

func TestScanFindsCredentialsByTheirShape(t *testing.T) {
	system := newFakeFS().
		with("config/aws.yml", "access_key: "+awsKeyID+"\n").
		with("src/deploy.sh", "export GITHUB_TOKEN="+githubToken+"\n").
		with("src/pay.go", `const key = "`+stripeKey+`"`+"\n").
		with("web/app.js", "const maps = '"+googleKey+"';\n").
		with("README.md", "This project does things.\n")

	result := scan(t, system, ScanRequest{})

	want := []string{"aws-access-key-id", "github-token", "google-api-key", "stripe-secret-key"}
	got := rulesFound(result.Findings)
	for _, rule := range want {
		if !contains(got, rule) {
			t.Errorf("rules found = %v, want %q among them", got, rule)
		}
	}
	if result.FilesScanned != 5 {
		t.Errorf("filesScanned = %d, want every file looked at", result.FilesScanned)
	}
}

// The one property that matters more than any other: a credential never
// appears in a result, in any field, in any output format.
func TestFindingsNeverCarryTheCredential(t *testing.T) {
	system := newFakeFS().
		with("config/aws.yml", "access_key: "+awsKeyID+"\n").
		with("src/deploy.sh", "GITHUB_TOKEN="+githubToken+"\n").
		with("src/pay.go", stripeKey+"\n")

	result := scan(t, system, ScanRequest{})
	if result.Count == 0 {
		t.Fatal("nothing was found, so this test proved nothing")
	}

	// The JSON form is the one that gets attached to a ticket, so it is the
	// one checked: every field, serialised, compared against the secrets.
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal the result: %v", err)
	}

	for _, secret := range []string{awsKeyID, githubToken, stripeKey} {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("the result carries a credential in full: %s", redact(secret))
		}
	}
}

func TestRedactionShowsEnoughToRecogniseAndNoMore(t *testing.T) {
	redacted := redact(awsKeyID)

	if !strings.HasPrefix(redacted, "AKIA") {
		t.Errorf("redacted = %q, want the recognisable prefix", redacted)
	}
	if strings.Contains(redacted, awsKeyID[4:]) {
		t.Errorf("redacted = %q, want the rest of the value gone", redacted)
	}
	if !strings.Contains(redacted, "20 chars") {
		t.Errorf("redacted = %q, want the length reported", redacted)
	}

	// A short value gets no prefix at all: four characters of eight is half
	// the secret.
	if short := redact("abc123def"); strings.Contains(short, "abc") {
		t.Errorf("redacted = %q, want nothing of a short value shown", short)
	}
}

// A placeholder has the right shape and no information. The entropy floor is
// what separates it from a key, and it is the single most important defence
// against a scanner people switch off.
func TestPlaceholdersDoNotFire(t *testing.T) {
	system := newFakeFS().
		with(".env.example", "STRIPE_KEY="+placeholder+"\n").
		with("docs/setup.md", "Set api_key to YOUR_API_KEY_HERE_XXXXXXXX\n")

	result := scan(t, system, ScanRequest{})

	for _, finding := range result.Findings {
		if finding.Rule == "generic-assignment" || finding.Rule == "stripe-secret-key" {
			t.Errorf("a placeholder was reported: %+v", finding)
		}
	}
}

func TestEntropyScoresRealValuesAboveFakeOnes(t *testing.T) {
	real := entropy(realPassword)
	fake := entropy("XXXXXXXXXXXXXXXXXXXXXXXX")

	if real <= fake {
		t.Errorf("entropy of a real value (%v) is not above a placeholder (%v)", real, fake)
	}
	if entropy("") != 0 {
		t.Error("the empty string has entropy")
	}
}

// A line the user marked is not reported, and the marker is honoured above the
// line as well as beside it.
func TestInlineSuppressionSilencesALine(t *testing.T) {
	system := newFakeFS().
		with("src/beside.go", `key := "`+awsKeyID+`" // devnest:allow-secret`+"\n").
		with("src/above.go", "// devnest:allow-secret\nkey := \""+awsKeyID+"\"\n")

	result := scan(t, system, ScanRequest{})

	if result.Count != 0 {
		t.Errorf("findings = %+v, want the marked lines left alone", result.Findings)
	}
	if result.Suppressed != 2 {
		t.Errorf("suppressed = %d, want both counted", result.Suppressed)
	}
}

// Test fixtures are full of fake credentials by design. Scanning them by
// default is how this kind of tool earns its reputation for noise.
func TestFixtureDirectoriesAreSkippedByDefault(t *testing.T) {
	system := newFakeFS().
		with("testdata/keys.txt", awsKeyID+"\n").
		with("fixtures/sample.env", "GITHUB_TOKEN="+githubToken+"\n").
		with("src/real.go", "// nothing here\n")

	quiet := scan(t, system, ScanRequest{})
	if quiet.Count != 0 {
		t.Errorf("findings = %+v, want fixtures skipped", quiet.Findings)
	}

	loud := scan(t, system, ScanRequest{IncludeTests: true})
	if loud.Count != 2 {
		t.Errorf("count = %d, want both fixtures scanned when asked", loud.Count)
	}
}

func TestLockFilesAndDependencyDirectoriesAreSkipped(t *testing.T) {
	system := newFakeFS().
		with("package-lock.json", `{"integrity":"`+googleKey+`"}`+"\n").
		with("node_modules/pkg/index.js", "const k = '"+githubToken+"';\n").
		with("src/app.js", "// clean\n")

	result := scan(t, system, ScanRequest{})
	if result.Count != 0 {
		t.Errorf("findings = %+v, want lock files and dependencies skipped", result.Findings)
	}
}

func TestExcludeAddsToTheBuiltInList(t *testing.T) {
	system := newFakeFS().
		with("secrets/keys.yml", "aws_key: "+awsKeyID+"\n").
		with("src/app.go", "// clean\n")

	if got := scan(t, system, ScanRequest{}); got.Count == 0 {
		t.Fatal("the fixture produced no finding, so the exclusion test proves nothing")
	}

	excluded := scan(t, system, ScanRequest{Exclude: []string{"secrets"}})
	if excluded.Count != 0 {
		t.Errorf("findings = %+v, want the excluded directory skipped", excluded.Findings)
	}
}

func TestBinaryFilesAreSkippedRatherThanScanned(t *testing.T) {
	system := newFakeFS().
		with("assets/logo.png", "\x89PNG\x00\x1a\n"+awsKeyID).
		with("src/app.go", "// clean\n")

	result := scan(t, system, ScanRequest{})

	if result.Count != 0 {
		t.Errorf("findings = %+v, want binary content skipped", result.Findings)
	}
	if result.FilesSkipped == 0 {
		t.Error("a skipped file was not counted as skipped, so a clean result overstates itself")
	}
}

func TestScanNarrowsToSelectedRules(t *testing.T) {
	system := newFakeFS().
		with("a.txt", awsKeyID+"\n").
		with("b.txt", githubToken+"\n")

	result := scan(t, system, ScanRequest{Rules: []string{"github-token"}})

	if result.RulesUsed != 1 {
		t.Errorf("rulesUsed = %d, want one", result.RulesUsed)
	}
	for _, finding := range result.Findings {
		if finding.Rule != "github-token" {
			t.Errorf("finding = %+v, want only the selected rule", finding)
		}
	}
}

func TestScanRejectsARuleThatDoesNotExist(t *testing.T) {
	_, err := Scan(context.Background(), newFakeFS(), ScanRequest{
		Root:  root(),
		Rules: []string{"githbu-token"},
	})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestScanCountsBySeverity(t *testing.T) {
	system := newFakeFS().
		with("a.txt", awsKeyID+"\n").
		with("b.txt", stripeKey+"\n")

	result := scan(t, system, ScanRequest{})

	if result.BySeverity[SeverityCritical] == 0 {
		t.Errorf("bySeverity = %v, want the stripe key counted as critical", result.BySeverity)
	}
	if result.BySeverity[SeverityHigh] == 0 {
		t.Errorf("bySeverity = %v, want the aws key counted as high", result.BySeverity)
	}
}

func TestScanRefusesAFile(t *testing.T) {
	system := newFakeFS().with("a.txt", "text\n")

	_, err := Scan(context.Background(), system, ScanRequest{Root: root("a.txt")})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestInspectAnswersWithoutEchoingTheValue(t *testing.T) {
	result, err := Inspect(InspectRequest{Value: githubToken})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}

	if !result.Matched || len(result.Findings) == 0 {
		t.Fatalf("result = %+v, want a match", result)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), githubToken) {
		t.Error("the tuning command echoed the value it was given")
	}
	if result.Length != len(githubToken) {
		t.Errorf("length = %d, want the real length reported", result.Length)
	}
}

func TestInspectReportsAValueThatMatchesNothing(t *testing.T) {
	result, err := Inspect(InspectRequest{Value: "just some ordinary text"})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if result.Matched || len(result.Findings) != 0 {
		t.Errorf("result = %+v, want no match", result)
	}
}

func TestInspectRejectsNothing(t *testing.T) {
	_, err := Inspect(InspectRequest{Value: "   "})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestThresholdValidatesAndCompares(t *testing.T) {
	if _, err := Threshold("shouty"); err == nil {
		t.Error("an invented severity was accepted")
	}

	level, err := Threshold("HIGH")
	if err != nil || level != SeverityHigh {
		t.Errorf("Threshold(HIGH) = %q, %v", level, err)
	}

	counts := map[string]int{SeverityMedium: 3}
	if MeetsThreshold(counts, SeverityHigh) {
		t.Error("medium findings met a high threshold")
	}
	if !MeetsThreshold(counts, SeverityLow) {
		t.Error("medium findings did not meet a low threshold")
	}
	if MeetsThreshold(counts, "") {
		t.Error("an empty threshold was met, which would fail every run")
	}
}

func TestRulesAreListedSortedAndDescribed(t *testing.T) {
	listing := Rules()
	if len(listing) == 0 {
		t.Fatal("there are no rules")
	}

	for index := 1; index < len(listing); index++ {
		if listing[index-1].Name > listing[index].Name {
			t.Fatalf("rules are not sorted at %d", index)
		}
	}
	for _, rule := range listing {
		if rule.Description == "" || rule.Severity == "" || rule.Pattern == "" {
			t.Errorf("rule %q is missing part of its description", rule.Name)
		}
		if !validSeverity(rule.Severity) {
			t.Errorf("rule %q has severity %q, which is not one of the four",
				rule.Name, rule.Severity)
		}
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
