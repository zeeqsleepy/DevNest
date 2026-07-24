package doctor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/proc"
	"github.com/devnest/devnest/internal/platform/sys"
)

// fakeEnvironment is a machine that answers whatever a test needs it to, and
// never touches the real one.
type fakeEnvironment struct {
	missing     map[string]bool
	writableErr error
	info        sys.Info
	located     map[string][]string
	output      proc.Output
	runErr      error
	commands    []proc.Command
}

func newFake() *fakeEnvironment {
	return &fakeEnvironment{
		missing: map[string]bool{},
		info: sys.Info{
			OS:           "linux",
			Architecture: "amd64",
			GoVersion:    "go1.24.0",
			Hostname:     "someones-laptop",
			Home:         "/home/someone",
			Shell:        "bash",
			Terminal:     "xterm-256color",
		},
		located: map[string][]string{"git": {"/usr/bin/git"}},
		output:  proc.Output{Stdout: "git version 2.45.1\n"},
	}
}

func (f *fakeEnvironment) Exists(path string) (bool, error) { return !f.missing[path], nil }

func (f *fakeEnvironment) Writable(string) error { return f.writableErr }

func (f *fakeEnvironment) Describe() sys.Info { return f.info }

func (f *fakeEnvironment) Lookup(name string) []string { return f.located[name] }

func (f *fakeEnvironment) Run(_ context.Context, command proc.Command) (proc.Output, error) {
	f.commands = append(f.commands, command)
	return f.output, f.runErr
}

// writeConfig puts a real file on disk, because the configuration check loads
// it the same way a real command does and that is the point of the check.
func writeConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func request(path string) Request {
	return Request{
		ConfigPath:     path,
		ConfigExplicit: true,
		RuleSets:       []RuleSet{{Name: "secret", Count: 16}, {Name: "clean", Count: 12}},
		OutputFormat:   "table",
	}
}

func find(t *testing.T, result Result, name string) Check {
	t.Helper()

	for _, check := range result.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("no check named %q in %+v", name, result.Checks)
	return Check{}
}

func TestHealthyInstallationPassesEveryCheck(t *testing.T) {
	fake := newFake()
	result := Run(context.Background(), fake, request(writeConfig(t, "[general]\noutput = \"json\"\n")))

	if !result.Healthy || result.Failed != 0 {
		t.Fatalf("healthy = %v, failed = %d, checks = %+v", result.Healthy, result.Failed, result.Checks)
	}
	if got := find(t, result, "git"); got.Status != StatusOK || !strings.Contains(got.Detail, "2.45.1") {
		t.Errorf("git check = %+v", got)
	}
	if len(fake.commands) != 1 || fake.commands[0].Timeout == 0 {
		t.Errorf("the version probe was unbounded or did not run: %+v", fake.commands)
	}
}

// A file that will not parse is the single most common reason DevNest refuses
// to start, which is why doctor loads it rather than only looking for it.
func TestUnparseableConfigFails(t *testing.T) {
	result := Run(context.Background(), newFake(), request(writeConfig(t, "general = [[[")))

	if got := find(t, result, "configuration"); got.Status != StatusFailed {
		t.Errorf("configuration check = %+v, want a failure", got)
	}
	if result.Healthy {
		t.Error("healthy = true with a broken configuration file")
	}
}

// A value the schema rejects passes the parser and fails the command, so the
// check has to validate as well as load.
func TestInvalidConfigValueFails(t *testing.T) {
	result := Run(context.Background(), newFake(), request(writeConfig(t, "[general]\noutput = \"pdf\"\n")))

	if got := find(t, result, "configuration"); got.Status != StatusFailed {
		t.Errorf("configuration check = %+v, want a failure", got)
	}
}

// Most machines that run DevNest never call git, so its absence must not make
// the installation look broken.
func TestMissingGitWarnsAndStaysHealthy(t *testing.T) {
	fake := newFake()
	fake.located = map[string][]string{}

	result := Run(context.Background(), fake, request(writeConfig(t, "")))

	got := find(t, result, "git")
	if got.Status != StatusWarning {
		t.Errorf("git check = %+v, want a warning", got)
	}
	if !result.Healthy || result.Warned != 1 {
		t.Errorf("healthy = %v, warned = %d", result.Healthy, result.Warned)
	}
}

// A tool that is installed and will not answer is a different problem from one
// that is not installed, and gets said differently.
func TestUnresponsiveToolIsReportedAsSuch(t *testing.T) {
	fake := newFake()
	fake.runErr = errors.New(errors.CodeTimeout, "timed out")

	result := Run(context.Background(), fake, request(writeConfig(t, "")))

	got := find(t, result, "git")
	if got.Status != StatusWarning || !strings.Contains(got.Detail, "would not report a version") {
		t.Errorf("git check = %+v", got)
	}
}

func TestUnwritableConfigDirectoryFails(t *testing.T) {
	fake := newFake()
	fake.writableErr = errors.New(errors.CodePermissionDenied, "cannot write to it")

	result := Run(context.Background(), fake, request(writeConfig(t, "")))

	if got := find(t, result, "configuration directory"); got.Status != StatusFailed {
		t.Errorf("directory check = %+v, want a failure", got)
	}
	if result.Healthy {
		t.Error("healthy = true with an unwritable configuration directory")
	}
}

// An empty rule table means a scan that finds nothing and reports success,
// which is the quietest way this tool could be broken.
func TestEmptyRuleSetFails(t *testing.T) {
	req := request(writeConfig(t, ""))
	req.RuleSets = []RuleSet{{Name: "secret", Count: 0}}

	result := Run(context.Background(), newFake(), req)

	got := find(t, result, "rule sets")
	if got.Status != StatusFailed || !strings.Contains(got.Detail, "secret") {
		t.Errorf("rule set check = %+v", got)
	}
}

// The report is written to be pasted into a public issue.
func TestReportIdentifiesNobody(t *testing.T) {
	fake := newFake()
	fake.located = map[string][]string{"git": {"/home/someone/bin/git"}}

	result := Run(context.Background(), fake, request(writeConfig(t, "")))

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, leaked := range []string{"someones-laptop", "/home/someone"} {
		if strings.Contains(string(encoded), leaked) {
			t.Errorf("the report contains %q:\n%s", leaked, encoded)
		}
	}
	if got := find(t, result, "git"); !strings.Contains(got.Detail, "~/bin/git") {
		t.Errorf("git path = %q, want it shortened to ~", got.Detail)
	}
}

// A file the user named with --config has to be there. Silently falling back
// to the defaults is how a person spends an afternoon wondering why a setting
// does nothing.
func TestExplicitConfigThatIsMissingFails(t *testing.T) {
	fake := newFake()
	path := filepath.Join(t.TempDir(), "nowhere.toml")
	fake.missing[path] = true

	result := Run(context.Background(), fake, request(path))

	if got := find(t, result, "configuration"); got.Status != StatusFailed {
		t.Errorf("configuration check = %+v, want a failure", got)
	}
}

// The default location holding no file is the ordinary state of a fresh
// installation, not a problem.
func TestAbsentDefaultConfigIsFine(t *testing.T) {
	fake := newFake()
	path := filepath.Join(t.TempDir(), "config.toml")
	fake.missing[path] = true

	req := request(path)
	req.ConfigExplicit = false

	result := Run(context.Background(), fake, req)

	if got := find(t, result, "configuration"); got.Status != StatusOK {
		t.Errorf("configuration check = %+v, want it to pass", got)
	}
}
