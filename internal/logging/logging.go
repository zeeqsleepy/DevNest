// Package logging builds the structured logger used across DevNest.
//
// Two rules shape everything here: log records always go to stderr, never to
// stdout, and message text is a fixed string with everything variable carried
// as a key-value attribute.
//
// Levels, filtering and attribute handling come from log/slog. This package
// adds the human-readable handler, because slog's own text output is logfmt
// with a timestamp on every line, which is the wrong shape for a CLI.
package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// Format selects the handler used for log records.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// Options configures a logger.
type Options struct {
	Level      slog.Level
	Format     Format
	Color      bool
	Timestamps bool
}

// New returns a logger writing to w.
func New(w io.Writer, opts Options) *slog.Logger {
	if opts.Format == FormatJSON {
		return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: opts.Level}))
	}
	return slog.New(&textHandler{
		w:          w,
		mu:         &sync.Mutex{},
		level:      opts.Level,
		color:      opts.Color,
		timestamps: opts.Timestamps,
	})
}

// Nop returns a logger that discards everything. It is the default in tests,
// so a package under test stays silent unless a test asks for records.
func Nop() *slog.Logger { return slog.New(slog.DiscardHandler) }

// ParseLevel converts a configured verbosity name into a level.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, errors.New(errors.CodeInvalidInput,
		"unknown verbosity %q, expected one of: debug, info, warn, error", name)
}

// ParseFormat converts a configured log format name into a Format.
func ParseFormat(name string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "text":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	}
	return "", errors.New(errors.CodeInvalidInput,
		"unknown log format %q, expected one of: text, json", name)
}

const (
	levelColumn   = 7  // width of the level column, including its trailing gap
	messageColumn = 30 // column at which attributes start
	colorReset    = "\x1b[0m"
)

// textHandler renders records as "level  message  key=value", which is the
// format documented in docs/logging.md.
type textHandler struct {
	w          io.Writer
	mu         *sync.Mutex
	level      slog.Level
	color      bool
	timestamps bool
	attrs      []slog.Attr
	group      string
}

func (h *textHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *textHandler) Handle(_ context.Context, record slog.Record) error {
	var line strings.Builder

	if h.timestamps {
		line.WriteString(record.Time.UTC().Format(time.RFC3339))
		line.WriteByte(' ')
	}

	label := levelLabel(record.Level)
	if color := levelColor(record.Level); h.color && color != "" {
		line.WriteString(color)
		line.WriteString(label)
		line.WriteString(colorReset)
	} else {
		line.WriteString(label)
	}
	line.WriteString(pad(levelColumn - len(label)))
	line.WriteString(record.Message)

	if pairs := h.pairs(record); len(pairs) > 0 {
		line.WriteString(pad(messageColumn - len(record.Message)))
		line.WriteString(strings.Join(pairs, " "))
	}
	line.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, line.String())
	return err
}

func (h *textHandler) pairs(record slog.Record) []string {
	pairs := make([]string, 0, len(h.attrs)+record.NumAttrs())
	for _, attr := range h.attrs {
		pairs = append(pairs, formatAttr(h.group, attr))
	}
	record.Attrs(func(attr slog.Attr) bool {
		pairs = append(pairs, formatAttr(h.group, attr))
		return true
	})
	return pairs
}

func (h *textHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	next := *h
	next.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &next
}

// WithGroup prefixes subsequent attribute keys with the group name. DevNest
// does not use groups; this satisfies the slog.Handler contract cheaply. If
// nested grouping is ever needed, this is the place to build a proper tree.
func (h *textHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := *h
	next.group = h.group + name + "."
	return &next
}

func formatAttr(group string, attr slog.Attr) string {
	value := attr.Value.Resolve().String()
	if strings.ContainsAny(value, " \t\"") {
		value = `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return group + attr.Key + "=" + value
}

func levelLabel(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "error"
	case level >= slog.LevelWarn:
		return "warn"
	case level >= slog.LevelInfo:
		return "info"
	default:
		return "debug"
	}
}

func levelColor(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "\x1b[31m"
	case level >= slog.LevelWarn:
		return "\x1b[33m"
	default:
		return ""
	}
}

func pad(width int) string {
	if width < 1 {
		width = 1
	}
	return strings.Repeat(" ", width)
}
