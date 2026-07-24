package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/config"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
)

func networkEnv(t *testing.T) *Env {
	t.Helper()
	env, _, _ := newTestEnv(t, "table")
	env.Config = config.Default()
	return env
}

func TestNetworkSystemTakesTheTimeoutFromConfiguration(t *testing.T) {
	env := networkEnv(t)
	env.Config.Network.TimeoutMs = 5000

	var flags networkFlags
	system := flags.system(env, true, 0)

	if system.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", system.Timeout)
	}
}

// A flag is the highest-precedence source; that is the whole point of the
// four-layer resolution.
func TestNetworkSystemPrefersTheFlagOverConfiguration(t *testing.T) {
	env := networkEnv(t)
	env.Config.Network.TimeoutMs = 30000

	flags := networkFlags{timeout: 2 * time.Second}
	system := flags.system(env, true, 0)

	if system.Timeout != 2*time.Second {
		t.Errorf("Timeout = %v, want the flag value 2s", system.Timeout)
	}
}

func TestNetworkSystemRedirectLimit(t *testing.T) {
	env := networkEnv(t)
	env.Config.Network.MaxRedirects = 7

	var flags networkFlags

	fromConfig := flags.system(env, true, 0)
	if fromConfig.MaxRedirects != 7 {
		t.Errorf("MaxRedirects = %d, want 7", fromConfig.MaxRedirects)
	}

	fromFlag := flags.system(env, true, 2)
	if fromFlag.MaxRedirects != 2 {
		t.Errorf("MaxRedirects = %d, want the flag value 2", fromFlag.MaxRedirects)
	}
}

func TestNetworkSystemCarriesTheInsecureFlagAndAUserAgent(t *testing.T) {
	env := networkEnv(t)

	flags := networkFlags{insecure: true}
	system := flags.system(env, false, 0)

	if !system.Insecure {
		t.Error("Insecure was not passed through")
	}
	if system.FollowRedirects {
		t.Error("FollowRedirects = true although the caller asked for false")
	}
	if !strings.HasPrefix(system.UserAgent, "devnest/") {
		t.Errorf("UserAgent = %q, want it to identify DevNest", system.UserAgent)
	}
}

// Disabling verification has to be visible every single time. Habituation is
// the actual risk, which is why this is a warning rather than a second flag.
func TestWarnInsecureRecordsAWarning(t *testing.T) {
	env := networkEnv(t)

	quiet := networkFlags{}
	quiet.warnInsecure(env)
	if len(env.warnings) != 0 {
		t.Errorf("warnings = %v, want none without --insecure", env.warnings)
	}

	loud := networkFlags{insecure: true}
	loud.warnInsecure(env)
	if len(env.warnings) != 1 {
		t.Fatalf("warnings = %v, want one", env.warnings)
	}
	if !strings.Contains(env.warnings[0].Message, "verification") {
		t.Errorf("warning = %q, want it to say what was disabled", env.warnings[0].Message)
	}
}

func TestAttemptsAndIntervalPreferTheFlag(t *testing.T) {
	env := networkEnv(t)
	env.Config.Network.Attempts = 3
	env.Config.Network.IntervalMs = 200

	if got := attemptsOf(env, 0); got != 3 {
		t.Errorf("attemptsOf(unset) = %d, want the configured 3", got)
	}
	if got := attemptsOf(env, 10); got != 10 {
		t.Errorf("attemptsOf(10) = %d, want the flag value", got)
	}

	if got := intervalOf(env, 0); got != 200*time.Millisecond {
		t.Errorf("intervalOf(unset) = %v, want the configured 200ms", got)
	}
	if got := intervalOf(env, time.Second); got != time.Second {
		t.Errorf("intervalOf(1s) = %v, want the flag value", got)
	}
}

func TestRequestBodyFromAString(t *testing.T) {
	body, err := requestBody(`{"a":1}`, "")
	if err != nil {
		t.Fatalf("requestBody: %v", err)
	}
	if string(body) != `{"a":1}` {
		t.Errorf("body = %q", body)
	}
}

func TestRequestBodyFromAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(path, []byte(`{"from":"file"}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	body, err := requestBody("", path)
	if err != nil {
		t.Fatalf("requestBody: %v", err)
	}
	if string(body) != `{"from":"file"}` {
		t.Errorf("body = %q", body)
	}
}

// Preferring one silently would mean the request carried something the user
// did not intend.
func TestRequestBodyRefusesBothSources(t *testing.T) {
	_, err := requestBody("inline", "some-file")
	if errors.CodeOf(err) != errors.CodeInvalidInput {
		t.Errorf("code = %q, want %q", errors.CodeOf(err), errors.CodeInvalidInput)
	}
}

func TestRequestBodyMissingFileIsNotFound(t *testing.T) {
	_, err := requestBody("", filepath.Join(t.TempDir(), "absent.json"))
	if errors.CodeOf(err) != errors.CodeNotFound {
		t.Errorf("code = %q, want %q", errors.CodeOf(err), errors.CodeNotFound)
	}
}

func TestRequestBodyEmptyWhenNeitherIsGiven(t *testing.T) {
	body, err := requestBody("", "")
	if err != nil {
		t.Fatalf("requestBody: %v", err)
	}
	if body != nil {
		t.Errorf("body = %q, want nil", body)
	}
}

func TestNoteForAnEmptyAnswer(t *testing.T) {
	if got := noteFor(net.Answer{Kind: "MX"}); got != "none" {
		t.Errorf("noteFor = %q, want %q", got, "none")
	}
	if got := noteFor(net.Answer{Kind: "MX", Error: "timed out"}); got != "timed out" {
		t.Errorf("noteFor = %q", got)
	}
}

func TestYesNo(t *testing.T) {
	if yesNo(true) != "yes" || yesNo(false) != "no" {
		t.Error("yesNo is wrong")
	}
}

func TestPluralRecords(t *testing.T) {
	if got := pluralRecords(1); got != "1 record" {
		t.Errorf("pluralRecords(1) = %q", got)
	}
	if got := pluralRecords(1500); got != "1,500 records" {
		t.Errorf("pluralRecords(1500) = %q", got)
	}
}
