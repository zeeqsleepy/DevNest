# Architecture

Status: implemented through 1.0.0
Last revised: 2026-07-24

This document describes how DevNest is put together, which layer is allowed to know about which
other layer, and why the boundaries sit where they do. It is the reference for any structural
argument during code review.

## Guiding constraint

Every design choice below serves one goal: **a module must be understandable and testable on its
own, without starting the CLI.** If adding a feature requires touching four layers, the layering
is wrong and we fix the layering rather than working around it.

## Layers

Four layers, top to bottom. Control flows down. Dependencies point down and never up.

```
┌────────────────────────────────────────────────────────────┐
│ 1. Entrypoint            cmd/devnest                       │
│    Process concerns only: argv, signals, exit code.        │
├────────────────────────────────────────────────────────────┤
│ 2. Interface             internal/cli                      │
│    Command tree, flag parsing, help text, wiring.          │
│    Translates flags into a request; renders a result.      │
├────────────────────────────────────────────────────────────┤
│ 3. Domain                internal/core/<module>            │
│    The actual work. Pure Go, no flags, no printing.        │
│    Takes a typed request, returns a typed result or error. │
├────────────────────────────────────────────────────────────┤
│ 4. Platform              internal/platform/*               │
│    Filesystem, process, network, OS specifics.             │
│    Every syscall-adjacent thing lives behind an interface. │
└────────────────────────────────────────────────────────────┘

Cross-cutting, imported by any layer, importing none of them:
  internal/errors    internal/logging    internal/output    internal/config
```

### Layer 1: Entrypoint (`cmd/devnest`)

The smallest layer. It builds the root command, installs a signal handler that cancels the root
context, runs the command, maps the returned error to an exit code, and returns. It contains no
business logic and no flag definitions of its own. If this file grows past roughly a hundred
lines, something has leaked downward that belongs in `internal/cli`.

Keeping it this thin means the entire application is reachable from a test that never spawns a
process: a test constructs the same root command and runs it against in-memory streams.

### Layer 2: Interface (`internal/cli`)

Owns everything the user types and everything the user reads. Concretely:

- The command tree and each command's flags, arguments, and help text.
- Validation of user input into a well-formed domain request. A malformed flag never reaches the
  domain layer; it is rejected here with a message that names the flag.
- Choosing a renderer based on `--output` and handing it the domain result.
- Nothing else. In particular, no filesystem walking, no HTTP, no parsing of data files.

A command handler in this layer reads roughly as: build request from flags, call one domain
function, pass the result to the renderer, return. When a handler starts branching on business
conditions, that branching belongs in the domain.

This layer is the *only* place that knows how commands are parsed. Should that ever change, the
change is confined here.

**Implementation note.** There is no CLI framework dependency. The command tree is a plain struct
(`cli.Command`) over the standard library's `flag` package, in `internal/cli/command.go`. The
reasoning: what DevNest needs from a framework is a tree, flag registration, and help text, which
is roughly two hundred lines; what a framework brings with it is a permanent dependency, its own
help formatting to fight, and a release cadence to track. The help output in `design.md` is quite
specific about shape, and owning the renderer is easier than configuring one.

One thing the hand-rolled parser does that `flag` alone does not: argv is split into a command
path, flag arguments, and positional arguments *before* parsing, so flag position does not matter.
`devnest version --output json` and `devnest --output json version` behave identically. Go's `flag`
package stops at the first non-flag argument, which would otherwise make those two differ.

### Layer 3: Domain (`internal/core/<module>`)

One package per module: `core/env`, `core/scan`, `core/clean`, `core/port`, `core/hash`,
`core/encoding`, `core/data`, `core/httpprobe`, `core/git`, `core/secret`.

Every module exposes the same shape:

- A `Request` struct: plain data, already validated, no pointers to CLI state.
- A `Result` struct: plain data with JSON tags, fully serialisable, no interfaces, no channels,
  no `io.Writer` fields.
- One or a few exported functions taking `(context.Context, Request)` and returning
  `(Result, error)`.
- Its dependencies on the outside world declared as narrow interfaces in the module's own package,
  satisfied by `internal/platform` in production and by fakes in tests.

**Implementation note.** `internal/core/file` is the first module. It holds six operations
(organise, duplicates, rename, filter, size, hash), each with its own request and result type and
its own file. They share a package rather than being six modules because they share a vocabulary:
the same `Selection` options, the same `file.Info`, the same category table. Splitting them would
have meant either six copies of that vocabulary or a seventh package holding it, and neither buys
anything.

The module declares two dependency interfaces rather than one, and the split is load-bearing:

- **`Inspector`**: resolve, contains, protected-reason, stat, walk, digest. Read-only.
- **`Mover`**: `Inspector` plus exists, ensure-directory, move.

An operation that takes an `Inspector` cannot change the disk, and the signature says so without
anyone reading the body. Only `Organize` and `RenameFiles` take a `Mover`.

`internal/core/network` is the second module, added in Phase 3, holding six operations: monitor,
fetch, latency, ping, lookup, and inspect. It splits its dependencies further (`Requester`,
`Resolver`, `Prober`, and `Inspector`, one per kind of network operation) because unlike the file
module they genuinely need different things. Faking a DNS lookup means implementing one method.

`internal/core/security` is the third module, added in Phase 4: password generation, strength
checking, hashing, checksum verification, and Base64. It declares a single dependency, `Hasher`,
which is `platform/fs`'s digest surface and nothing else: the module cannot walk a tree, move a
file, or open a socket, and the interface is where that is enforced rather than merely intended.

Two things about it are worth recording here rather than only in `modules.md`:

- **It has no logger.** Every other module takes one. This one does not, because the simplest way
  to guarantee a password never reaches a log is for there to be nothing to log to.
- **Randomness arrives as a parameter**, an `io.Reader`, rather than being reached for inside the
  package. Production passes `crypto/rand.Reader`; tests pass a deterministic stream. There is no
  path to `math/rand` and no seed.

`internal/core/log` is the fourth module, added in Phase 5: seven read-only operations over one
text log file. It declares one dependency, `Reader`, with three methods (resolve, stat, open), and
it is the first module whose results are produced by streaming rather than by collecting.

That last point stretched a rule rather than breaking it. A module returns plain serialisable data,
which means it cannot hand back a lazy sequence; what it can do is read lazily and return the
summary. So the module streams internally, and everything it returns is a bounded aggregate: counts,
rankings capped at a limit, the ten longest lines. Nothing in a log result grows with the size of
the file, which is what keeps "results are plain data" affordable on a four gigabyte input.

`internal/core/env` and `internal/core/scan` are the fifth and sixth modules, added in Phase 6.
`env` reports what is installed on the machine: it declares `Runner`, `Locator`, and `Describer`,
one per kind of question, so an operation that only reads PATH is never handed something that can
start a process. `scan` reports what a project tree is made of, over a read-only `Inspector`.

`internal/core/encoding` and `internal/core/data` are the seventh and eighth modules, added in
Phase 7. `encoding` declares no dependencies at all: hex, percent-encoding, and JWT decoding all
work on a value the caller already has, so there is nothing for it to open. `data` declares the
same three-method `Reader` the log module uses, and is the one module that deliberately holds its
whole input in memory, because a document is a tree and formatting, querying, or converting one
needs its shape before it can act. The limit that follows from that is 64 MiB, enforced and
reported rather than discovered as an out-of-memory kill.

`data` is also where DevNest's only third-party dependency lives, a YAML parser. Everything above
and around it is still the standard library, and the module graph is pinned in CI so that a second
dependency has to be a decision.

`internal/core/port` and `internal/core/clean` are the ninth and tenth modules, added in Phase 8,
and they are the two that can change the machine rather than describe it. `port` splits its
dependencies by capability: `Enumerator` lists sockets, `Inspector` names processes, and
`Terminator` is the only one that can end one, so `List` and `Check` are read-only by signature.
`clean` does the same with `Inspector` and `Remover`. In both, the destructive function is the one
whose parameter type has a destructive method, and no other function can reach it.

Phase 8 is also where the platform layer stopped being portable by luck. Socket enumeration has
three implementations under build tags (`syscall` against the IP Helper API on Windows, `/proc` on
Linux, `lsof` on macOS), and process termination has three more. Two honest gaps are recorded
rather than papered over: macOS uses `lsof` because `libproc` needs cgo, and Windows has no way to
ask a process to exit, so `port free` there requires `--force`.

`internal/core/git` and `internal/core/secret` are the eleventh and twelfth modules, added in Phase
9. `git` declares a `Runner` and a `Locator` and runs the git executable, which is a decision
recorded in `modules.md` and tested rather than trusted: the fake records every invocation and the
build fails if any of them is a subcommand that can write. `secret` declares a `Reader` for the
working-tree scan and a `Runner` used only by the history scan, so a tree scan cannot start a
process.

`secret` is also where a redaction rule is enforced by the type rather than by discipline: a
`Finding` has no field holding the matched value, so no renderer, export, or verbosity setting can
print one. A test serialises a whole result to JSON and fails if a credential appears anywhere in
it.

The twelve modules share nothing, and none imports another. All twelve walk their own path to the
platform layer, which is the property the layering exists to protect.

Where two of them genuinely need the same thing, the shared code lives one layer down or in a
shared package, never sideways between them. Two cases exist so far:

- **Hashing**, needed by `file` and by `security`, lives in `platform/fs`. `DigestReader` was added
  there in Phase 4 so that hashing a string and hashing a file are the same code. Two
  implementations of "which algorithm is this" is how two commands end up disagreeing about what
  SHA-256 means.
- **Classification**, needed by `scan` and later by `clean` and `secret`, lives in
  `internal/classify`, a leaf package below the modules holding rules and nothing else: what kind a
  file is (source, test, generated, vendored, build output, asset, docs, config) and what language
  it is written in, along with the comment syntax the line counter needs. It walks nothing and
  reads nothing. It is deliberately *not* the same table as `core/file`'s: that one answers "photo
  or document" for sorting a downloads folder, this one answers "authored or generated" for
  reporting what a project is made of, and a `.png` is an image to one and an asset to the other.

Rules that keep this layer honest:

- It never writes to stdout or stderr. Progress is reported through the logger, results through
  the returned value.
- It never reads flags, environment variables, or config files directly. Anything it needs arrives
  in the `Request`.
- It never calls `os.Exit`.
- It never imports another `core/*` package. Modules that appear to need each other actually share
  something that belongs one layer down or in a shared helper; see "Module independence" below.

Because a module is a function from data to data, testing it means calling it with a struct and
comparing structs. No process spawning, no output scraping.

### Layer 4: Platform (`internal/platform`)

Everything the operating system does differently, isolated:

- `platform/fs`: walking, reading, writing, removal, size and permission queries.
- `platform/proc`: running an external program under a timeout, and locating one on PATH.
- `platform/net`: socket enumeration, HTTP transport construction, TLS inspection.
- `platform/sys`: OS and architecture identification, user home directory, shell and terminal
  detection, and the environment.

Platform-specific code uses Go build tags with one file per operating system
(`ports_windows.go`, `ports_linux.go`, `ports_darwin.go`) exposing an identical exported surface.
A build-tag file is the *only* acceptable place for an `if runtime.GOOS == ...` style decision.
Such a check appearing in `cli` or `core` is a review blocker.

**Implementation note.** `platform/proc` and `platform/sys` were added in Phase 6 for the `env`
module. `proc` is where the two operating systems differ most in this layer: what makes a file
runnable is an execute bit on Unix and a PATHEXT extension on Windows, and what name a shell would
type to run it is the file name on Unix and its stem on Windows. Both decisions live in
`platform_windows.go` and `platform_unix.go` and nowhere else, so the `env` module counts shadowed
executables without knowing that `go.exe` and `go.cmd` are two copies of `go`. Every invocation is
bounded by a timeout with a default, because a toolchain version flag that opens a network
connection or waits on a lock is common enough that an unbounded probe would make the summary
unusable.

**Implementation note.** `internal/platform/fs` was created in Phase 2, when the file module gave
it something to do. It provides one concrete type, `fs.System`, whose zero value is ready to use,
with methods for walking, stating, resolving, containment, digesting, and moving.

Three things live in its build-tag files (`platform_windows.go`, `platform_unix.go`) and nowhere
else:

- **`pathKey`**: path normalisation for comparison. Windows filesystems are case-insensitive but
  case-preserving, so two spellings of one path have to compare equal; elsewhere they must not.
  Every containment and collision check goes through it, and `fs.PathIdentity` exposes it for the
  module layer without exposing the reason.
- **`isHidden`**: a leading dot everywhere, plus the real hidden attribute on Windows.
- **`protectedReason`**: the directories where a bulk operation is refused. The list differs
  entirely by platform; the policy that consumes it does not.

`internal/platform/net` followed in Phase 3, and differs from `fs` in one way worth noting: it
carries settings. `fs.System` is a zero-value struct because a filesystem has no options; a
network client has a timeout, a redirect policy, and a verification setting, all of which are
decisions the caller makes and the platform layer merely honours. Those live as fields on
`net.System` rather than being threaded through every method signature.

`platform/proc` and `platform/sys` arrived in Phase 6 with `env`, the module that needed them. Both
carry no settings, like `fs` and unlike `net`: a process runner and a machine describer have
nothing for the caller to configure that is not already an argument to a call.

Terminal detection stayed in `internal/output`: colour is an output concern, and the check is a
character-device test with no platform-specific code. `platform/sys` describes the shell and the
terminal *program* for the environment summary, which is a different question with a different
answer and no bearing on whether colour is used.

### Cross-cutting packages

These are leaves. They import from the standard library and from each other only in the direction
noted. Anything may import them.

- **`internal/errors`**: the typed error model, error codes, wrapping helpers, and the mapping
  from error to process exit code. It also re-exports `Is`, `As`, and `Join` so a caller inspecting
  an error chain needs one import rather than two. Detailed in `error-handling.md`.
- **`internal/logging`**: the structured logger writing to stderr. Levels, filtering, and
  attribute handling come from `log/slog`; this package adds the human-readable handler and the
  constructors. Detailed in `logging.md`.
- **`internal/output`**: the result envelope, the renderers, and terminal and colour detection.
  Detailed in `export-system.md`. It gained a CSV renderer in Phase 5, and with it the one piece of
  optional structure in the layer: a command that has a row view of its result supplies one, and a
  renderer that needs rows asks for it through an interface the other renderers do not implement.
  A command with no rows is refused by `--output csv` with a message saying so, rather than being
  rendered as an invented shape somebody would then script against.
- **`internal/config`**: configuration loading and merging across defaults, file, environment,
  and flags, including the TOML decoder. Detailed in `configuration.md`.
- **`internal/classify`**: the file classification and language rules, added in Phase 6. A leaf like
  the others: it imports the standard library and nothing else, and it holds rules with no logic
  beyond applying them. It sits here rather than inside `scan` because `clean` and `secret` will
  need the same answers, and a module may not import another module.
- **`internal/version`**: build metadata (version, commit, build date) injected at link time.

Cross-cutting packages may import each other where the dependency is genuinely one-directional.
`config`, `logging`, and `output` all produce typed errors, so they import `internal/errors`, which
imports nothing. What none of them may do is import a layer. That is checked mechanically; see
below.

## Dependency direction

The only permitted import edges:

```
cmd/devnest      →  internal/cli
internal/cli     →  internal/core/*, internal/output, internal/config,
                    internal/errors, internal/logging, internal/version
internal/core/*  →  internal/platform/*, internal/errors, internal/logging
internal/platform→  internal/errors, internal/logging
pkg/*            →  standard library only
```

Everything else is forbidden. Notably:

- `core` must never import `cli`. If a module seems to need a flag value, the flag value belongs
  in its `Request`.
- `core` must never import `output`. A module produces data; deciding how data looks is not its
  concern.
- `platform` must never import `core`. The platform layer knows nothing about what its callers
  are trying to accomplish.
- No `core/a` importing `core/b`.

This is enforced mechanically, not by good intentions. `tests/boundaries_test.go` parses every
Go file in the repository and fails the build on a forbidden edge, on a module importing another
module, and on the entrypoint growing past the size where something has clearly leaked out of
`internal/cli`. It runs as part of `make test`, so a violation is caught locally before it reaches
review.

## Module independence

The temptation to have one module call another shows up quickly. `clean` wants to know which
directories are build artifacts, and `scan` already classifies files. `secret` wants a file
walker, and `scan` already has one.

The resolution is always the same: **push the shared thing down, never sideways.**

- A shared file walker belongs in `platform/fs`.
- A shared classification rule set belongs in its own small package under `internal/` that both
  modules import, with no logic beyond the rules themselves.
- If a genuine pipeline emerges where one module's output feeds another's input, the composition
  happens in `internal/cli`, which is allowed to know about both.

Sideways imports between modules are how a codebase quietly turns into a single knot. The rule is
absolute so that nobody has to argue it case by case.

## Request and result flow

A single invocation, end to end:

```
argv
  │
  ▼
cmd/devnest            build root command, install signal handler,
                       create root context
  │
  ▼
internal/cli           parse flags, load config (defaults ← file ← env ← flags),
                       initialise logger and renderer, validate input,
                       construct core.Request
  │
  ▼
internal/core/<module> execute against platform interfaces,
                       return core.Result or a typed error
  │
  ├── error ──────────► internal/errors → user message + exit code
  │
  ▼
internal/output        render Result as table / json / csv / markdown
  │
  ▼
stdout                 results only        stderr: logs and diagnostics only
```

The stdout/stderr split matters: `devnest scan --output json | jq` must work even at
`--verbose`, so no log line is ever permitted on stdout.

## `pkg/` versus `internal/`

`internal/` is the default. Go's compiler enforces that nothing outside the module can import it,
which means we can refactor freely without breaking anyone.

`pkg/` is a public commitment. A package moves there only when all of the following hold: it is
genuinely useful outside DevNest, its API has stabilised through real use, it depends on nothing
but the standard library, and a maintainer accepts responsibility for its compatibility. In Phase
0, `pkg/` is empty on purpose. It is far easier to promote a package later than to withdraw one.

## Concurrency

- Every long-running operation takes a `context.Context` as its first parameter and honours
  cancellation. Ctrl+C cancels the root context and unwinds cleanly rather than killing the
  process mid-write.
- Parallelism exists only where it is measurably worth it: currently the directory walk in
  `scan` and the hashing of multiple files. It is bounded by a worker pool sized from
  `runtime.NumCPU()` and configurable.
- Results are collected and ordered deterministically before rendering. Two runs over an unchanged
  tree produce byte-identical output; anything else makes diffing reports useless.
- Shared mutable state between goroutines is avoided by design. Workers own their data and send it
  over a channel. The race detector runs in CI on every push.

## Extensibility

Adding a module in this structure means:

1. A new package under `internal/core/`, with its `Request`, `Result`, dependency interfaces, and
   tests.
2. A new command file under `internal/cli/`, wiring flags to the request.
3. Registration of the command in the command tree.
4. Documentation: an entry in `commands.md`, and in `modules.md`.

No existing module is touched. That property is the entire point of the layering, and it is the
test to apply when someone proposes a structural change: does it preserve "new feature, new
files"?

## Future scalability

Directions the structure already accommodates, without committing to any of them:

- **A library-first consumer.** Because domain modules take and return plain data, exposing a
  stable subset through `pkg/` requires no rewrite, only a decision.
- **Alternative front ends.** A second front end (a local HTTP endpoint, an editor extension
  backend) would sit beside `internal/cli` as a peer, reusing the same domain layer untouched.
- **Additional platforms.** A new operating system means new build-tag files under
  `internal/platform`. No other layer notices.
- **Plugins.** Deliberately excluded from 1.x. Should it ever be reconsidered, the boundary is
  already drawn at the module interface, and the likely mechanism is a subprocess exchanging JSON
  over stdio, not code loaded into this process. See `prd.md`.

## Decisions recorded

| Decision | Reasoning | Cost accepted |
|---|---|---|
| Domain modules never import each other | Prevents a dependency knot; keeps modules independently testable | Occasional duplication, or an extra shared package |
| All I/O behind platform interfaces | Tests run without touching a real disk or network | More types to define up front |
| `pkg/` empty until proven | Public API is a promise; promises are expensive to keep | Contributors must justify promotion |
| Results are plain serialisable structs | JSON output is free and always consistent with the table | Cannot return lazy or streaming values from a module |
| Windows treated as primary, not as a port | Avoids the usual pattern where Windows support is bolted on and stays broken | Some designs are constrained by the weakest platform |
| No plugin system in 1.x | Security surface and compatibility burden are not worth it yet | Extension requires contributing upstream |
| No CLI framework; a command tree over stdlib `flag` | Full control over help output, which `design.md` specifies precisely, and one less dependency | Roughly 200 lines of parsing to own and test |
| Logging built on `log/slog` | Levels, attributes, filtering, and a JSON handler come from the standard library | The human-readable handler is ours, and JSON records use slog's key names |
| A TOML subset decoder rather than a dependency | The configuration schema is flat sections of scalars and string arrays; a parser for exactly that is small, and its error messages name the line | A file using nested tables or multi-line strings is rejected rather than understood |
| `internal/platform` deferred to Phase 2 | Phase 1 needs nothing from it that the standard library does not provide portably | The layer's boundaries are proven later than the rest |
| Six file operations in one module, not six modules | They share a vocabulary; splitting means duplicating it or inventing a seventh package to hold it | One package with several entry points instead of one each |
| Two dependency interfaces, split by whether they mutate | An operation's signature states whether it can change the disk | Two names to learn instead of one |
| `platform/fs` tests use a real temporary directory and are not tagged | A filesystem seam tested against a fake tests the fake | The one package in the tree whose tests touch a disk |
| `net.System` carries settings; `fs.System` does not | A network client has a timeout and a redirect policy; a filesystem has neither | The two platform packages are not shaped identically |
| `network ping` opens a TCP connection rather than sending ICMP | ICMP needs a raw socket and therefore elevation, which DevNest never requests | The command answers a slightly different question, and says so everywhere |
| An unreachable host is a result, not an error | The exit code can then mean "the site is down" rather than "DevNest broke" | Callers must read the result, not just the error |
| A shared DNS deadline returns partial answers | Four good answers should not be thrown away because a fifth query was slow | The result can be part real and part timed out |
| `core/security` has no logger at all | The surest way to guarantee a password is never logged is to have nowhere to log it | That module cannot report progress, which it never needs to |
| The strength checker's findings are fixed strings | A result is exported and pasted into tickets; a message assembled from the input is a leak | Findings describe the shape of a weakness, never quote it |
| Randomness is a parameter, not a package-level call | Makes the generator testable and makes reaching for `math/rand` impossible | One more argument on one function |
| A dictionary match caps the score rather than only subtracting | A known base is cracked in milliseconds however much entropy the character maths reports | Two knobs per finding instead of one |
| `core/log` streams internally and returns bounded aggregates | Keeps "results are plain data" affordable on a file too large to hold | A log result can never be the lines themselves, only a summary of them |
| One collection pass behind the three HTTP log commands | Two parsers is how two commands end up disagreeing about how many requests a file holds | Each of the three collects a little more than it reports |
| A line past the length cap is truncated, not abandoned | Memory must not follow the input, and a file must not be rejected over one odd line | The reported content of such a line is incomplete, and says so |
| CSV is opt-in per command through a row view | A result that is a handful of named values has no honest CSV form | Two render functions on the commands that have rows |
| The log module has no `--since` or `--follow` | Timestamp parsing across every log format is a project; following a file never terminates | Two things people will ask for are absent |
| Classification rules live in `internal/classify`, not in `scan` | `clean` and `secret` need the same answers, and modules may not import each other | A leaf package exists before its second consumer does |
| `classify` is a separate table from `core/file`'s | One answers "photo or document", the other "authored or generated"; merging serves neither | Two extension tables, each small and single-purpose |
| Toolchain probes run under a bounded timeout with a default | A version flag that opens a socket or waits on a lock is common; an unbounded probe makes the summary unusable | A tool slower than the timeout is reported as not answering |
| A tool not on PATH is never run | Skips most of the table on a typical machine without starting a process | The lookup happens before the probe, always |
| `env vars` masks by variable name, not by value shape | Guessing whether a value is a secret is wrong in the dangerous direction | A variable with an innocuous name holding a secret is shown |
| `env which` lists every copy, not the first | The winner is the one fact that does not help when the version is wrong | More output than the shell built-in gives |
| One dependency, `github.com/goccy/go-yaml`, for YAML | Hand-writing a YAML parser is not a reasonable use of anyone's time, and its errors already carry a line, a column, and an excerpt | A `go.sum`, a supply chain of one, and a CI job pinning the whole graph |
| `core/data` holds a whole document in memory, with a limit | A document is a tree: formatting, querying, and converting all need the shape before they can act | 64 MiB is refused with a sentence; anything larger belongs in a streaming tool |
| Reprinting works on bytes, querying re-encodes | A formatter that reorders keys produces a diff nobody can review | `query` output has sorted keys, and says so |
| The JSON query syntax is a path expression and stops there | A query language is a product of its own; a partial one is a worse `jq` that nobody documented | No filters or wildcards; `jq` is a pipe away |
| No `yaml format` command | Re-emitting YAML deletes every comment in the file | YAML can be validated and converted, never reprinted |
| A nested value is refused rather than stringified into a CSV cell | A spreadsheet that looks converted and is not gets found weeks later, in a report | `--flatten` is a decision the user makes, not a default |
| JWT decoding never verifies, and the result carries the field | Verification needs the key, an algorithm policy, and an audience decision; half of it teaches false trust | A user wanting verification needs a different tool |
| Socket enumeration is three implementations behind one surface | There is no portable answer; each kernel publishes it differently | Three files to maintain, and only one of them tested on any given machine |
| macOS shells out to `lsof` rather than calling `libproc` | `libproc` needs cgo, and static cross-compiled releases are worth more than one syscall | A subprocess, a timeout, and a dependency on a program that ships with the OS |
| `port free` requires `--force` on Windows | Windows has no cross-process way to ask politely; the only mechanism is a kill | One command behaves differently on one platform, and says so |
| Process ownership is left to the kernel | It is the authority; a second check would duplicate or contradict it | DevNest reports a permission error rather than predicting one |
| `clean` requires a marker file beside a generic directory name | "build" is output in a project and somebody's work anywhere else | A project with no recognised marker file is not cleaned |
| Every `clean` candidate is re-checked immediately before removal | The tree has had time to change since the scan | The guards run twice on every run |
| `Scan` takes an interface with no destructive method | Calling the wrong function cannot delete anything | Two interfaces where one would compile |
| `core/git` shells out rather than embedding a git library | Any machine with a repository has git; a git implementation is enormous | A subprocess per question, and git must be on PATH |
| Read-only is asserted by a test over recorded invocations | "It only runs read subcommands" is a claim that decays; a test does not | The fake has to record every call |
| Git fields are joined with 0x1F | A null byte cannot be passed in an argument vector; tabs and pipes appear in commit subjects | An unusual separator in every format string |
| A secret `Finding` has no field for the matched value | Redaction that a renderer applies is one `--output json` from being bypassed | The value cannot be recovered downstream, by design |
| Every secret rule carries an entropy floor | A placeholder has the shape of a key and none of the information | A genuinely low-entropy credential is missed |
| History scanning is a separate command, not a default | A pre-commit hook wants the tree in under a second; an audit can wait minutes | Somebody has to know to run it |
