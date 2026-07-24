# Coding Standard

Status: current as of Phase 10
Last revised: 2026-07-24

Style rules for DevNest's Go code. Anything the standard Go toolchain already decides (formatting,
import grouping, brace placement) is decided by the toolchain and not discussed here. `gofmt`
output is the format; there is no house style overriding it.

`rules.md` holds the rules with structural consequences. This document is about how the code reads.

## Baseline

- `gofmt` on save, enforced in CI. A pull request that is not gofmt-clean does not build.
- `go vet` clean.
- The linter configuration in `tools/` is the source of truth for enabled checks. A disabled check
  carries a comment explaining why.
- Effective Go and the Go Code Review Comments wiki apply by default. Where this document is
  silent, they decide.

## Naming

**Packages.** One lowercase word. No underscores, no camelCase, no plurals. `scan`, not `scanner`,
`scanUtils`, or `scans`. If no single word describes the package, the boundary is probably wrong.
Fix the boundary rather than inventing a compound name.

Never `util`, `utils`, `common`, `helpers`, `misc`, `shared`, or `base`. These are names for
"things I have not decided where to put", and a package with such a name accumulates unrelated
code until it depends on everything.

**No stuttering.** The package name is part of every call site: `scan.Result`, not
`scan.ScanResult`. `errors.Code`, not `errors.ErrorCode`.

**Full words.** `configuration`, `request`, `response`, `index`, `value`, `directory`. Not `cfg`,
`req`, `res`, `idx`, `val`, `dir`. Established Go idioms are the exception and stay short: `ctx`,
`err`, `ok`, `i` and `j` for loop counters, `w` and `r` for a writer and reader in a function
whose whole job is copying between them.

**Acronyms keep their case.** `HTTPClient`, `parseURL`, `userID`, `writeJSON`. When an acronym
starts an unexported name it is fully lowercase: `httpClient`, `urlPath`.

**Booleans read as assertions.** `isValid`, `hasErrors`, `shouldRetry`, `followSymlinks`. Not
`valid`, `errorFlag`, `symlinkMode`.

**Errors.** Sentinel values are `ErrSomething`. Types are `SomethingError`. Variables holding an
error are `err`, or `<thing>Err` when several are in scope.

**Tests.** `TestFunctionName_Scenario` or a sentence: `TestScanSkipsSymlinksByDefault`,
`TestCleanRefusesHomeDirectory`. A reader should know what broke from the failure line alone,
without opening the file.

## Functions

**One job each.** A function that validates, transforms, and writes is three functions. The
tell is the comment that says "first we..., then we...".

**Short enough to see whole.** Roughly 50 lines is the point at which to look for a split.
This is a smell threshold, not a limit: a flat 80-line switch over token types is clearer in one
piece than broken up to satisfy a number.

**Shallow nesting.** Three levels is plenty. Deeper usually means guard clauses were skipped:

```go
// preferred
if err != nil {
    return Result{}, fmt.Errorf("read manifest: %w", err)
}
if len(entries) == 0 {
    return Result{}, nil
}
// the actual work, at the top level

// avoided
if err == nil {
    if len(entries) > 0 {
        // the actual work, three levels in
    }
}
```

Handle the exceptional case and return; leave the main path unindented.

**Parameters.** Beyond four or five, take a struct. Consecutive parameters of the same type are a
call-site hazard: `copyFile(src, dst string)` is one transposition away from a bad day, so name
things clearly and consider a struct.

**Return values.** Two or three at most, and `(T, error)` is the standard shape. Named return
values only where they genuinely document a same-typed pair; never as a way to use a naked
`return`, which forces the reader to scan upward to learn what is being returned.

**No boolean parameters at call sites.** `walk(root, true, false)` is unreadable. Use an options
struct or separate functions.

## Types

**Prefer plain structs.** Interfaces are for things with more than one real implementation, or
for a seam a test needs. An interface with one implementation and no test seam is indirection
without benefit.

**Interfaces are declared by the consumer** and kept narrow. A module declares exactly what it
needs:

```go
// deps.go, inside the module that needs it
type fileReader interface {
    ReadFile(path string) ([]byte, error)
}
```

Not a twenty-method `FileSystem` interface that every module drags in.

**Zero values should be usable** where it is cheap to arrange. A struct requiring three setter
calls before it works will eventually be used without one of them.

**Constructors return concrete types**, not interfaces. The caller decides what abstraction it
wants.

**Enumerations are typed strings or typed integers with a `String()` method.** Bare untyped
constants lose all type checking at the call site.

## Comments

**Explain why, never what.** The code shows what.

```go
// Windows reports the socket table in a single call, but the buffer size can
// change between the sizing call and the data call when sockets open. Retry a
// bounded number of times rather than allocating a speculatively large buffer.
```

Not `// get the socket table`.

**Every exported identifier has a doc comment**, starting with the identifier's name and forming a
sentence:

```go
// Run walks the tree rooted at request.Path and returns a structural summary.
// It returns an error only when the root itself is unreadable; problems with
// individual entries are collected in Result.Warnings.
func Run(ctx context.Context, request Request) (Result, error)
```

State the error behaviour. That is the part callers get wrong.

**Package doc comments** live in the package's primary file, say what the package is for and how
it fits into the layering, and are worth two or three sentences rather than one.

**No commented-out code.** Version control remembers. Commented-out code is unreviewable, untested,
and rots silently.

**`TODO` comments carry an owner and an issue.** `// TODO(#142): handle NTFS junction points`.
An unattributed TODO is a wish, and it stays in the codebase forever.

## Error handling in code

Full model in `error-handling.md`; the mechanics:

```go
// wrap with what the caller cannot know: the operation and the subject
if err := os.MkdirAll(dir, 0o755); err != nil {
    return fmt.Errorf("create report directory %s: %w", dir, err)
}
```

- Always `%w`, so `errors.Is` and `errors.As` keep working up the chain.
- Message text is lowercase, no trailing punctuation, these get embedded in other messages.
- No `fmt.Errorf("error: %w", err)`; wrapping that adds nothing is noise in the final message.
- Ignoring an error requires an explicit assignment and a same-line reason:
  `_ = file.Close() // read-only, close failure cannot lose data`.
- Sentinel errors are compared with `errors.Is`, never `==`, and never by string matching.

## Concurrency

- Every goroutine has an identifiable owner and a guaranteed termination path.
- Bounded worker pools only. Never one goroutine per input item over an unbounded input.
- `context.Context` is checked at every loop boundary in a long operation.
- Prefer channels for handing data between goroutines; use a mutex when guarding a small piece of
  shared state is genuinely simpler, and keep the critical section tiny.
- `sync.WaitGroup` for "wait for all", `errgroup` for "wait for all, keep the first error".
- Results are sorted before return, so output is deterministic regardless of completion order.
- Every concurrent block carries a comment stating why concurrency is worth it here.

## Imports

Three groups, separated by blank lines, in this order: standard library, external dependencies,
DevNest packages. `gofmt` keeps them sorted within each group.

- No dot imports.
- No import aliasing except to resolve a genuine name collision.
- No blank imports outside `main`.

## Files

- One command group per file in `internal/cli`.
- A file over roughly 400 lines is a prompt to split by concern.
- File names are lowercase with no separators: `portcheck.go`, not `port_check.go`, except for
  build-tag files, where `ports_windows.go` follows Go's required convention.
- `helpers.go` and `utils.go` are banned as file names for the same reason they are banned as
  package names.

## Formatting details the toolchain does not decide

- Line length: no hard limit, but a line past about 120 columns usually wants an intermediate
  variable with a meaningful name.
- Struct literals in tests use field names, always. Positional literals break silently when a
  field is added.
- Long argument lists break one argument per line with a trailing comma.
- Magic numbers become named constants at their second use; a `0o755` in a single `MkdirAll` call
  is clear enough on its own.
