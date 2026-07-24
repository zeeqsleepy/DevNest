# Contributing to DevNest

Thanks for looking. This is the short version. The full guide is in
[docs/contributing-guide.md](docs/contributing-guide.md).

## Before you write code

**Bug fix?** Open an issue if one does not exist, then go ahead.

**Feature?** Open an issue and get agreement first. The project stays useful by staying narrow,
and there is a list of things it deliberately will not do in [docs/prd.md](docs/prd.md). A
maintainer will say yes, no, or "yes but differently". All three save you time.

**Refactor?** Open an issue explaining what is wrong with the current structure. Large unsolicited
refactors rarely land.

**Documentation?** Go ahead, no discussion needed.

## Setup

Requires Go 1.25 or newer, git, and make.

```
git clone https://github.com/<owner>/devnest.git
cd devnest
make setup
make check
```

Full environment notes in [docs/development-guide.md](docs/development-guide.md).

## The loop

```
make test        unit tests, under 10 seconds, run this constantly
make check       lint + full test suite, what CI runs
make run ARGS="scan ."
```

## Before you push

- `make check` passes.
- New behaviour has tests. A bug fix has a test that fails without the fix.
- Documentation updated in the same commit, not afterwards.
- A `CHANGELOG.md` entry if the change is user-visible.

## Pull requests

One logical change per pull request. CI green before review, because reviewer time is for design and
correctness, not lint errors.

The description should say **why**, not what. What is in the diff.

Commit messages use conventional prefixes: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`,
`perf:`, `build:`, `ci:`, `chore:`. Imperative subject, under 72 characters.

## The rules that fail the build

No point arguing these in review; they are checked mechanically:

- `gofmt`, `go vet`, and the linter configuration in `tools/`
- Import boundaries from [docs/architecture.md](docs/architecture.md): `core` never imports
  `cli`, modules never import each other
- Race detector on all tests
- 80% coverage floor on `internal/core`
- Tests passing on Windows, Linux, and macOS
- Permissive licenses only on dependencies

The full list is in [docs/rules.md](docs/rules.md).

## Things worth knowing

**Adding a dependency** needs a written justification: what it does, why the standard library is
insufficient, its maintenance status, its own dependency count, its license, and what happens if
it is abandoned. See rule R13.

**Domain code never prints.** No `fmt.Println` under `internal/core`. Results come back in the
return value; diagnostics go through the logger to stderr.

**Windows is the primary platform.** Path separators, long paths, case-insensitive comparison, and
junctions are all things to think about, not things to fix later.

## Reporting bugs

Include `devnest version`, `devnest doctor`, your OS, the exact command, what you expected, and
what happened. Redact anything sensitive; paths often contain names.

## Reporting security issues

Do not open a public issue. See [SECURITY.md](SECURITY.md).

## Conduct

[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) applies everywhere. Assume good faith, aim criticism at
the code, and remember the person on the other end is doing this voluntarily.

## License

Contributions are licensed under the project's licence: Apache License 2.0 with the Commons Clause.
By opening a pull request you confirm you have the right to contribute the code and agree to it
being released under those terms. There is no CLA.
