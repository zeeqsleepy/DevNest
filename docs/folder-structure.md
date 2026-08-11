# Folder Structure

Status: current as of Phase 10
Last revised: 2026-07-25

Every directory in this repository has a stated purpose. If a file does not clearly belong in one
of them, that is a signal the file is doing something the project has not decided about yet.
Resolve the question before adding the file, rather than creating a directory to hold the
confusion.

## Top level

```
devnest/
├── cmd/              application entrypoints
├── internal/         private implementation, the bulk of the code
├── pkg/              public, importable packages (empty by design)
├── configs/          shipped default and example configuration
├── docs/             project documentation
├── scripts/          developer and maintenance scripts
├── assets/           static non-code files embedded or distributed
├── examples/         worked usage examples
├── reports/          local output directory for generated reports (git-ignored)
├── tests/            cross-cutting integration and end-to-end tests
├── testdata/         shared fixture inputs for tests
├── fixtures/         larger, realistic sample projects for scenario tests
├── benchmarks/       performance benchmarks and their baselines
├── templates/        project scaffolding templates
├── tools/            build-time tooling, isolated from the main module graph
├── build/            packaging and release definitions
├── .github/          CI workflows and repository templates
├── go.mod
├── README.md
├── CHANGELOG.md
├── SECURITY.md
├── CODE_OF_CONDUCT.md
├── CONTRIBUTING.md
├── LICENSE
├── Makefile
├── .gitignore
└── .editorconfig
```

---

## `cmd/`

Application entrypoints. One subdirectory per binary; `cmd/devnest/` is the only one, and there is
no plan for a second.

**Belongs here.** `main.go` and nothing else. It constructs the root command, installs the signal
handler, runs, maps the error to an exit code, and returns.

**Does not belong here.** Flag definitions, command implementations, business logic, helper
packages. Everything past process startup lives in `internal/cli`.

**Why it exists.** Go convention, and it enforces the thin entrypoint. Anything that needs to grow
has nowhere to grow here, so it grows in the right place instead.

---

## `internal/`

Private implementation. The compiler enforces that no external module can import anything under
this path, which means everything here can be refactored without breaking anyone. The bulk of the
codebase lives here, and that is deliberate.

```
internal/
├── cli/          command tree, flags, help text, rendering wiring
├── config/       configuration loading, merging, validation
├── core/         one package per module, the actual work
├── errors/       typed errors, error codes, exit code mapping
├── logging/      structured logger
├── output/       renderers: table, JSON, CSV, Markdown
├── platform/     OS-specific implementations behind interfaces
└── version/      build metadata injected at link time
```

**`internal/cli`**: one file per command group (`env.go`, `scan.go`, `port.go`), plus root
command setup and shared flag helpers. This is the only place aware of which CLI framework is in
use.

**`internal/core`**: one subdirectory per module. See `modules.md`. No package here imports
another package here.

**`internal/platform`**: subdivided by concern (`fs`, `proc`, `net`, `sys`). Build-tag files
(`*_windows.go`, `*_linux.go`, `*_darwin.go`) live here and only here.

**`internal/errors`, `internal/logging`, `internal/output`, `internal/config`, `internal/version`**
are the cross-cutting leaves. Any layer may import them; they import no layer.

---

## `pkg/`

Public API surface. Anything placed here can be imported by any Go module in the world, and
removing it afterwards is a breaking change.

**Empty in Phase 0, on purpose.** A package graduates here only when it is genuinely useful
outside DevNest, its API has stabilised through real use, it depends on nothing beyond the
standard library, and a maintainer accepts long-term compatibility responsibility for it.

Promoting a package later is easy. Withdrawing one is not.

---

## `configs/`

Configuration that ships with the project.

**Belongs here.** The annotated default configuration file with every key documented and its
default shown, plus example configurations for common setups.

**Does not belong here.** A user's actual configuration lives in the OS config directory
at runtime, never in the repository. Nothing here is ever secret; these files are published.

---

## `docs/`

All project documentation, in Markdown, flat: no subdirectories until there are enough files to
justify one, and there are not yet.

Every document states its status and last revision date at the top. Documentation that contradicts
the code is worse than no documentation, so it is updated in the same pull request as the change
it describes, not afterwards.

---

## `scripts/`

Scripts a developer or maintainer runs directly: environment setup, cross-platform build helpers,
release preparation, documentation checks, coverage reporting.

**Convention.** PowerShell (`.ps1`) and POSIX shell (`.sh`) variants for anything that needs to
run on all three platforms. Every script starts with a comment saying what it does and how to
invoke it. Scripts are convenience wrappers: the Makefile is the source of truth for what a task
actually consists of.

**Does not belong here.** Anything a user runs. Users run `devnest`.

---

## `assets/`

Static non-code files: the embedded secret-detection rule set, the toolchain detection table,
project logo and icons, the Windows executable manifest and icon resource.

Files embedded into the binary with `go:embed` live here so it is obvious what is compiled in.
Keep this directory small: every embedded byte is in the binary the user downloads.

---

## `examples/`

Worked examples of using DevNest: annotated command sequences, sample CI workflow files, scripts
that consume the JSON output, real configuration files with commentary.

Examples are tested. An example that has drifted out of date teaches the wrong thing to exactly
the people least able to notice. See `testing.md`.

---

## `reports/`

Output directory for reports generated locally by `devnest export`. Git-ignored except for the
`.gitkeep` that keeps the directory present.

It exists in the repository so that examples and documentation can reference a stable relative
path without asking the reader to create a directory first.

---

## `tests/`

Tests that cross module boundaries: end-to-end runs of the built binary, command tree consistency
checks (every command has help text and at least one example), JSON output schema stability
checks, exit code contract verification, and the import-boundary check that enforces the
layering rules in `architecture.md`.

**Unit tests do not live here.** They live beside the code they test, in the same package, per Go
convention. This directory is only for tests that have no single natural home because they are
about the whole application.

---

## `testdata/`

Go treats any directory named `testdata` as invisible to the build, which is why the name is
fixed. This top-level one holds fixtures shared across packages: malformed JSON and YAML samples,
sample toolchain version output for each supported tool, captured platform output for socket
enumeration, and expected-output files for rendering tests.

Module-specific fixtures live in that module's own `testdata/` directory instead. Only genuinely
shared material comes here.

---

## `fixtures/`

Larger, realistic sample material: whole miniature project trees used by scenario tests: a Node
project with `node_modules`, a Go project with a build directory, a Python project with
`__pycache__`, a repository with deliberately planted fake credentials for the secret scanner.

Separate from `testdata/` because these are constructed *environments* rather than input files,
they are large enough that mixing them with small fixtures makes both harder to find, and some
are generated by a script rather than committed whole.

**Planted credentials are always obviously fake**: a documented, invalid, never-issued pattern,
so that scanning DevNest's own repository does not produce a scare.

---

## `benchmarks/`

Performance benchmarks and their recorded baselines. Kept separate from unit tests because they
are slow, they need a quiet machine to mean anything, and they run on a different schedule.

Contains the benchmark code, committed baseline results with the hardware they were measured on,
and the comparison script. Targets are stated in `performance.md`.

---

## `templates/`

Scaffolding templates for project generation. They live under `internal/core/scaffold/templates/`
so they can be embedded into the binary with `go:embed` — a release download must scaffold
exactly what a source build does, and an embed cannot cross a module boundary.

One subdirectory per template, each a plain tree of files that `devnest init` copies. A `.tpl`
suffix is dropped on copy, so a template can carry a `go.mod` without making the scaffolding
package itself look like a nested module. Adding a template is adding a directory of files; the
command that copies them does not change.

The repository root's `templates/` directory is empty, kept only because the original plan
(introduced in phase 0) put templates there.

---

## `tools/`

Build-time and development-time tooling: linter configuration, code generators, the import-boundary
checker, changelog assembly.

Isolated with its own module file so that development tooling dependencies never appear in the
main module graph, and therefore never end up in a user's build or in the project's dependency
audit surface.

---

## `build/`

Everything about producing distributable artifacts: the release build configuration, packaging
definitions for Windows (installer, winget manifest), Linux (deb, rpm, tarball), and macOS
(Homebrew formula), plus container definitions if they ever exist.

**Does not belong here.** Build *output*, which is git-ignored and written to `dist/`.

---

## `.github/`

Repository configuration for the hosting platform: CI workflow definitions under `workflows/`,
issue templates under `ISSUE_TEMPLATE/`, and the pull request template.

CI runs on Windows, Linux, and macOS for every push. Windows is not an afterthought job that gets
disabled when it goes red.

---

## Root files

| File | Purpose |
|---|---|
| `go.mod` | Module definition, Go version floor, dependencies |
| `README.md` | What DevNest is, how to install it, what it does, the front door |
| `CHANGELOG.md` | Human-written record of what changed per release, newest first |
| `SECURITY.md` | Supported versions and how to report a vulnerability privately |
| `CODE_OF_CONDUCT.md` | Behavioural expectations for project spaces |
| `CONTRIBUTING.md` | How to get set up and get a change merged |
| `LICENSE` | MIT |
| `Makefile` | The canonical task list: build, test, lint, benchmark, release |
| `.gitignore` | Build output, local config, editor and OS noise |
| `.editorconfig` | Indentation and line endings, so cross-platform contributors stop fighting |

## Adding a directory

New top-level directories need a reason that survives being written down. Before adding one, ask
whether the files belong in an existing directory whose purpose you have not fully read, whether
this is one file that could sit at root, and whether the directory would still be justified with
only one file in it a year from now.

If a new directory is genuinely warranted, it is added to this document in the same change.
