# Internal Interfaces

Status: draft; no public API surface exists yet, see roadmap.md
Last revised: 2026-07-24

How the parts of DevNest talk to each other. This is an internal document: nothing described here
is a public API, and everything under `internal/` can change without notice. The public commitment
is the CLI surface and the JSON output, described in `cli-reference.md` and `schema.md`.

## The one interface that matters

Every module presents the same shape. Learn it once and every module is navigable.

```
Request  →  Run(ctx, Request)  →  (Result, error)
```

- **`Request`**: plain data, already validated by the CLI layer. Absolute paths, resolved values.
  No interfaces, no pointers into CLI state, no `io.Writer`.
- **`Result`**: plain data with JSON tags. Fully serialisable. Slices deterministically ordered
  before return. Includes a `Warnings` field for non-fatal problems.
- **`error`**: typed, carrying an `errors.Code`.

Everything else follows: JSON output is free, tests compare structs, and the renderer never needs
to know which module produced what it is rendering.

## Consumer-declared dependencies

A module declares the interfaces it needs in its own `deps.go`, as narrowly as it actually needs
them. It does not import a large shared `FileSystem` interface, and it does not import a concrete
platform type.

Conceptually, a module that walks a tree and reads files declares something like: a walk function
taking a root and a per-entry callback, a file reader, and a stat. Three methods, not thirty.

Why this way:

- **Narrow interfaces are trivial to fake.** A test implements three methods, not an entire
  filesystem abstraction.
- **The dependency is visible.** Reading `deps.go` tells you exactly what a module touches in the
  outside world, which is the first thing a security reviewer wants to know.
- **The platform layer stays free.** Because interfaces are declared by consumers, `platform` can
  add, change, or split methods without any module declaring a dependency on the parts it does not
  use.

The concrete types in `internal/platform` satisfy these interfaces by having the right methods. No
`implements` declaration exists in Go, and none is wanted: the platform layer does not know which
modules consume it, which is the correct direction of ignorance.

## Layer contracts

### `cli` → `core`

The CLI layer:

- Validates all user input and constructs a `Request` whose contents are known good.
- Passes a context, a logger, and platform implementations.
- Calls exactly one module entry function per command.
- Never inspects module internals or calls unexported behaviour through a back door.

The domain layer:

- Never reads flags, environment variables, or configuration.
- Never prints. Never exits.
- Returns everything it has to say in `Result` and `error`.

If a command handler needs to branch on a business condition, that branching belongs in the
module. If a module needs a flag value, the value belongs in its `Request`. Both violations are
common and both are review blockers.

### `core` → `platform`

The domain layer:

- Accesses the outside world only through its declared interfaces.
- Contains no `runtime.GOOS` comparison, ever.
- Assumes nothing about path separators; it uses the path helpers the platform layer provides.

The platform layer:

- Presents an identical exported surface on every operating system.
- Absorbs platform differences in build-tag files.
- Knows nothing about what its callers are doing. It has no concept of a "scan" or a "clean".
- Returns typed errors carrying the operation and the subject.

### Anything → cross-cutting packages

`errors`, `logging`, `output`, `config`, and `version` are leaves. Any layer imports them; they
import no layer. They hold no state that a caller could mutate, and none of them has a global
default instance.

## Renderer contract

`internal/output` accepts any module result and produces a rendering. It does not know which
module produced the value, and it must not.

- **JSON**: wraps the result in the envelope from `schema.md` and encodes it. Works for every
  result by construction, since results are plain serialisable data.
- **Table**: needs column definitions, which are supplied by the CLI layer alongside the result.
  Not by the module: how data looks is an interface concern, and a module that carried column
  widths would have output formatting embedded in the domain.
- **CSV**: a flattened rectangular projection. A result that cannot be flattened without loss
  rejects `--output csv` with a message saying so, rather than emitting something misleading.
- **Markdown**: headings and tables, report-shaped rather than data-shaped.

The renderer writes to an injected `io.Writer`, never to `os.Stdout` directly, so tests capture
output with a buffer and never touch a real stream.

## Error contract

`internal/errors` defines the codes, the wrapping helpers, and the classification that turns any
error into a user message plus an exit code.

- Every layer wraps with `%w`, adding what the layer below could not know.
- Callers branch on `errors.Code`, never on message text.
- Classification happens once, in the CLI layer, at the top of the stack.
- `main` maps the code to an exit code.

Detail in `error-handling.md`.

## Logger contract

- Constructed once during startup, passed down explicitly. No package-level logger, no global
  default, which would be global mutable state and is forbidden by `rules.md` R7.
- Modules receive it through their `Request` or their dependency interfaces.
- The no-op logger is the default in tests, so a module given nothing logs nothing.
- Structured key-value fields only; message text is a fixed string.

Detail in `logging.md`.

## Adding a module: the contract checklist

1. `Request` and `Result` are plain serialisable structs. Design the JSON output *before* writing
   any logic: the output shape is the contract, and deciding it last produces awkward types.
2. `Run(ctx, Request) (Result, error)` is the entry point, and the only exported function unless
   there is a clear reason for more.
3. Dependency interfaces are declared in `deps.go`, narrow, and satisfied by fakes in tests.
4. No printing, no exiting, no config reading, no flag reading.
5. No import of another `core/*` package. If one seems necessary, the shared thing moves down a
   layer first.
6. Errors are typed with a code and wrapped with context.
7. Non-fatal problems go into `Result.Warnings` rather than being logged and lost.
8. Slices are sorted before return, so two runs over unchanged input produce identical output.

## Deliberate non-features

**No plugin interface.** Third-party code loaded into this process is a security surface and a
compatibility obligation. Extension happens by contributing a module upstream, or by consuming the
JSON output from your own tool. See `prd.md`.

**No event bus, no dependency injection container, no service locator.** Dependencies are passed
as parameters. Indirection that hides which code calls which is not worth what it costs when
someone is reading the codebase for the first time.

**No shared mutable application state.** There is no `App` struct holding everything. Each command
gets what it needs and nothing more, which is also what makes each command independently testable.
