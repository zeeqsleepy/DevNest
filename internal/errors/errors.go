// Package errors defines DevNest's error model: a stable classification code
// carried by every error, helpers that attach it while wrapping, and the
// mapping from an error to a process exit code.
//
// Callers branch on Code. They never match on message text, because message
// wording improves between releases and codes do not.
package errors

import (
	"context"
	stderrors "errors"
	"fmt"
	"io/fs"
	"os"
)

// Code classifies an error. Codes are part of the JSON output contract and do
// not change within a major version.
type Code string

const (
	CodeOK               Code = "OK"
	CodeInternal         Code = "INTERNAL"
	CodeInvalidInput     Code = "INVALID_INPUT"
	CodeNotFound         Code = "NOT_FOUND"
	CodePermissionDenied Code = "PERMISSION_DENIED"
	CodeCancelled        Code = "CANCELLED"
	CodeIO               Code = "IO_ERROR"
	CodeParse            Code = "PARSE_ERROR"
	CodeNetwork          Code = "NETWORK_ERROR"
	CodeTimeout          Code = "TIMEOUT"
	CodeUnsupported      Code = "UNSUPPORTED"
	CodeConflict         Code = "CONFLICT"
	CodeCheckFailed      Code = "CHECK_FAILED"
)

// Process exit codes. Documented in docs/error-handling.md and relied on by
// scripts, so they are covered by tests and frozen for the major version.
const (
	ExitSuccess    = 0
	ExitFailure    = 1
	ExitUsage      = 2
	ExitNotFound   = 3
	ExitPermission = 4
	ExitCancelled  = 5
)

var exitCodes = map[Code]int{
	CodeOK:               ExitSuccess,
	CodeInvalidInput:     ExitUsage,
	CodeNotFound:         ExitNotFound,
	CodePermissionDenied: ExitPermission,
	CodeCancelled:        ExitCancelled,
}

// Error carries a classification code, a message written for the user, and an
// optional hint naming the next action.
type Error struct {
	Code    Code
	Message string
	Hint    string

	cause error
}

// New returns an Error with the given code and formatted message.
func New(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap returns an Error that classifies cause and adds context the caller
// could not know, such as the operation and the path it acted on.
func Wrap(cause error, code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), cause: cause}
}

// WithHint attaches a hint describing what the user can do next.
func (e *Error) WithHint(format string, args ...any) *Error {
	e.Hint = fmt.Sprintf(format, args...)
	return e
}

func (e *Error) Error() string {
	if e.cause == nil {
		return e.Message
	}
	return e.Message + ": " + e.cause.Error()
}

// Unwrap exposes the wrapped cause so errors.Is and errors.As keep working.
func (e *Error) Unwrap() error { return e.cause }

// Re-exported so callers need only this package to inspect an error chain.
var (
	Is   = stderrors.Is
	As   = stderrors.As
	Join = stderrors.Join
)

// CodeOf returns the classification for err.
func CodeOf(err error) Code { return Classify(err).Code }

// ExitCode returns the process exit code for err. Anything without a specific
// mapping is a general failure.
func ExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}
	if code, ok := exitCodes[CodeOf(err)]; ok {
		return code
	}
	return ExitFailure
}

// Report is the presentation of an error: what the user reads, plus the
// diagnostic detail shown only under --verbose.
type Report struct {
	Code    Code
	Message string
	Hint    string
	Detail  string
}

// Classify turns any error into a Report. Errors created by this package keep
// the code they were given; well-known standard library errors are recognised;
// anything else is reported as an internal error, which is the signal that a
// layer forgot to classify something.
func Classify(err error) Report {
	if err == nil {
		return Report{Code: CodeOK}
	}

	detail := err.Error()

	var typed *Error
	if stderrors.As(err, &typed) {
		return Report{Code: typed.Code, Message: typed.Message, Hint: typed.Hint, Detail: detail}
	}

	switch {
	case stderrors.Is(err, context.Canceled):
		return Report{Code: CodeCancelled, Message: "cancelled", Detail: detail}
	case stderrors.Is(err, context.DeadlineExceeded):
		return Report{Code: CodeTimeout, Message: "timed out", Detail: detail}
	case stderrors.Is(err, fs.ErrNotExist):
		return Report{Code: CodeNotFound, Message: detail, Detail: detail}
	case stderrors.Is(err, fs.ErrPermission), stderrors.Is(err, os.ErrPermission):
		return Report{Code: CodePermissionDenied, Message: detail, Detail: detail}
	}

	return Report{Code: CodeInternal, Message: detail, Detail: detail}
}
