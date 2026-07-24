package errors

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
)

func TestNewCarriesCodeAndMessage(t *testing.T) {
	err := New(CodeNotFound, "port %d is not in use", 5173)

	if err.Code != CodeNotFound {
		t.Errorf("Code = %q, want %q", err.Code, CodeNotFound)
	}
	if err.Error() != "port 5173 is not in use" {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestWrapPreservesTheCause(t *testing.T) {
	cause := fs.ErrNotExist
	err := Wrap(cause, CodeIO, "read configuration %s", "config.toml")

	if !Is(err, fs.ErrNotExist) {
		t.Error("Is(err, fs.ErrNotExist) = false, want true; %w chain is broken")
	}
	if !strings.HasPrefix(err.Error(), "read configuration config.toml: ") {
		t.Errorf("Error() = %q, want the wrap text followed by the cause", err.Error())
	}
}

func TestWrapKeepsTheOutermostCode(t *testing.T) {
	inner := New(CodeIO, "write failed")
	outer := Wrap(inner, CodePermissionDenied, "save report")

	if got := CodeOf(outer); got != CodePermissionDenied {
		t.Errorf("CodeOf = %q, want %q", got, CodePermissionDenied)
	}
}

func TestWithHintIsCarriedIntoTheReport(t *testing.T) {
	err := New(CodeInvalidInput, "unsupported output format \"csv\"").
		WithHint("expected one of: table, json")

	report := Classify(err)
	if report.Hint != "expected one of: table, json" {
		t.Errorf("Hint = %q", report.Hint)
	}
}

func TestExitCodeContract(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"success", nil, ExitSuccess},
		{"invalid input", New(CodeInvalidInput, "bad flag"), ExitUsage},
		{"not found", New(CodeNotFound, "missing"), ExitNotFound},
		{"permission denied", New(CodePermissionDenied, "denied"), ExitPermission},
		{"cancelled", New(CodeCancelled, "cancelled"), ExitCancelled},
		{"io error", New(CodeIO, "read failed"), ExitFailure},
		{"parse error", New(CodeParse, "line 3"), ExitFailure},
		{"check failed", New(CodeCheckFailed, "digest mismatch"), ExitFailure},
		{"unclassified", fmt.Errorf("something went wrong"), ExitFailure},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ExitCode(test.err); got != test.want {
				t.Errorf("ExitCode = %d, want %d", got, test.want)
			}
		})
	}
}

func TestClassifyRecognisesStandardLibraryErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Code
	}{
		{"nil", nil, CodeOK},
		{"cancelled", context.Canceled, CodeCancelled},
		{"deadline", context.DeadlineExceeded, CodeTimeout},
		{"not exist", fmt.Errorf("open x: %w", fs.ErrNotExist), CodeNotFound},
		{"permission", fmt.Errorf("open x: %w", os.ErrPermission), CodePermissionDenied},
		{"unknown", fmt.Errorf("boom"), CodeInternal},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Classify(test.err).Code; got != test.want {
				t.Errorf("Classify().Code = %q, want %q", got, test.want)
			}
		})
	}
}

func TestClassifyDetailCarriesTheFullChain(t *testing.T) {
	err := Wrap(fmt.Errorf("access is denied"), CodePermissionDenied, "read %s", "node_modules")

	report := Classify(err)
	if report.Message != "read node_modules" {
		t.Errorf("Message = %q, want only the outermost context", report.Message)
	}
	if !strings.Contains(report.Detail, "access is denied") {
		t.Errorf("Detail = %q, want it to contain the cause", report.Detail)
	}
}

func TestAsFindsTypedErrorThroughStandardWrapping(t *testing.T) {
	err := fmt.Errorf("outer: %w", New(CodeConflict, "already running"))

	var typed *Error
	if !As(err, &typed) {
		t.Fatal("As did not find the typed error")
	}
	if typed.Code != CodeConflict {
		t.Errorf("Code = %q, want %q", typed.Code, CodeConflict)
	}
}
