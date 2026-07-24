# Logging

Status: implemented in Phase 1
Last revised: 2026-07-23

Logging in a CLI has one property that makes it different from logging in a service: the user is
watching. Every line has to earn its place on their screen, and none of it may interfere with the
output they actually asked for.

## The rules that everything follows from

**1. Logs go to stderr. Always. Without exception.** No verbosity level, no output format, no
special case puts a log line on stdout. stdout carries the command's result and nothing else.

This is what makes `devnest scan --output json | jq` behave identically with and without
`--verbose`. Breaking it breaks every script anyone has written.

**2. Logs are diagnostics, not results.** If information belongs in the output, it goes in the
`Result`. If it belongs in a report, it goes in `Result.Warnings`. Logging is for explaining what
the program is doing while it does it.

**3. Default verbosity is nearly silent.** At the default level a successful command prints its
result and nothing else. Progress on a long operation is the only exception.

## Levels

| Level | When | Visible by default |
|---|---|---|
| `error` | The operation failed | Yes |
| `warn` | Something was skipped or degraded, work continued | Yes |
| `info` | Milestones in a long operation | No |
| `debug` | Detail for diagnosing a problem | No |

Default is `warn`. `--verbose` sets `debug`. `--quiet` sets `error`. The config key
`general.verbosity` sets the baseline, and flags override it.

There is no `trace` and no `fatal`. Four levels is enough; more produces inconsistent
categorisation between contributors. Fatal is not a log level: a fatal condition is an error
returned up the stack, and only `main` exits.

### What belongs at each level

**error**: the operation is failing. One per failure, emitted where the error is finally handled,
not at every level it passes through. An error logged three times as it unwinds looks like three
problems.

**warn**: something was skipped and the user should know: an unreadable file during a walk, a
toolchain probe that timed out, a symlink loop, a config key that was not recognised. These also
appear in `Result.Warnings`; the log line is for someone watching, the warnings array is for the
record.

**info**: milestones a user might want to see during a long run: "scanning C:\projects\api",
"38,412 files walked", "applying 12 removals". Not per-file.

**debug**: per-item detail, resolved configuration values and their origins, timing breakdowns,
platform quirks encountered, subprocess command lines and exit codes. This is what someone attaches
to a bug report.

## Formats

**Text**: the default when stderr is a terminal.

```
warn  permission denied, skipping   path=C:\projects\api\node_modules\.cache
info  scan complete                 files=38412 bytes=2469606195 durationMs=1834
```

Level first, then the message, then key-value pairs. Aligned so the eye can scan down the level
column. Colour on the level only (red for error, yellow for warn) dropped when stderr is not a
terminal or `NO_COLOR` is set.

No timestamps by default. A command that runs for 300 milliseconds does not need a wall-clock time
on every line, and the noise costs more than it gives. `--log-timestamps` adds them for the rare
case of correlating with another system.

**JSON**: one object per line, when `--output json` or `--log-format json` is set.

```json
{"time":"2026-07-23T09:14:02.481Z","level":"WARN","msg":"permission denied, skipping","path":"C:\\projects\\api\\node_modules\\.cache"}
```

This is `log/slog`'s own JSON handler, so the keys are slog's: `time`, `level`, `msg`, then the
attributes. Reusing it rather than renaming the keys means the records interoperate with every tool
that already understands slog output, which is worth more than matching DevNest's own field-naming
convention in a stream that is diagnostics rather than results.

Timestamps are always present in JSON output, since something is consuming it. `--output json`
switching the log handler to JSON automatically means a pipeline capturing both streams gets
machine-readable data on both. `--log-format` overrides that pairing in either direction.

## Structure

Structured key-value fields, never formatted into the message string.

```go
// good: the path is a field, queryable
logger.Warn("permission denied, skipping", "path", path)

// bad: the path is embedded in prose, only greppable
logger.Warn(fmt.Sprintf("permission denied, skipping %s", path))
```

Message text is a fixed string. Everything variable is a field. This is what makes
`devnest scan --output json 2>&1 | jq 'select(.level=="warn") | .path'` work.

Field name conventions, used consistently across the codebase:

| Field | Meaning |
|---|---|
| `path` | A filesystem path, always absolute |
| `count` | A quantity of items |
| `bytes` | A size in bytes, always an integer |
| `durationMs` | An elapsed duration in milliseconds, always an integer |
| `error` | An error's message text |
| `code` | An `errors.Code` value |
| `command` | The command being run |
| `pid` | A process identifier |
| `port` | A port number |

Message text is lowercase without trailing punctuation, matching the error-string convention in
`coding-standard.md`.

## Redaction

**No secret value ever reaches a log field.** Not at `debug`, not with `--verbose`, not in JSON
output.

The logger cannot enforce this on its own (it receives whatever it is given) so redaction
happens at the source. Scanner matches are redacted before they become a log field. HTTP headers
are masked before logging. Environment variable values matching credential patterns are masked
before they leave the module that read them.

The rule for anyone adding a log line: if the value could be a credential, log its presence and
shape, never its content. `logger.Debug("authorization header present", "length", len(value))`,
never the value.

## Progress

Distinct from logging, and handled separately.

- Appears only for operations expected to exceed roughly one second.
- Only when stderr is a terminal. Redirected, it would be megabytes of escape sequences.
- Updates at most 10 times per second. Per-item redraws are a measurable cost.
- Cleared when the operation finishes, so it does not pollute the final output.
- Suppressed entirely by `--quiet` and by `--output json`.
- Written to stderr, like everything else that is not the result.

## Implementation

`internal/logging` is built on `log/slog`. Levels, filtering, attribute handling, and the JSON
handler all come from the standard library; what this package adds is the text handler, the
constructors, and level and format parsing.

The type passed around is `*slog.Logger` itself. There is no wrapper: a wrapper would add a layer
to maintain and would break the `With` and `WithGroup` methods that callers already know.

- The logger is constructed once during startup and passed down. There is no package-level logger
  and no global default; a global logger is global mutable state, which `rules.md` R7 forbids.
- Modules receive a logger through their `Request` or their dependency interfaces. A module that
  is given nothing logs nothing, which is exactly what a unit test wants.
- `logging.Nop()` is the default in tests, so test output stays clean unless a test explicitly
  asks to capture log records and assert on them.

The text handler implements `slog.Handler` in about seventy lines. `WithGroup` prefixes attribute
keys with the group name: enough to satisfy the interface, and honest about the fact that DevNest
does not use groups. If nested grouping is ever needed, that method is where it goes.

## Testing

Live as of Phase 1:

- `TestStdoutCarriesNoLogOutput` runs a command at default, `--quiet`, and `--verbose`, and parses
  stdout as JSON each time. This is the contract that matters most and the one most likely to break
  by accident. `tests/e2e_test.go` asserts the same thing against the real process.
- JSON log lines are asserted to be valid JSON, one object per line, carrying `time`, `level`,
  `msg`, and the attributes.
- Level filtering is tested at each level, as are colour, timestamps, value quoting, and the fact
  that `With` does not leak attributes back into the logger it was derived from.

Arriving with the modules that need them:

- Redaction: given a scanner finding, no log record contains the matched value.
