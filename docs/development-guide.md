# Development Guide

Status: current as of Phase 10
Last revised: 2026-07-24

How to set up a machine for working on DevNest and how the day-to-day loop goes.

`contributing-guide.md` covers getting a change accepted. This document covers doing the work.

## Prerequisites

- **Go 1.25 or newer.** `go version` to check. The floor moves only on a minor release.
- **Git.**
- **Make.** Present by default on Linux and macOS. On Windows it comes with Git Bash, or via
  `winget install GnuWin32.Make`. Every Make target has an equivalent script in `scripts/` for
  anyone who would rather not install it.
- **A terminal that handles ANSI colour.** Windows Terminal, PowerShell 7, or any modern Linux or
  macOS terminal. The legacy `cmd.exe` console works but colour detection falls back to disabled.

Optional but useful: `golangci-lint` for running the linter locally rather than waiting for CI
(its configuration is `tools/golangci.yml`), and `delve` for debugging.

Install the same version CI pins, or the results will not match:

```
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
```

The configuration uses schema version 2. The binary also refuses to run when it was built with an
older Go than the `go` directive in `go.mod`, which is what pinning a recent version avoids.

**A C compiler, for the race detector.** Go's `-race` requires cgo. On Linux and macOS that is
already satisfied; on Windows it needs gcc, usually from mingw-w64 or the toolchain that ships with
Git for Windows. Without it, `go test -race` fails with `-race requires cgo`. This is not a blocker:
run `go test -count=1 ./...` locally and let CI run the race detector on all three platforms.
It is still worth installing if you touch anything concurrent.

## Setup

```
git clone https://github.com/<owner>/devnest.git
cd devnest
make setup
```

`make setup` downloads dependencies, verifies the toolchain version, and installs the development
tools listed in `tools/`. Those tools live in a separate module so their dependencies never enter
the main module graph: nothing a contributor installs ends up in a user's build.

Confirm it worked:

```
make build
make test
```

## Daily loop

```
make build       compile to dist/
make run         build and run, ARGS="version --output json" to pass arguments
make test        the fast suite: everything not behind a build tag
make test-e2e    end-to-end tests against the built binary
make test-all    unit, integration, and end-to-end
make lint        gofmt check, go vet, golangci-lint
make check       lint + test, what CI runs
make cover       coverage report to reports/coverage.html
make bench       benchmarks against committed baselines
make dist-all    cross-compile every release target
make clean       remove build output
```

`make test` covers `./...`, which includes the cross-cutting checks in `tests/`: the import
boundaries, the entrypoint size, and the shipped example configuration. Integration and end-to-end
tests sit behind build tags, so they stay out of the fast loop.

`make check` is what CI runs. Running it before pushing means CI rarely tells you something you
did not already know.

The tight loop is `make test`: the unit suite is under 10 seconds, and it is meant to be run
constantly. If it stops being fast, that is a defect in the tests, not a reason to run them less.

## Working on a change

**1. Find where it goes.** `architecture.md` for the layering, `modules.md` for module
boundaries, `folder-structure.md` for directories. If it is genuinely unclear where something
belongs, that is worth raising in the issue before writing code: an ambiguous placement is
usually a sign that the design needs a decision, not that the documentation is incomplete.

**2. Design the output first.** For anything producing a result, write the JSON you want to see
before writing any logic. The output shape is the contract, and deciding it last produces awkward
types that leak their awkwardness into the renderer.

**3. Write the module.** Domain logic in `internal/core/<name>`, taking a `Request` and returning
a `Result`. No printing, no exiting, no config reading, no flag reading. Dependencies declared as
narrow interfaces in `deps.go`.

**4. Write the tests alongside.** Not afterwards. Tests written after the fact test what the code
does; tests written alongside test what it should do, and the difference shows up in the edge
cases.

**5. Wire the command.** A file in `internal/cli` translating flags into a `Request` and handing
the `Result` to the renderer. If the handler starts branching on business conditions, that
branching belongs in the module.

**6. Update the documentation** in the same change. `commands.md`, `cli-reference.md`,
`modules.md` as applicable, plus a `CHANGELOG.md` entry.

**7. `make check`.** Then push.

## Adding a module

The checklist, in the order that avoids rework:

```
[ ] internal/core/<name>/<name>.go   Request, Result, Run
[ ] internal/core/<name>/deps.go     narrow dependency interfaces
[ ] internal/core/<name>/<name>_test.go
[ ] internal/cli/<name>.go           command, flags, wiring
[ ] register the command in the tree
[ ] docs/commands.md                 the command surface
[ ] docs/modules.md                  the module entry
[ ] CHANGELOG.md
```

If step 3 requires importing another `core/*` package, stop. The shared thing moves down a layer
first, into `platform`, or into its own small package below the module layer. Sideways imports
between modules are how this codebase would turn into a knot, and `rules.md` R1 makes it a build
failure rather than a discussion.

## Where tests go

Three places, and the choice is not a matter of taste:

- **Beside the code, using fakes.** Everything above the platform layer. A module declares narrow
  dependency interfaces, and `internal/core/file/fake_test.go` implements them with an in-memory
  filesystem. These tests need no cleanup, can produce a permission error on demand, and behave
  identically on every platform.
- **Beside the code, using a real temporary directory.** `internal/platform/fs` only. This is the
  filesystem seam, and a test of it against a fake tests the fake. They use `t.TempDir()`, so they
  clean up after themselves and depend on nothing about the machine.
- **Beside the code, using a loopback server.** `internal/platform/net` only, for the same reason.
  `httptest.NewServer` and `httptest.NewTLSServer` give a real HTTP and a real TLS endpoint on
  `127.0.0.1`, which is how the redirect handling, the credential dropping, the body cap, and the
  certificate chain get tested against real sockets without the suite depending on the internet.
  A handful of DNS tests resolve `localhost` from the hosts file, and a couple use a `.invalid`
  domain, which RFC 2606 reserves so it can never resolve.
- **`tests/`, behind a build tag.** Cross-cutting scenarios: a whole operation end to end on a
  real tree (`integration`), and the built binary run as a process (`e2e`).

If you are adding a test that touches a real disk or a real socket anywhere other than
`internal/platform/*` or `tests/`, that is a signal the code under test is reaching past its
dependency interfaces.

**No test makes an outbound connection.** Loopback and the hosts file only. A suite that fails
when the office wifi does is a suite people learn to ignore.

## Adding a command

The pattern is set by `internal/cli/version.go`, which is short on purpose: read it before
writing a new one.

1. A constructor returning `*Command` with `Name`, `Summary`, `Usage`, `Description`, and at least
   two `Examples`.
2. `SetFlags` if the command has its own flags. Global flags are already registered; do not
   redefine them.
3. `Run(ctx, env, args)`: validate the positional arguments, build the module request, call the
   module, and hand the result to `env.Emit(data, text)`.
4. Register it in `NewRoot` in `root.go`.

`env.Emit` takes the data and a function that writes the human view of it. The renderer picks. A
handler must never branch on `--output` itself; that is what keeps the JSON and the table from
drifting apart.

`env.EmitTable(data, text, table)` is the same thing plus a row view, and it is what makes
`--output csv` work for a command. Supply one only when the result really is rows: a summary of
named values has no honest CSV form, and a command that does not supply a row view reports that
plainly instead of emitting an invented shape. Rows carry unformatted numbers, no thousands
separators and no percent signs, because a spreadsheet handed "1,204" has to be told it is a
number.

Non-fatal problems go through `env.Warn(code, message, attrs...)`, which records them in the result
envelope *and* logs them. Both audiences get served from one call.

## Working on platform code

Anything under `internal/platform` needs care, because it is the only place that differs per
operating system.

- One file per platform with build tags, exposing an identical exported surface.
- The exported surface is decided once and does not vary: no function that exists only on
  Windows.
- Tests for platform code are platform-tagged and run on that platform in CI.
- Test on Windows if you can. It is the primary platform, and it is where the surprises are: paths
  over 260 characters, UNC paths, junctions, locked files, case-insensitive but case-preserving
  comparison.

If you only have one platform available, say so in the pull request. CI covers the others, and a
reviewer with the missing platform can verify.

## Debugging

`--verbose` first. It prints the full error chain, the resolved configuration with the origin of
each value, and per-item detail from the running module. Most problems are visible there.

`devnest doctor` for anything that looks environmental.

For a real debugger, `dlv debug ./cmd/devnest -- scan .`.

For a performance question, `make bench` first to see whether it is measurable at all, then
`pprof`. "This should be faster" without a number does not get merged; see `performance.md`.

## Things that will bite you

**Log output on stdout.** Easy to do by accident with a stray `fmt.Println` while debugging, and
it breaks every pipeline. A test asserts stdout stays clean; run `make test` before pushing and it
catches you.

**Forgetting `%w`.** `fmt.Errorf("...: %v", err)` compiles fine and silently breaks `errors.Is`
up the chain. The linter catches it.

**Path assumptions.** Hardcoded `/` separators, assuming a path fits in 260 characters, assuming
case-sensitive comparison. All fine on Linux, all broken on Windows.

**Tests that pass because of ordering.** Run `go test -count=5 -race ./...` when something feels
flaky. Map iteration order is randomised per run, which finds these quickly.

**Sideways module imports.** `tests/boundaries_test.go` fails the build, but it is easier to design
around than to refactor out afterwards.

**Forgetting the second example.** Every runnable command needs a description, a usage line, and at
least two realistic examples. `TestEveryRunnableCommandIsDocumented` fails the build without them,
which is deliberate: help text is the interface.

**`-race` failing on Windows.** That is the missing C compiler, not your change. See the
prerequisites above.

**Registering a flag name twice.** The `flag` package panics, and only when that command runs.
`TestEveryCommandBuildsItsFlagSet` catches it at build time now, but it is worth knowing why the
test exists: a group's shared flags and a command's own flags are registered on the same set, so
`--depth` in both places is a crash.

**A composite literal in an `if` condition.** `if err := System{}.Do(); ...` does not parse; Go
reads the brace as the start of the block. Assign it to a variable first.

**Package `net` shadowing the standard library.** Inside `internal/platform/net`, the standard
library's `net` is imported as `stdnet`. `net/http` and `net/url` are unaffected: only the bare
name collides.

**Treating a network failure as an error.** In `core/network`, an unreachable host, a refused
connection, and an expired certificate are results. Returning them as errors breaks the exit-code
contract that makes these commands usable in cron. Only a failure to *ask* the question is an
error.

## Working on the log module

`internal/core/log` reads files that do not fit in memory, so a few rules there are stricter than
elsewhere.

**A line is borrowed, not owned.** `scanner.line` points into the read buffer and is valid until
the next line. Keeping one means copying it, and every place that keeps something does so on
purpose: `counter.add` converts to a string on first sight of a value, and the message and excerpt
fields go through `truncate`. A slice stored anywhere else will be quietly overwritten a few
thousand lines later, and the resulting bug looks like corrupted input rather than aliasing.

**Do not add a second parser.** `http`, `status`, and `top` are projections of one collection pass
in `access.go`. A new command that needs access-log fields adds a projection, not another parse. A
test asserts the three agree on the request count, which is the property this protects.

**Nothing per line.** Anything in the visit function runs ten million times on a real file. That
rules out `fmt.Sprintf`, a `regexp`, a `strings.ToLower` that allocates, and a map write keyed by a
fresh string. The existing helpers take a reusable buffer as a parameter for exactly this reason,
and `make bench` reports allocations per run so a regression is visible as a number.

**A malformed line is a count, not an error.** Returning an error from the visit function aborts
the whole run. That is right for cancellation and wrong for a line that does not parse, and the
difference is the difference between a tool people use on real logs and one they do not.

## Working on the env module

`internal/core/env` runs other people's programs, so a few rules there are stricter than elsewhere.

**Every probe is bounded.** A version flag that opens a network connection or waits on a lock is
common, and an unbounded probe would hang the whole summary. `platform/proc` gives every invocation
a timeout with a default; a new call site never has to remember one, and must not remove one.

**No shell, ever.** Commands are run with their arguments as a list, never as a string handed to a
shell. A string is how an argument with a space becomes two arguments and a path with a semicolon
becomes two commands. The tables in the module supply the arguments, never the user.

**Locate before you run.** A tool not on PATH is never started. The lookup is cheap and the process
creation is not, and on a typical machine most of the table is skipped without a single process.

**Two platforms, one seam.** What makes a file runnable (execute bit versus PATHEXT) and what a
shell calls it (name versus stem) differ by operating system and live in `proc`'s build-tag files.
If you find yourself writing `runtime.GOOS` in the module, the decision belongs one layer down.

## Working on the scan module

`internal/core/scan` walks trees that can be enormous, so it shares the log module's discipline.

**Skip before you descend.** The ignore rules are applied through the walker's `Skip` hook, which
runs before a directory is read. Skipping `node_modules` after walking into it is the most common
way to make a scanner slow.

**Nothing per file that allocates.** The line counter reuses one buffer across the whole walk. A
tree can hold a hundred thousand files, and anything allocated per file shows up.

**Classification is not yours.** Whether a path is source or generated is `internal/classify`'s
answer. If a rule is wrong, fix it there, where `clean` and `secret` will get the fix too. Do not
grow a second table in the module.

## Working on the security module

`internal/core/security` handles passwords, so a few rules there are stricter than elsewhere.

**Do not add a logger.** The module deliberately has none. If you find yourself wanting one, the
thing you want to log is almost certainly the thing that must not be logged.

**Never put input into a finding.** Messages in `CheckStrength` are fixed strings chosen by code.
Assembling one from the password (quoting the sequence you found, naming the dictionary
word) turns a result into a leak, because results are serialised, exported, and pasted into
tickets.
`TestCheckStrengthFindingsNeverVaryWithTheInput` fails if a message varies with what was typed,
which is the property that makes leakage structurally impossible rather than merely avoided.

**Never reach for `math/rand`.** Randomness arrives as an `io.Reader` parameter. If a new function
needs randomness, take it the same way rather than importing a package-level source.

**Do not add a second hashing implementation.** Both `core/file` and `core/security` hash things,
and both go through `platform/fs`. `DigestReader` exists so that hashing a string uses exactly the
code that hashes a file. Two implementations is how two commands end up disagreeing about what
SHA-256 means.

**Test the secret handling, not just the happy path.** Every claim in `docs/security.md` about this
module has a test behind it. If you weaken one, a test should stop you.

## Repository layout

Quick orientation; full detail in `folder-structure.md`.

```
cmd/devnest/           entrypoint: argv, signals, exit code. Nothing else.
internal/cli/          command tree, flags, help, wiring, the only place that parses argv
internal/core/file/    organise, duplicate, rename, filter, size, hash
internal/core/network/ monitor, http, latency, ping, dns, ssl, scan
internal/core/security/ password, password-check, hash, checksum, encode, decode
internal/core/log/     analyze, http, errors, status, top, search, stats
internal/core/env/     summary, list, path, which, vars
internal/core/scan/    summary, types, lines, tree
internal/classify/     file category and language rules, shared below the modules
internal/platform/proc/ running a program under a timeout, locating one on PATH
internal/platform/sys/  os, arch, shell, terminal, environment
internal/platform/fs/  walking, stating, moving, hashing, path rules, protected paths
internal/platform/net/ HTTP with timing, DNS, TLS inspection, TCP probing
internal/config/       four-layer resolution and the TOML decoder
internal/errors/       codes, wrapping, classification, exit codes
internal/logging/      the slog-based logger and its text handler
internal/output/       the result envelope, renderers, tables, terminal detection
internal/version/      link-time build metadata
tests/                 checks that span the whole application; unit tests live beside their code
docs/                  everything in this directory
```

A few concrete pointers for finding your way around `internal/cli`:

- `command.go`: the `Command` type, the `Env` handed to handlers, and the argv splitter.
- `execute.go`: startup order: parse, resolve configuration, build the logger, build the
  renderer, run.
- `help.go` (help rendering. `globals.go`) the global flags.
- `version.go` and `help_command.go`: the two commands, and the pattern any new one follows.

## Getting help

Open a discussion for design questions, an issue for defects. For a change that will take real
effort, open an issue first and get agreement on the approach: it is a much better outcome than
a large pull request that turns out to be pointed in the wrong direction.
