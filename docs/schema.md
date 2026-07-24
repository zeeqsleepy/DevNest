# Schema

Status: implemented, current as of Phase 10
Last revised: 2026-07-24

The shapes of things: how packages are structured internally, how data moves between them, what
configuration looks like, and what the input and output contracts are.

## Package schema

Every package under `internal/core/` has the same internal structure. Uniformity here means a
contributor who has read one module can navigate any of them.

```
internal/core/<name>/
    <name>.go        Request, Result, and the entry function
    deps.go          interfaces this module needs from the outside world
    <name>_test.go   tests, using fakes for those interfaces
    testdata/        module-specific fixtures (optional)
```

Additional files split by concern when a module grows (`rules.go`, `classify.go`,
`format.go`) never by arbitrary size. A file named `helpers.go` or `utils.go` is a sign that
something has no home yet, and both names are banned.

### The module surface

Expressed as a shape rather than as code, since Phase 0 writes no code:

- **`Request`**: a struct of plain fields, already validated by the CLI layer. Absolute paths,
  not relative. Resolved values, not flag strings. No pointers into CLI state, no interfaces.
- **`Result`**: a struct of plain fields with JSON tags. Fully serialisable. No interfaces, no
  channels, no `io.Writer`. Slices are ordered deterministically before return.
- **Entry function**: takes `(context.Context, Request)`, returns `(Result, error)`.
- **Dependency interfaces**: declared in `deps.go`, as narrow as the module actually needs.

Everything follows from `Result` being plain data: JSON output is free, tests compare structs,
and the renderer never needs to know which module produced what it is rendering.

## Result envelope

Every JSON output shares an envelope. Consumers can therefore write one piece of handling code
that works for every command.

```
{
  "devnest":  { "version": "...", "command": "scan", "startedAt": "...", "durationMs": 0 },
  "status":   "ok" | "warning" | "error",
  "data":     { ... },
  "warnings": [ { "code": "...", "message": "...", "path": "..." } ],
  "error":    { "code": "...", "message": "...", "hint": "..." }
}
```

- `data` holds the module's `Result` and is the only part whose shape varies by command.
- `warnings` is always present, possibly empty. Non-fatal problems (an unreadable file during a
  walk, a toolchain probe that timed out) go here rather than being printed and lost.
- `error` is present only when `status` is `error`, in which case `data` is null.
- `status` is `warning` when the command succeeded but produced warnings.

Field naming is `camelCase` throughout, and durations are integers in milliseconds with the unit
in the field name. Sizes are integers in bytes, likewise named. No unit-less numbers, no
human-readable strings in machine output: `"1.4 GB"` is a rendering concern.

Field names within a major version are additive only. Removing or renaming a field is a breaking
change under `rules.md` R50.

## Data flow

### One invocation

```
argv
  │
  ▼
[cmd/devnest]        root command built; signal handler installed; root context created
  │
  ▼
[internal/cli]       flags parsed
                     config resolved: defaults ← file ← environment ← flags
                     logger constructed from resolved verbosity
                     renderer selected from --output
                     input validated → core.Request
  │
  ▼
[internal/core/*]    executes against platform interfaces
                     emits log records for progress
                     accumulates non-fatal problems as warnings
                     returns (Result, error)
  │
  ├─ error ─────────►[internal/errors] classify → user message + hint + exit code
  │
  ▼
[internal/output]    wrap Result in envelope, render per --output
  │
  ▼
stdout: results only          stderr: logs, progress, diagnostics
```

The stdout/stderr split is a hard rule, not a convention. `devnest scan --output json | jq` must
work identically at every verbosity level, which is impossible if any log line can reach stdout.

### Configuration resolution

Four sources, lowest to highest precedence. Each layer overrides only the keys it actually sets;
an absent key is not the same as a zero value.

```
1. compiled defaults
2. config file            (path from --config, or the OS config directory)
3. environment variables  (DEVNEST_ prefix)
4. command-line flags
```

Resolution happens once, in `internal/cli`, before any module runs. Modules never read
configuration; whatever they need arrives in their `Request`. This is what makes a module
testable without a config file present.

### Error flow

```
platform: syscall fails
    → wrapped with the operation and the path, typed with a code
core: adds domain context, decides fatal vs. warning
    → fatal returns; non-fatal accumulates into Result.Warnings
cli: receives error
    → errors.Classify() → code, user message, hint, exit code
    → message and hint to stderr; full chain to stderr under --verbose
main: exits with the mapped code
```

Full detail in `error-handling.md`.

## Configuration schema

Format is TOML: unambiguous, comment-friendly, and free of YAML's indentation and type-coercion
surprises. The full annotated default lives in `configs/`.

```toml
[general]
output          = "table"      # table | json | csv | markdown
color           = "auto"       # auto | always | never
verbosity       = "info"       # error | warn | info | debug
confirm         = true         # prompt before destructive operations

[scan]
follow_symlinks = false
respect_ignore  = true
max_depth       = 0            # 0 = unlimited
exclude         = [".git", "node_modules"]

[clean]
patterns        = ["node_modules", "dist", "build", "target", "__pycache__"]
protect         = []           # paths never touched, whatever the patterns say
require_confirm = true

[secret]
entropy_threshold = 4.5
exclude_paths     = ["testdata/", "fixtures/", "*.lock"]
custom_rules      = []

[network]
timeout_ms      = 30000
follow_redirect = true
max_redirects   = 10
attempts        = 3
interval_ms     = 200

[export]
directory       = "reports"
timestamp_files = true
```

Rules for this file:

- Every key has a compiled default. A missing or empty config file is fully functional.
- Unknown keys are a warning, never a fatal error, a newer config read by an older binary should
  still work.
- Type mismatches are fatal, with the key path and both types named.
- Nothing secret is ever stored here. DevNest has no credentials to store, and if a feature ever
  seems to need one, that is a design discussion first.

Environment variables mirror the structure: `[scan] max_depth` becomes `DEVNEST_SCAN_MAX_DEPTH`,
and `[network] timeout_ms` becomes `DEVNEST_NETWORK_TIMEOUT_MS`.

## Input contracts

**Paths.** Accepted relative, resolved to absolute immediately, with symlinks resolved before any
security decision is made. A path that escapes the operation root after resolution is rejected.
Windows separators, forward slashes, UNC paths, and long paths all work.

**stdin.** Commands that accept piped input (`hash`, `encode`, `decode`, `json`) read stdin when
no positional argument is given and stdin is not a terminal. When stdin *is* a terminal and no
argument was supplied, the command prints usage rather than hanging on a read that will never
return.

**Sizes and durations.** Accepted with unit suffixes (`10MB`, `30s`, `7d`) and normalised
internally to bytes and milliseconds.

**Validation happens in the CLI layer.** By the time a `Request` exists, its contents are known
good. Modules do not re-validate, and modules do not receive raw user strings.

## Output contracts

**Table**: the default when stdout is a terminal. Aligned columns, a header row, no box drawing.
Values are formatted for reading: `1.4 GB` rather than `1503238553`. Colour carries meaning only,
and is dropped when stdout is redirected or `NO_COLOR` is set.

**JSON**: the envelope above, indented by default, single-line with `--compact`. This is the
contract that scripts depend on and the one that changes most conservatively.

**CSV**: a flattened tabular projection for commands whose data is naturally rectangular. A
header row always. Commands whose results are deeply nested reject `--output csv` with a message
saying so rather than emitting something lossy.

**Markdown**: tables plus headings, for pasting into a ticket or a pull request. Report-shaped
rather than data-shaped.

Full detail in `export-system.md`.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | general failure |
| 2 | usage error, bad flag, missing argument |
| 3 | not found, path, port, or resource does not exist |
| 4 | permission denied |
| 5 | cancelled by user or by signal |

Commands with a pass/fail character (`hash verify`, `secret scan`, a future `env check`) exit
non-zero on a negative finding so they work as CI gates without output parsing. That behaviour is
documented per command in `commands.md`.
