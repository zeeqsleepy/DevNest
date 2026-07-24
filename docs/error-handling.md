# Error Handling

Status: implemented in Phase 1
Last revised: 2026-07-23

Errors are part of the user interface. Most of the time someone types a command it works and they
move on; the times they remember are the times it failed. A good failure message costs one
sentence and saves a support round trip.

## The model

Three concepts, kept separate on purpose:

1. **The cause**: the underlying failure. A syscall, a parse error, a timeout. Technical, precise,
   useless to most users on its own.
2. **The code**: a stable classification. Drives the exit code and lets callers branch without
   matching on message text.
3. **The presentation**: what the user reads. What was attempted, what went wrong, what to do
   next.

Every error carries all three by the time it reaches the top. Layers add to it as it travels up;
no layer discards what a lower layer attached.

## Error codes

Defined in `internal/errors`, stable within a major version, part of the JSON output contract.

| Code | Meaning | Exit |
|---|---|---|
| `OK` | Success | 0 |
| `INTERNAL` | A bug in DevNest | 1 |
| `INVALID_INPUT` | Bad flag, bad argument, malformed value | 2 |
| `NOT_FOUND` | Path, port, or resource does not exist | 3 |
| `PERMISSION_DENIED` | The operating system refused | 4 |
| `CANCELLED` | Interrupted by the user or a signal | 5 |
| `IO_ERROR` | Read or write failed for a reason other than permission | 1 |
| `PARSE_ERROR` | Input was structurally invalid | 1 |
| `NETWORK_ERROR` | Connection, DNS, or TLS failure | 1 |
| `TIMEOUT` | An operation exceeded its deadline | 1 |
| `UNSUPPORTED` | Not available on this platform or in this format | 1 |
| `CONFLICT` | The requested state is inconsistent with reality | 1 |
| `CHECK_FAILED` | A verification command found a negative result | 1 |

`CHECK_FAILED` is not a malfunction. `hash verify` on a mismatch, `secret scan` with findings, a
future `env check` on drift: the command worked correctly and the answer was no. Distinguishing
it from `INTERNAL` matters when reading CI logs.

`INTERNAL` is also what an *unclassified* error becomes. `errors.Classify` recognises typed errors
and a handful of standard library sentinels (`context.Canceled`, `context.DeadlineExceeded`,
`fs.ErrNotExist`, `fs.ErrPermission`); anything else falls through to `INTERNAL`. That is
deliberate: an error reaching the user unclassified means a layer forgot to type it, and the code
should say so rather than guess.

## In code

```go
// create a classified error
errors.New(errors.CodeInvalidInput, "unsupported output format %q", name).
    WithHint("expected one of: table, json")

// wrap a cause, adding what this layer knows
errors.Wrap(err, errors.CodeIO, "cannot read configuration file %s", path)
```

`errors.Wrap` keeps the cause reachable through `Unwrap`, so `errors.Is` and `errors.As` work all
the way up. The package re-exports `Is`, `As`, and `Join`, so inspecting a chain needs one import
rather than two.

`errors.CodeOf` gives the classification, `errors.ExitCode` the process exit code, and
`errors.Classify` the full presentation: message, hint, and the diagnostic detail shown only under
`--verbose`.

## Wrapping

Each layer adds only what it knows and the layer below could not.

```
platform:  "read directory C:\projects\api\node_modules: access is denied"
           ↑ operation and path, the platform layer knows these

core:      "scan tree: read directory C:\projects\api\node_modules: access is denied"
           ↑ what the operation was part of

cli:       Error: cannot scan C:\projects\api
             Access was denied reading node_modules.
             Run the shell with elevated permissions, or pass --skip-unreadable.
           ↑ what the user asked for and what they can do
```

Rules:

- Always `%w`, so `errors.Is` and `errors.As` continue to work at the top.
- Each wrap adds new information. `fmt.Errorf("error: %w", err)` adds nothing and is banned.
- Wrap text is lowercase without trailing punctuation, because it gets embedded in other messages.
- The full chain is never shown to the user by default. It goes to stderr under `--verbose`, where
  someone debugging can find it.

## User-facing messages

The shape:

```
Error: <what failed, in the user's terms>
  <why, in one sentence>
  <what to do next>
```

Written well:

```
Error: port 5173 is already in use
  Process "node.exe" (PID 18432) is listening on it.
  Run "devnest port free 5173" to stop that process.
```

```
Error: cannot read configuration
  C:\Users\haziq\AppData\Roaming\devnest\config.toml, line 14: expected a
  string for "general.output", found an integer.
  Run "devnest config validate" to see all problems, or delete the file to
  start from defaults.
```

Written badly:

```
Error: operation failed
Error: invalid input
Error: scan: walk: readdirent: The system cannot find the path specified.
panic: runtime error: index out of range [3] with length 2
```

The first two say nothing. The third leaks internal function names and a raw syscall message. The
fourth is a defect.

Guidelines:

- **Name the specific thing.** The path, the port, the flag, the key. "invalid configuration" is
  not a message; "line 14: expected a string for general.output" is.
- **Suggest the next action** whenever one exists. A flag to pass, a command to run, a permission
  to change.
- **Never print a stack trace by default.** It tells a user nothing and looks like a crash even
  when the situation is ordinary.
- **Never blame the user.** "Access was denied" describes the situation; "you don't have
  permission" assigns fault, and is often wrong: the process may be running under a service
  account nobody chose.

## Warnings

Not every problem should stop the work. A scan across 40,000 files that aborts on one unreadable
file is useless.

Non-fatal problems accumulate in `Result.Warnings` and are reported alongside the result:

```
Scanned 38,412 files in C:\projects\api (2.3 GB)

3 warnings:
  - permission denied: node_modules\.cache\deep\locked
  - symlink loop skipped: vendor\self
  - unreadable file: build\temp.lock
```

- Warnings are always present in JSON output as an array, possibly empty.
- `status` in the envelope becomes `"warning"` when the array is non-empty.
- Exit code stays 0 unless `--strict` is passed, which promotes warnings to a failure, useful in
  CI where an unexpectedly unreadable file may indicate a real problem.
- Warnings are never silently dropped. `--quiet` suppresses their display, not their presence in
  the structured output.

## Panics

Panics indicate a bug, never a bad input and never a missing file. A nil map write, an index out
of range, a broken invariant.

`main` installs a recovery that logs the panic as `INTERNAL`, prints a short message with the
issue URL, and exits 1:

```
Error: internal error in DevNest
  This is a bug. Please report it:
  https://github.com/<owner>/devnest/issues
  Run with --verbose to include the full trace in your report.
```

The recovery exists so the user gets something filable, not so panics become acceptable. Any panic
reaching a user is a defect to be fixed, and its fix ships with a test that panics without it.

## Cancellation

Ctrl+C is a normal outcome, not an error:

- Root context is cancelled; modules observe it at loop boundaries and return.
- `context.Canceled` maps to `CANCELLED`, message "cancelled", exit 5. No stack trace, no red
  "Error:" banner for something the user chose to do.
- Partial results and warnings collected so far still render.
- A second signal exits immediately with code 5.
- Destructive operations observe cancellation *between* targets, never inside one. A half-removed
  directory is worse than one extra directory removed.

## Errors in JSON output

Structured output always produces a well-formed document, even on failure. A consumer parsing
stdout should never have to handle the case where stdout contains something that is not JSON:

```json
{
  "devnest":  { "version": "1.0.0", "command": "scan", "startedAt": "...", "durationMs": 34 },
  "status":   "error",
  "data":     null,
  "warnings": [],
  "error": {
    "code":    "PERMISSION_DENIED",
    "message": "cannot scan C:\\projects\\api",
    "detail":  "access was denied reading node_modules",
    "hint":    "run with elevated permissions, or pass --skip-unreadable"
  }
}
```

`detail` carries the full wrap chain only under `--verbose`. It is diagnostic, and its exact
content is explicitly not part of the compatibility contract: `code` is what consumers branch on.

Reporting happens in `cli.ReportError`, after `Execute` returns. A failure can occur before
configuration is resolved (a malformed flag, an unreadable configuration file) and the error
still has to be reported in the shape the user asked for, so the output format and verbosity are
read straight from argv at that point.

## Testing errors

- Every error path gets a test. Error paths are where the untested code hides, and they run
  precisely when the user is already having a bad time.
- Tests assert on the **code**, never the message text. Message wording improves over time; codes
  do not.
- Every exit code in the table has a test that produces it, in `internal/errors` for the mapping
  and in `tests/e2e_test.go` for the real process.
- Fakes injected through module dependency interfaces make error paths easy to trigger, a fake
  filesystem returning permission errors on demand needs no elevated test runner and no
  platform-specific setup.
