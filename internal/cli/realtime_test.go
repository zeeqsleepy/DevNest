package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// A monitoring loop checks a site more than once and reports the collected
// series on stdout while each live result goes to stderr.
func TestMonitorPollsUntilTheCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	got := exec(t, nil, append(isolated(t),
		"network", "monitor", server.URL+"/health", "--interval", "20ms", "--count", "3")...)

	if got.err != nil {
		t.Fatalf("Execute: %v", got.err)
	}
	if !strings.Contains(got.stdout, "checks") || !strings.Contains(got.stdout, "up") {
		t.Errorf("stdout = %q, want the poll summary", got.stdout)
	}
	// The live per-check lines belong to stderr, never stdout.
	if !strings.Contains(got.stderr, "up") {
		t.Errorf("stderr = %q, want the live check lines", got.stderr)
	}
}

// The exit code of a polled monitor reflects the last check, so a job does not
// stay red after a site has recovered.
func TestMonitorPollFailsOnTheLastUnhealthyCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	got := exec(t, nil, append(isolated(t),
		"network", "monitor", server.URL+"/", "--expect-status", "404",
		"--interval", "20ms", "--count", "2")...)

	if errors.CodeOf(got.err) != errors.CodeCheckFailed {
		t.Errorf("code = %q, want %q", errors.CodeOf(got.err), errors.CodeCheckFailed)
	}
}

// Without --interval the check is single and --count is ignored; the output is
// the one-result shape, not a series.
func TestMonitorWithoutIntervalIgnoresTheCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	got := exec(t, nil, append(isolated(t),
		"network", "monitor", server.URL+"/", "--count", "5")...)

	if got.err != nil {
		t.Fatalf("Execute: %v", got.err)
	}
	if strings.Contains(got.stdout, "checks") {
		t.Errorf("stdout = %q, want the single-check shape without --interval", got.stdout)
	}
	if got.stderr != "" {
		t.Errorf("stderr = %q, want it quiet for a single check", got.stderr)
	}
}

// The JSON form of a polled run is one envelope holding the whole series.
func TestMonitorPollJSONCarriesTheSeries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	got := exec(t, nil, append(isolated(t),
		"network", "monitor", server.URL+"/", "--interval", "20ms", "--count", "2",
		"--output", "json")...)

	if got.err != nil {
		t.Fatalf("Execute: %v", got.err)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			Checks  int  `json:"checks"`
			Up      int  `json:"up"`
			Healthy bool `json:"healthy"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, got.stdout)
	}
	if envelope.Data.Checks != 2 || envelope.Data.Up != 2 || !envelope.Data.Healthy {
		t.Errorf("series = %+v, want two healthy checks", envelope.Data)
	}
}

// Long-running network commands report each attempt on stderr while the
// summary goes to stdout, so a person sees the run moving.
func TestLatencyAndPingReportLiveProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	address := server.URL[strings.Index(server.URL, "//")+2:]
	_, port, _ := strings.Cut(address, ":")

	latency := exec(t, nil, append(isolated(t),
		"network", "latency", server.URL+"/", "--attempts", "2", "--interval", "10ms")...)
	if latency.err != nil {
		t.Fatalf("latency: %v", latency.err)
	}
	if !strings.Contains(latency.stderr, "attempt 1") {
		t.Errorf("latency stderr = %q, want the live attempt lines", latency.stderr)
	}

	ping := exec(t, nil, append(isolated(t),
		"network", "ping", "127.0.0.1", "--port", port, "--attempts", "2", "--interval", "10ms")...)
	if ping.err != nil {
		t.Fatalf("ping: %v", ping.err)
	}
	if !strings.Contains(ping.stderr, "probe 1") {
		t.Errorf("ping stderr = %q, want the live probe lines", ping.stderr)
	}
}

// log follow streams a tail, so the envelope formats have nothing to carry.
func TestLogFollowRefusesTheEnvelopeFormats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	for _, format := range []string{"json", "csv", "markdown"} {
		got := exec(t, nil, append(isolated(t), "log", "follow", path, "--output", format)...)
		if errors.CodeOf(got.err) != errors.CodeInvalidInput {
			t.Errorf("%s: code = %q, want %q", format, errors.CodeOf(got.err), errors.CodeInvalidInput)
		}
	}

	got := exec(t, nil, append(isolated(t), "log", "follow", path, "--export", filepath.Join(t.TempDir(), "x.md"))...)
	if errors.CodeOf(got.err) != errors.CodeInvalidInput {
		t.Errorf("--export: code = %q, want %q", errors.CodeOf(got.err), errors.CodeInvalidInput)
	}
}

func TestLogFollowNeedsExactlyOneFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("a\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	for _, args := range [][]string{{"log", "follow"}, {"log", "follow", path, "extra"}} {
		got := exec(t, nil, append(isolated(t), args...)...)
		if errors.CodeOf(got.err) != errors.CodeInvalidInput {
			t.Errorf("%v: code = %q, want %q", args, errors.CodeOf(got.err), errors.CodeInvalidInput)
		}
	}
}

// The follow command seeds its tail onto stdout and says what it is doing on
// stderr, then finishes when the run is cancelled.
func TestLogFollowStreamsTheTailToStdout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("first line\nsecond line\nthird line\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr lockedBuffer
	done := make(chan error, 1)

	go func() {
		opts := Options{
			Args:      append(isolated(t), "log", "follow", path, "--lines", "2", "--interval", "20ms"),
			Stdout:    &stdout,
			Stderr:    &stderr,
			LookupEnv: func(string) (string, bool) { return "", false },
		}
		done <- Execute(ctx, opts)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(stdout.String(), "third line") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; errors.CodeOf(err) != errors.CodeCancelled {
		t.Fatalf("Execute: %v, want the run to finish as cancelled", err)
	}

	if !strings.Contains(stdout.String(), "second line") || !strings.Contains(stdout.String(), "third line") {
		t.Errorf("stdout = %q, want the seeded tail", stdout.String())
	}
	if strings.Contains(stdout.String(), "first line") {
		t.Errorf("stdout = %q, the --lines cap should have cut the first line", stdout.String())
	}
	if !strings.Contains(stderr.String(), "following") {
		t.Errorf("stderr = %q, want the follow notice", stderr.String())
	}
}

// lockedBuffer is a bytes.Buffer that one goroutine writes while the test
// reads, so the follow streaming test can watch stdout without the race
// detector flagging it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Working-tree commands report progress on stderr while their result occupies
// stdout.
func TestFileDuplicateAndSecretScanReportLiveProgress(t *testing.T) {
	directory := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("a.txt", "identical")
	write("b.txt", "identical")
	write("plain.md", "nothing to see here\n")

	duplicate := exec(t, nil, append(isolated(t), "file", "duplicate", directory)...)
	if duplicate.err != nil {
		t.Fatalf("duplicate: %v", duplicate.err)
	}
	if !strings.Contains(duplicate.stderr, "compared") {
		t.Errorf("duplicate stderr = %q, want hashing progress", duplicate.stderr)
	}
	if !strings.Contains(duplicate.stdout, "duplicate  b.txt") {
		t.Errorf("duplicate stdout = %q, want the duplicate group", duplicate.stdout)
	}

	scan := exec(t, nil, append(isolated(t), "secret", "scan", directory)...)
	if scan.err != nil {
		t.Fatalf("secret scan: %v", scan.err)
	}
	if !strings.Contains(scan.stderr, "scanned") {
		t.Errorf("secret scan stderr = %q, want walk progress", scan.stderr)
	}
	if !strings.Contains(scan.stdout, "scanned") {
		t.Errorf("secret scan stdout = %q, want the run summary", scan.stdout)
	}
}

// --quiet silences progress everywhere, while the result still arrives.
func TestQuietSilencesProgressNotResults(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "a.txt"), []byte("ident"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := exec(t, nil, append(isolated(t), "--quiet", "file", "duplicate", directory)...)
	if got.err != nil {
		t.Fatalf("Execute: %v", got.err)
	}
	if got.stderr != "" {
		t.Errorf("stderr = %q, want no progress under --quiet", got.stderr)
	}
	if !strings.Contains(got.stdout, "No duplicates") {
		t.Errorf("stdout = %q, want the result regardless of --quiet", got.stdout)
	}
}
