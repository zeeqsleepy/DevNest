// Package output renders a command's result.
//
// Every command produces plain data plus a function that writes the
// human-readable view of it. The renderers below are two views of that one
// value, which is what keeps the table and the JSON from ever disagreeing.
package output

import (
	"io"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// Status values carried by the envelope.
const (
	StatusOK      = "ok"
	StatusWarning = "warning"
	StatusError   = "error"
)

// Meta identifies the invocation that produced a result.
type Meta struct {
	Version    string    `json:"version"`
	Command    string    `json:"command"`
	StartedAt  time.Time `json:"startedAt"`
	DurationMs int64     `json:"durationMs"`
}

// Warning is a non-fatal problem encountered during a command.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

// ErrorInfo is the machine-readable form of a failure.
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
	Hint    string `json:"hint,omitempty"`
}

// Envelope wraps every result so that consumers can write one piece of
// handling code that works for any command. Its shape is documented in
// docs/schema.md and is part of the compatibility contract.
type Envelope struct {
	DevNest  Meta       `json:"devnest"`
	Status   string     `json:"status"`
	Data     any        `json:"data"`
	Warnings []Warning  `json:"warnings"`
	Error    *ErrorInfo `json:"error"`
}

// NewEnvelope returns an envelope with the required fields populated and an
// empty warning list, so consumers never have to null-check before iterating.
func NewEnvelope(meta Meta, data any) Envelope {
	return Envelope{
		DevNest:  meta,
		Status:   StatusOK,
		Data:     data,
		Warnings: []Warning{},
	}
}

// WithWarnings attaches warnings and promotes the status accordingly.
func (e Envelope) WithWarnings(warnings []Warning) Envelope {
	if len(warnings) == 0 {
		return e
	}
	e.Warnings = warnings
	if e.Status == StatusOK {
		e.Status = StatusWarning
	}
	return e
}

// WithError marks the envelope as a failure. Data is cleared, because a failed
// command has no result to report.
func (e Envelope) WithError(info ErrorInfo) Envelope {
	e.Status = StatusError
	e.Data = nil
	e.Error = &info
	return e
}

// TextFunc writes the human-readable view of a result. The CLI layer supplies
// it alongside the data, because how something looks is an interface concern
// and not the business of the code that produced it.
type TextFunc func(w io.Writer) error

// Renderer turns an envelope into bytes.
type Renderer interface {
	// Name is the value of --output that selects this renderer.
	Name() string
	// Render writes the result. Renderers that present data for people use
	// text; renderers that emit structured data ignore it.
	Render(w io.Writer, envelope Envelope, text TextFunc) error
}

// NewRenderer returns the renderer selected by --output.
func NewRenderer(name string) (Renderer, error) {
	switch name {
	case "table", "text":
		return textRenderer{}, nil
	case "json":
		return jsonRenderer{}, nil
	case "csv":
		return csvRenderer{}, nil
	}
	return nil, errors.New(errors.CodeInvalidInput, "unsupported output format %q", name).
		WithHint("expected one of: table, json, csv")
}
