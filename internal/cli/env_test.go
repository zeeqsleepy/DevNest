package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/logging"
	"github.com/devnest/devnest/internal/output"
)

func newTestEnv(t *testing.T, format string) (*Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	renderer, err := output.NewRenderer(format)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	var stdout, stderr bytes.Buffer

	return &Env{
		Renderer: renderer,
		Logger:   logging.New(&stderr, logging.Options{Level: slog.LevelWarn}),
		Stdout:   &stdout,
		Stderr:   &stderr,
		command:  "version",
		started:  time.Now(),
	}, &stdout, &stderr
}

// A warning has to survive both audiences: a person watching stderr and a
// script reading the envelope on stdout.
func TestWarnReachesBothTheLogAndTheEnvelope(t *testing.T) {
	env, stdout, stderr := newTestEnv(t, "json")

	env.Warn(errors.CodePermissionDenied, "cannot read directory", "path", "node_modules")
	if err := env.Emit(map[string]int{"count": 1}, nil); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	var envelope struct {
		Status   string           `json:"status"`
		Warnings []output.Warning `json:"warnings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}

	if envelope.Status != output.StatusWarning {
		t.Errorf("status = %q, want %q", envelope.Status, output.StatusWarning)
	}
	if len(envelope.Warnings) != 1 {
		t.Fatalf("warnings = %v, want one", envelope.Warnings)
	}
	if envelope.Warnings[0].Code != string(errors.CodePermissionDenied) {
		t.Errorf("code = %q, want %q", envelope.Warnings[0].Code, errors.CodePermissionDenied)
	}
	if stderr.Len() == 0 {
		t.Error("stderr is empty; the warning should also be logged")
	}
}

func TestEmitRecordsCommandAndDuration(t *testing.T) {
	env, stdout, _ := newTestEnv(t, "json")
	env.started = time.Now().Add(-50 * time.Millisecond)

	if err := env.Emit(map[string]string{"a": "b"}, nil); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	var envelope struct {
		DevNest output.Meta `json:"devnest"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}

	if envelope.DevNest.Command != "version" {
		t.Errorf("command = %q, want \"version\"", envelope.DevNest.Command)
	}
	if envelope.DevNest.DurationMs < 50 {
		t.Errorf("durationMs = %d, want at least 50", envelope.DevNest.DurationMs)
	}
	if envelope.DevNest.Version == "" {
		t.Error("version is empty")
	}
}

func TestEmitUsesTheTextViewInTableMode(t *testing.T) {
	env, stdout, _ := newTestEnv(t, "table")

	err := env.Emit(nil, func(w io.Writer) error {
		_, err := io.WriteString(w, "all good\n")
		return err
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if stdout.String() != "all good\n" {
		t.Errorf("stdout = %q", stdout.String())
	}
}
