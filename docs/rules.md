# Project Rules

Status: draft, Phase 0
Last revised: 2026-07-23

Rules exist so that structural questions get answered once instead of every pull request. They are
short and blunt on purpose. Where a rule is absolute it says so; where judgement applies it says
that too.

`coding-standard.md` covers style in more depth. This document covers the rules that carry
consequences.

## Code rules

**R1. Layer boundaries are absolute.** The import graph in `architecture.md` is the whole list of
permitted edges. `core` never imports `cli`. `core` never imports `output`. `platform` never
imports `core`. No `core/a` imports `core/b`. Checked in CI; a violation fails the build.

**R2. Domain code does not print.** No `fmt.Print*`, no `os.Stdout`, no `log.Print*` anywhere
under `internal/core` or `internal/platform`. Diagnostics go through the injected logger. Results
go through the return value.

**R3. Only `main` decides to exit.** `os.Exit` and `log.Fatal*` appear in `cmd/devnest/main.go`
and nowhere else. Everything else returns an error. A library call that kills the process cannot
be tested and cannot be reused.

**R4. Errors travel up, wrapped, never swallowed.** Every returned error is either handled or
returned with context added. `_ = someCall()` requires a comment on the same line explaining why
the error genuinely does not matter. Discarding an error silently is a review blocker.

**R5. Context is the first parameter, always.** Any function that performs I/O, spawns a process,
or may run longer than a few milliseconds takes `ctx context.Context` first and honours
cancellation. No `context.TODO()` in committed code.

**R6. Platform differences live in build-tag files.** `runtime.GOOS` comparisons are permitted
only inside `internal/platform`, and even there a build-tag file is preferred. A GOOS check in
`cli` or `core` is a blocker.

**R7. No global mutable state.** No package-level variables that anything writes to after
initialisation. Configuration, loggers, and clocks are passed in. Package-level constants and
genuinely immutable tables are fine.

**R8. No `init()` with side effects.** Registration, config loading, and connection setup happen
explicitly during startup where the order is visible.

**R9. Interfaces are declared by the consumer.** A module defines the narrow interface it needs in
its own package; `platform` provides concrete types that happen to satisfy it. Interfaces are not
declared next to their implementations, and an interface with a single method is a good sign, not
a code smell.

**R10. No reflection outside serialisation.** If a problem seems to need reflection, it usually
needs a plainer data structure. Encoding and decoding are the exception.

**R11. Concurrency is justified in a comment.** Every goroutine has an owner, a termination
condition, and a comment stating why concurrency is worth it here. Unbounded goroutine creation
is forbidden; use a bounded worker pool. The race detector runs in CI.

## Dependency rules

**R12. The standard library is the default.** Before adding a dependency, confirm that the
standard library does not already cover it, and that the amount of code being avoided is
substantial. A dependency to avoid fifty lines is a bad trade.

**R13. Adding a dependency requires a written justification.** In the pull request description:
what it does, why the standard library is insufficient, its maintenance status and release
cadence, its own transitive dependency count, its license, and what happens to DevNest if it is
abandoned.

**R14. No dependency with a copyleft license.** MIT, BSD, Apache-2.0, ISC only. Checked in CI.

**R15. Dependencies are pinned and their checksums committed.** `go.sum` is always in the
repository. Updates are their own pull request, never bundled with a feature.

**R16. No dependency that runs code at import time**, phones home, or requires a build step
beyond `go build`.

## Folder rules

**R17. Every directory's purpose is documented** in `folder-structure.md`. A new top-level
directory is added to that document in the same change that creates it.

**R18. `internal/` by default, `pkg/` by decision.** New code goes under `internal/` unless a
maintainer has explicitly agreed to a public API commitment. See `folder-structure.md`.

**R19. Unit tests sit beside their code.** `foo.go` is tested by `foo_test.go` in the same
package. `tests/` is only for tests that genuinely span the whole application.

**R20. One command group, one file.** `internal/cli/scan.go` holds the scan command group and
nothing else. When a file exceeds roughly 400 lines, split it by subcommand rather than letting
it grow.

## Naming rules

**R21. Package names are one lowercase word.** No underscores, no mixed case, no plurals. `scan`,
not `scanner` or `scan_utils`. If a good single word does not exist, the package boundary is
probably wrong.

**R22. No stuttering.** `scan.Result`, not `scan.ScanResult`. The package name is part of every
call site.

**R23. Full words in identifiers.** `configuration` or `config`, never `cfg`. `request`, never
`req`. `index`, never `idx`. The exceptions are established Go idioms that would look strange
spelled out: `ctx`, `err`, `i` and `j` as loop counters, `ok` for the comma-ok idiom.

**R24. Acronyms keep their case.** `HTTPClient`, `parseURL`, `userID`. Not `HttpClient`,
`parseUrl`, `userId`.

**R25. Exported identifiers are documented.** The comment starts with the identifier's name and
says what it does, not what type it is.

**R26. Commands are nouns and verbs, spelled out.** `devnest config list`, never
`devnest cfg ls`. See `design.md`.

**R27. Test names describe the scenario.** `TestScanSkipsSymlinksByDefault`, not `TestScan2`.

## Error handling rules

**R28. Errors are typed, not stringly-typed.** Every error carries a code from
`internal/errors`. Callers branch on the code, never on the message text.

**R29. Wrapping adds what the caller could not know.** `fmt.Errorf("read config %s: %w", path,
err)` adds the path. `fmt.Errorf("error: %w", err)` adds nothing and is noise.

**R30. Error strings are lowercase and unpunctuated.** They get wrapped into other messages;
capitals and full stops mid-sentence read badly. The user-facing presentation layer handles
capitalisation.

**R31. User-facing messages say what to do next.** An error the user can act on names the action.
See `error-handling.md`.

**R32. Panic is for programmer error only**: a broken invariant that indicates a bug, never a bad
input or a missing file. Any panic reaching the user is a defect, and `main` recovers, logs it as
an internal error with the report URL, and exits 1.

**R33. Exit codes are contractual.** The mapping in `error-handling.md` is covered by tests and
does not change within a major version.

## Testing rules

**R34. New behaviour ships with tests.** Bug fixes ship with a test that fails without the fix.

**R35. Tests do not touch the real filesystem, network, or process table.** Unit tests use fakes
injected through the module's dependency interfaces.

Two exceptions, both narrow:

- **`internal/platform/*`** may use a temporary directory created by the test, untagged. That
  layer *is* the seam to the outside world, and a test of it against a fake tests the fake. It
  must use `t.TempDir()` so it cleans up after itself and depends on nothing about the machine.
- **`tests/`** may do the same behind the `integration` or `e2e` build tag, for scenarios that
  span the whole application.

Anywhere else, a test reaching a real disk means the code under test is reaching past its
dependency interfaces.

**R36. Table-driven tests are the default** for anything with more than two cases.

**R37. No sleeping in tests.** Time is injected; waiting is done with synchronisation, not
`time.Sleep`. A sleeping test is a flaky test with a delay attached.

**R38. Tests are deterministic.** No dependence on map iteration order, wall-clock time, hostname,
or environment. Seeded randomness is fine; unseeded is not.

**R39. Coverage floor is 80% for `internal/core`.** The number is a smoke detector, not a goal:
a test that asserts nothing raises coverage and catches nothing.

**R40. Every supported platform runs the full suite in CI on every push.** A test that cannot pass
on Windows is either fixed or skipped with a documented reason and a linked issue. Disabling the
Windows job is not an option.

## Documentation rules

**R41. Documentation changes in the same pull request as the code.** Not afterwards.

**R42. Every command has help text and at least two realistic examples.** Enforced by a test in
`tests/`.

**R43. `CHANGELOG.md` is written by a person.** Entries describe what changed for a user, not
which files were edited. Generated commit lists are not a changelog.

**R44. Documented behaviour is tested behaviour.** If a document promises an exit code, an output
field, or a flag, a test asserts it.

**R45. Comments explain why.** The code shows what. A comment earns its place by recording a
platform quirk, a benchmark result, a rejected alternative, or a non-obvious constraint.

## Commit and review rules

**R46. Conventional commit prefixes.** `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `perf:`,
`build:`, `ci:`, `chore:`. The subject line is imperative and under 72 characters. The body
explains why, when why is not obvious.

**R47. One logical change per pull request.** A refactor and a feature in one branch cannot be
reviewed properly, and cannot be reverted separately when one of them turns out to be wrong.

**R48. CI must be green before review.** Reviewer time is not for catching lint errors.

**R49. A pull request that changes public behaviour updates the documentation and the changelog.**

**R50. Breaking changes require a major version and a migration note.** JSON field names, exit
codes, flag names, and command names are all public behaviour.
