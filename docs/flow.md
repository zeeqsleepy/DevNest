# Application Flow

Status: draft, Phase 0
Last revised: 2026-07-23

What happens between the user pressing Enter and the process exiting. Ordering matters here: most
of the awkward bugs in a CLI come from doing something in the wrong order during startup.

## Startup

```
process start
  │
  ├─ 1. build version metadata          link-time values, no I/O
  ├─ 2. construct root context          with cancellation
  ├─ 3. install signal handler          SIGINT, SIGTERM, Windows CTRL_C
  ├─ 4. build the command tree          declarations only, no execution
  ├─ 5. parse argv                      framework resolves command + flags
  │      └─ parse failure ─────────────► usage message, exit 2
  ├─ 6. resolve configuration           defaults ← file ← environment ← flags
  │      └─ malformed config ──────────► error with key path, exit 1
  ├─ 7. construct logger                level from resolved verbosity
  ├─ 8. detect terminal, select renderer
  └─ 9. run the command handler
```

Ordering constraints that are easy to get wrong:

- **Signals before work (step 3).** A handler installed after the first long operation begins
  leaves an early Ctrl+C to the default disposition, which kills the process mid-write.
- **Config before logger (6 before 7).** Verbosity comes from configuration. A logger built
  earlier logs at the wrong level, and the messages that matter during config loading are exactly
  the ones lost.
- **Nothing expensive before step 9.** No filesystem walking, no rule set parsing, no network. The
  50 ms startup budget in `performance.md` is spent almost entirely on process and runtime
  initialisation, and it stays that way only if steps 1–8 stay cheap. Rule sets and detection
  tables load lazily, inside the command that needs them.
- **Renderer selection before the handler runs (8).** The handler must never branch on output
  format; it hands its result to whatever renderer it was given.

## Command execution

```
handler entry
  │
  ├─ 1. validate input             positional arguments and flag combinations
  │      └─ invalid ──────────────► usage error naming the flag, exit 2
  ├─ 2. resolve paths              relative → absolute, symlinks resolved
  │      └─ outside root ─────────► permission error, exit 4
  ├─ 3. build core.Request         plain data, fully validated
  ├─ 4. confirm if destructive     unless --yes, --apply, or non-interactive
  │      └─ declined ─────────────► exit 5
  ├─ 5. call the module            core.Run(ctx, request)
  │      │
  │      ├─ acquires resources through platform interfaces
  │      ├─ honours ctx cancellation at every loop boundary
  │      ├─ logs progress to stderr
  │      ├─ accumulates non-fatal problems into Result.Warnings
  │      └─ returns (Result, error)
  │
  ├─ 6. on error ─────────────────► classify, render, exit with mapped code
  ├─ 7. render Result              envelope + selected renderer → stdout
  ├─ 8. export if requested        --export writes the same envelope to a file
  └─ 9. return exit code           0, or a command-specific code for a failed check
```

Step 4 exists at this layer, not inside the module, because a module must be callable without a
terminal. Confirmation is an interface concern.

Step 9's "command-specific code" covers commands with a pass/fail character: `hash verify` exits
non-zero on mismatch, `secret scan` exits non-zero when findings exist. That is what makes them
usable as CI gates without parsing output.

### Destructive command flow

Commands that delete or modify get an extra sequence, because this is where an ordering mistake
costs someone their work.

```
  ├─ enumerate targets            read-only pass, nothing modified
  ├─ apply protection rules       config `protect` list, root and home guards
  ├─ resolve every target fully   symlinks and .. resolved before any check
  ├─ verify containment           every target must be under the operation root
  ├─ report the plan              full paths, counts, total size
  ├─ if not --apply ─────────────► print plan, exit 0        ← the default
  ├─ if interactive and confirmation required ─► prompt
  │      └─ declined ────────────► exit 5
  └─ execute one target at a time
         ├─ log the full path before each removal
         ├─ a failure on one target does not abort the rest
         └─ collect failures into warnings, report them all at the end
```

Enumerate-then-act, never enumerate-while-acting: mutating a tree during a walk over that same
tree produces behaviour nobody can reason about, least of all during a bug report.

## Error flow

```
[platform] syscall fails
    └─ wrap with operation + path, attach an error code
           │
[core]     receives it
    ├─ fatal?    return the wrapped error
    └─ tolerable? append to Result.Warnings, continue
           │
[cli]      receives a returned error
    ├─ errors.Classify(err) → code, message, hint, exit code
    ├─ stderr: "Error: <message>" plus the hint, if any
    ├─ stderr, --verbose only: full wrap chain and, if present, stack context
    └─ stdout: for --output json, the envelope with status "error"
           │
[main]     exit with the mapped code
```

Notes:

- Cancellation is not a failure. A cancelled context produces exit code 5 and the message
  "cancelled", not a stack trace.
- A panic anywhere is recovered in `main`, logged as an internal error with the issue URL, and
  exits 1. A panic reaching the user is a defect under `rules.md` R32: the recovery exists so the
  user gets a filable report, not so panics become acceptable.
- Partial results survive. If a scan fails halfway, the warnings collected up to that point are
  still rendered.

Full model in `error-handling.md`.

## Logging flow

```
core / platform
    └─ logger.Debug/Info/Warn("message", key, value, ...)
           │
    level filter (from resolved verbosity)
           │
    handler: text (human) or JSON (--output json / --log-format json)
           │
    stderr, always
```

Two rules that everything else follows from:

- **Logs never reach stdout.** No exceptions, no verbosity level, no format. stdout carries the
  result and nothing else.
- **`--output json` switches the log handler to JSON too**, so a pipeline that captures both
  streams gets machine-readable data on both.

Levels and field conventions are in `logging.md`.

## Signal flow

```
SIGINT / SIGTERM / CTRL_C_EVENT
    └─ cancel root context
           │
    modules observe cancellation at loop boundaries and return
           │
    in-flight resources are released via defer
           │
    partial results and warnings render
           │
    exit 5
```

A second signal during shutdown exits immediately with code 5. Someone pressing Ctrl+C twice
means it, and the first press has already had its chance to unwind.

Destructive operations do not abandon a single in-flight removal partway. The cancellation is
observed between targets, not inside one: an interrupted directory removal is worse than one
extra directory removed.

## Export flow

`--export <path>` is available on every command that produces a result.

```
Result
  └─ envelope applied (same envelope as stdout)
       └─ format from the file extension, or --export-format
            └─ write to a temporary file in the destination directory
                 └─ fsync, then atomic rename over the target
```

The temporary-file-then-rename sequence means an interrupted export leaves either the previous
file or the new one, never a truncated file that looks valid. The temporary file lives in the
destination directory so the rename stays on one filesystem and remains atomic.

Exporting does not suppress stdout. The result still renders normally, because a user who exports
usually also wants to see what happened.

## Configuration flow

```
startup
  ├─ compiled defaults
  ├─ config file
  │    ├─ --config <path>          → must exist, missing is a fatal error
  │    └─ otherwise, OS config dir → missing is fine, defaults apply
  ├─ environment variables, DEVNEST_ prefix
  └─ command-line flags
         │
    merged, then validated as a whole
         │
    ├─ unknown key   → warning, continue
    ├─ type mismatch → fatal, naming the key and both types
    └─ valid         → resolved configuration, read-only from here on
```

An explicitly requested config file that does not exist is fatal, because the user asked for a
specific file and silently ignoring it would produce results they did not ask for. A missing
default config file is not fatal, because most users never create one.

After this point the resolved configuration is immutable. Nothing reconstructs or re-reads it
mid-run.
