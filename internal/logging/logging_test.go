package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input   string
		want    slog.Level
		wantErr bool
	}{
		{"debug", slog.LevelDebug, false},
		{"info", slog.LevelInfo, false},
		{"warn", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"ERROR", slog.LevelError, false},
		{" info ", slog.LevelInfo, false},
		{"trace", 0, true},
		{"", 0, true},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, err := ParseLevel(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseLevel(%q) = %v, want an error", test.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLevel(%q): %v", test.input, err)
			}
			if got != test.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", test.input, got, test.want)
			}
		})
	}
}

func TestParseFormat(t *testing.T) {
	if format, err := ParseFormat("json"); err != nil || format != FormatJSON {
		t.Errorf("ParseFormat(\"json\") = %v, %v", format, err)
	}
	if _, err := ParseFormat("xml"); err == nil {
		t.Error("ParseFormat(\"xml\") returned no error")
	}
}

func TestTextHandlerFormat(t *testing.T) {
	var buffer bytes.Buffer
	logger := New(&buffer, Options{Level: slog.LevelDebug, Format: FormatText})

	logger.Warn("permission denied, skipping", "path", `C:\projects\api`)

	line := buffer.String()
	if !strings.HasPrefix(line, "warn ") {
		t.Errorf("line = %q, want it to start with the level", line)
	}
	if !strings.Contains(line, "permission denied, skipping") {
		t.Errorf("line = %q, want it to contain the message", line)
	}
	if !strings.Contains(line, `path=C:\projects\api`) {
		t.Errorf("line = %q, want the attribute as key=value", line)
	}
	if !strings.HasSuffix(line, "\n") {
		t.Errorf("line = %q, want a trailing newline", line)
	}
}

func TestTextHandlerQuotesValuesContainingSpaces(t *testing.T) {
	var buffer bytes.Buffer
	logger := New(&buffer, Options{Level: slog.LevelInfo, Format: FormatText})

	logger.Info("scan complete", "path", "C:\\my projects\\api")

	if !strings.Contains(buffer.String(), `path="C:\my projects\api"`) {
		t.Errorf("line = %q, want the value quoted", buffer.String())
	}
}

func TestTextHandlerOmitsTimestampsByDefault(t *testing.T) {
	var withoutTimestamps, withTimestamps bytes.Buffer

	New(&withoutTimestamps, Options{Level: slog.LevelInfo}).Info("scan complete")
	New(&withTimestamps, Options{Level: slog.LevelInfo, Timestamps: true}).Info("scan complete")

	if strings.Contains(withoutTimestamps.String(), "T") {
		t.Errorf("line = %q, want no timestamp by default", withoutTimestamps.String())
	}
	if !strings.Contains(withTimestamps.String(), "T") {
		t.Errorf("line = %q, want a timestamp when enabled", withTimestamps.String())
	}
}

func TestTextHandlerColorOnlyWhenEnabled(t *testing.T) {
	var plain, colored bytes.Buffer

	New(&plain, Options{Level: slog.LevelError}).Error("failed")
	New(&colored, Options{Level: slog.LevelError, Color: true}).Error("failed")

	if strings.Contains(plain.String(), "\x1b[") {
		t.Errorf("line = %q, want no escape sequences", plain.String())
	}
	if !strings.Contains(colored.String(), "\x1b[31m") {
		t.Errorf("line = %q, want the error level coloured", colored.String())
	}
}

func TestLevelFiltering(t *testing.T) {
	var buffer bytes.Buffer
	logger := New(&buffer, Options{Level: slog.LevelWarn})

	logger.Debug("not shown")
	logger.Info("not shown")
	logger.Warn("shown")
	logger.Error("shown")

	lines := strings.Count(buffer.String(), "\n")
	if lines != 2 {
		t.Errorf("wrote %d lines, want 2; debug and info must be filtered out", lines)
	}
}

func TestJSONHandlerEmitsOneObjectPerLine(t *testing.T) {
	var buffer bytes.Buffer
	logger := New(&buffer, Options{Level: slog.LevelInfo, Format: FormatJSON})

	logger.Warn("permission denied", "path", "node_modules")
	logger.Info("scan complete", "count", 42)

	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrote %d lines, want 2", len(lines))
	}

	var record map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("line is not valid JSON: %v", err)
	}
	for _, key := range []string{"time", "level", "msg", "path"} {
		if _, ok := record[key]; !ok {
			t.Errorf("missing key %q in %s", key, lines[0])
		}
	}
}

func TestWithAttrsDoesNotLeakBetweenLoggers(t *testing.T) {
	var buffer bytes.Buffer
	base := New(&buffer, Options{Level: slog.LevelInfo})

	base.With("command", "scan").Info("first")
	base.Info("second")

	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrote %d lines, want 2", len(lines))
	}
	if !strings.Contains(lines[0], "command=scan") {
		t.Errorf("line = %q, want the attached attribute", lines[0])
	}
	if strings.Contains(lines[1], "command=scan") {
		t.Errorf("line = %q, want the base logger unaffected", lines[1])
	}
}

func TestNopLoggerWritesNothing(t *testing.T) {
	logger := Nop()
	// The point of the test is that this cannot panic and has nowhere to write.
	logger.Error("ignored", "key", "value")
}
