# Roadmap

Status: phases 0 through 10 complete; 0.1.0 released 2026-07-25
Last revised: 2026-07-25

Where the project is going and roughly in what order. Phases are ordered by dependency, not by
date: a phase ships when it is done, and the ordering exists so that each phase has a working
foundation under it.

## Phase 0: Foundation *(complete)*

Documentation, architecture, folder structure, and rules. No code.

The point of doing this first is that the structural decisions are the ones that are expensive to
change later. Deciding the layering after four modules exist means rewriting four modules.

**Deliverables:** the documents in `docs/`, the directory layout, the root files, and the rules in
`rules.md`.

**Done when:** a contributor can read `architecture.md` and `modules.md` and know exactly where a
new feature goes without asking.

## Phase 1: Skeleton *(complete)*

The application runs, does nothing useful, and everything structural is in place.

- `cmd/devnest/main.go`: signal handling, panic recovery, exit code mapping.
- `internal/cli`: command tree, global flags, help text, `help` and `version`.
- `internal/errors`: codes, wrapping, classification, exit mapping.
- `internal/logging`: levels, both handlers, stderr discipline.
- `internal/output`: the envelope, the table and JSON renderers, terminal detection.
- `internal/config`: four-layer precedence, TOML decoding, validation.
- `internal/version`: link-time metadata.
- CI on all three platforms, the import-boundary check, the coverage floor, and the linter.

**Done:** `devnest version --output json` works, the exit code contract is tested in-process and
end-to-end, and stdout carries no log output at any verbosity.

Two items moved out of this phase:

- **`internal/platform`** was not needed. Nothing in Phase 1 requires a platform abstraction that
  the standard library does not already provide portably. It is created in Phase 2, alongside the
  first module that needs socket enumeration and process inspection.
- **The CSV and Markdown renderers** were deferred. Neither has a consumer until a command produces
  tabular data, and a renderer written without one is a renderer written against a guess. They
  arrive with `scan` in Phase 2.

The first real command comes only after this. A skeleton built around a working command tends to
inherit that command's assumptions.

## Phase 2: File tools *(complete)*

The file module, and the platform layer it needed.

- `internal/platform/fs`: walking, stating, resolving, containment, moving, digests, and the
  per-platform rules for path case, hidden files, and protected directories.
- `core/file`: organise, duplicates, rename, filter, size, hash.
- `internal/cli`: the `file` command group, the table renderer, size-with-units flags, and the
  confirmation prompt.

**Done:** all six operations ship with tests at three levels: fakes for the module, a real
temporary directory for the platform layer, and the built binary end to end. `internal/core` is
above the 80% floor and so is the project overall.

Two things moved out of this phase:

- **The CSV and Markdown renderers** are still deferred. The table renderer arrived because the
  file commands produce rows and needed one; CSV and Markdown still have nobody asking. They land
  with the first user who wants a file listing in a spreadsheet or a report in a ticket.
- **`devnest hash` and `devnest scan size`**, as planned in Phase 0, are now `devnest file hash`
  and `devnest file size`. Both belong beside the other file operations. The two things a separate
  `hash` command would have added are now settled: checksum file verification shipped as a flag on
  `security checksum`, and tree digests are not planned.

## Phase 3: Network tools *(complete)*

The networking module, and the platform layer it needed.

- `internal/platform/net`: HTTP with a timing breakdown and a recorded redirect chain, DNS
  resolution, TLS chain inspection, and TCP probing.
- `core/network`: monitor, http, latency, ping, dns, ssl.
- `internal/cli`: the `network` command group and duration flags.
- Configuration: `[http]` renamed to `[network]`, gaining `attempts` and `interval_ms`.

**Done:** all six commands ship with tests at three levels: fakes for the module, a loopback
server for the platform layer, and the built binary end to end. `internal/core/network` is at 94%.
No test makes an outbound connection.

Decisions worth carrying forward, recorded in `modules.md` and `security.md`:

- **`ping` is a TCP probe, not ICMP**, because ICMP needs elevation and DevNest never asks for it.
  Every result says which was used.
- **A failure to reach something is a result, not an error**, which is what lets the exit code
  mean "the site is down".
- **`--insecure` is one flag plus a warning**, not two flags. The reasoning is in `security.md`.

## Phase 4: Security tools *(complete)*

The defensive security module, sharing the digest implementation already in the platform layer.

- `core/security`: password generation, strength checking, hashing, checksum verification, and
  Base64.
- `platform/fs`: gained `DigestReader`, `DigestLength`, and `AlgorithmForLength`, so that hashing
  a string and hashing a file are one implementation and a checksum can be recognised from its
  length.
- Configuration: a `[security]` section for password defaults.

**Done:** all six commands ship with tests at three levels. `internal/core/security` is at 93%.

Decisions worth carrying forward, recorded in `security.md` and `modules.md`:

- **The module has no logger**, which is the surest way to guarantee a password never reaches one.
- **Strength findings are fixed strings**, never assembled from the input, so a result can be
  exported without leaking what was typed.
- **A dictionary match caps the score** rather than only subtracting from it.
- **Randomness is a parameter**, so the generator is testable and `math/rand` is unreachable.

One redundancy was created knowingly: `devnest file hash` and `devnest security hash` overlap.
They share an implementation, and differ in that the first takes several files while the second
adds text and standard input. **Both stay**: each is the shortest spelling of a different job, and
collapsing them would put text and standard input on a file command or a directory of build
artefacts on a security one. The reasoning is in `modules.md`.

## Phase 5: Log tools *(complete)*

Reading log files too large to open in an editor, and reporting what is in them.

- `core/log`: analyse, HTTP access summary, error summary, status distribution, top endpoints,
  keyword search, and line statistics.
- `platform/fs`: gained `Open`, so a module can stream a file it could not hold.
- `internal/output`: gained a CSV renderer, and with it the row view a command supplies when its
  result is genuinely rows.

**Done:** all seven commands ship with tests at three levels, plus benchmarks in `benchmarks/`
against a generated 200,000 line access log. The figures are recorded in `performance.md`.

Decisions worth carrying forward, recorded in `modules.md` and `performance.md`:

- **One pass and one reused buffer**, so a four gigabyte log costs the same resident memory as a
  four kilobyte one. Everything the module returns is a bounded aggregate.
- **A malformed line is counted, not fatal.** Real log files carry rotation notices and entries
  from other programs, and a summary of the rest is what the user came for.
- **The three HTTP commands are projections of one collection pass**, which is why they can never
  disagree about how many requests a file holds.
- **Counters hold pointers**, which took the HTTP summary from 800,141 allocations per run to
  1,273.
- **CSV is opt-in per command.** A result that is a handful of named values has no honest CSV form,
  and says so rather than inventing one.

## Phase 6: Environment and project analysis *(complete)*

Two modules and two new platform packages: what is installed on the machine, and what a project
tree is made of.

- `platform/proc`: running a program under a timeout, and locating one on PATH. The two operating
  systems differ most here, and the differences (execute bit versus PATHEXT, name versus stem) live
  in its build-tag files.
- `platform/sys`: OS, architecture, shell, terminal, and the environment.
- `core/env`: a machine summary, toolchain detection, PATH inspection with shadowed executables, a
  resolve-everywhere lookup, and an environment listing with credentials masked.
- `core/scan`: a structural summary, a file-type breakdown, a line count split into code, comment,
  and blank, and a tree listing.
- `internal/classify`: the file category and language rules, in their own leaf package below the
  modules because `clean` and `secret` will need them too.

**Done:** all nine commands ship with tests at three levels, `internal/core` at 92%. The category
table did become a shared package, as planned, though as a new one rather than by moving
`core/file`'s: the two answer different questions and merging them served neither.

Decisions worth carrying forward, recorded in `modules.md` and `architecture.md`:

- **A missing tool, or a mute one, is a result.** Only a request that cannot be carried out at all
  is an error.
- **Every toolchain probe is bounded and shell-free**, because a version flag that hangs would
  otherwise hang the summary.
- **Credentials are masked by variable name, in the result itself**, because a listing gets
  attached to a ticket.
- **The scan skips what the project ignores**, before descending, or a small Node project reports
  four hundred thousand files.

## Phase 7: Data and encoding *(complete)*

Two modules that read what the user already has: encoded values, and structured documents.

- `core/encoding`: hex, URL percent-encoding, and JWT decoding. Base64 was already shipped in
  `core/security` in Phase 4 and was not duplicated.
- `core/data`: validate, format, minify, query, and convert between JSON, YAML, and CSV.
- `internal/cli`: the `encode`, `decode`, `json`, and `yaml` groups.

**Done:** all thirteen commands ship with tests at three levels, `internal/core` at 92%. The size
limit, the exit codes, and the shape of stdout are covered end to end, because those are what a
script depends on.

Decisions worth carrying forward, recorded in `modules.md` and `commands.md`:

- **The query syntax is a path expression and nothing more**, which settles the open question in
  `prd.md`: keys separated by dots, elements by `[n]`, awkward keys in `["quoted brackets"]`. No
  filters, no wildcards, no functions. A query language is a product of its own, and a half-built
  one is a worse `jq` that nobody has documented.
- **Reprinting and re-encoding are different operations and are kept apart.** `format` and `minify`
  work on the document's own bytes, so key order and number precision survive; `query` re-encodes
  what it selects and says that its keys come back sorted.
- **There is no `yaml format`**, because re-emitting YAML deletes every comment in the file.
- **A nested value is reported, never stringified into a CSV cell.** A spreadsheet that looks
  converted and is not gets found weeks later, in a report.
- **This module holds documents in memory and says so**, with a 64 MiB limit that fails with a
  sentence rather than an out-of-memory kill. The streaming module is `log`, and the error says so.

**The first dependency arrived here**: `github.com/goccy/go-yaml`, MIT, no transitive dependencies.
Hand-writing a YAML parser is not a reasonable use of anyone's time, and `modules.md` had said so
since Phase 0. CI now pins the whole module graph to an allow list, so a second dependency is a
decision rather than an accident.

## Phase 8: System modules *(complete)*

Where the cross-platform work got real, and where the first command that deletes data landed.

- `platform/net`: socket enumeration, in three implementations behind one surface. Windows calls
  the IP Helper API through `syscall`, Linux parses `/proc/net` and resolves socket inodes through
  `/proc/<pid>/fd`, macOS runs `lsof`.
- `platform/proc`: naming a process, asking it to exit, and killing it. Three more build-tagged
  files, and one honest gap: Windows has no way for one process to ask another to exit politely.
- `platform/fs`: gained `RemoveAll` and `DeviceID`.
- `core/port`: list, check, free.
- `core/clean`: scan, apply, and the rule table.
- `internal/cli`: the `port` and `clean` groups.

**Done:** eight commands with tests at three levels. `internal/core` is at 91%. The clean tests
are mostly refusals rather than successes, which is the shape the phase called for: every guard
has a test asserting nothing was removed.

Decisions worth carrying forward, recorded in `modules.md`, `security.md`, and `architecture.md`:

- **macOS shells out to `lsof` rather than calling `libproc`**, because `libproc` needs cgo and
  DevNest builds with it off so releases stay static and cross-compilable. The plan said `libproc`;
  the trade was not worth it for one command on one platform.
- **Windows cannot ask a process to exit**, so `port free` there requires `--force` and says why,
  rather than dressing a kill up as a polite request. The distinction matters when the process is
  a database.
- **Process ownership is the operating system's decision, not DevNest's.** No ownership check is
  computed here; the kernel refuses and the refusal is reported. A second opinion would either
  duplicate that check or contradict it.
- **A port held by more than one process is refused**, never guessed at, and the pid is
  re-verified against the port immediately before signalling.
- **`clean` needs evidence, not a name.** A generic directory name counts only when a project
  marker sits beside it, so `build` is build output next to a `package.json` and somebody's work
  everywhere else. Size, age, and emptiness are never evidence.
- **Every candidate is re-checked in the moment before removal**, because the tree between a scan
  and a delete has had time to change.
- **`vendor` is deliberately not a rule.** It is checked in on purpose in plenty of repositories,
  and deleting it breaks an offline build.

## Phase 9: Repository and secret scanning *(complete)*

- `core/git`: summary, branches, stale branches, contributors, large objects.
- `core/secret`: sixteen rules, entropy scoring, redaction, working-tree and history scanning.
- `platform/proc`: gained a per-command output limit, because the default is sized for a version
  banner and git listing every object in a repository is not that.

**Done:** nine commands with tests at three levels. `internal/core` is at 91%.

Decisions worth carrying forward, recorded in `modules.md` and `security.md`:

- **The history-scanning question from `prd.md` is settled: the working tree is the default and
  history is a separate command.** A pre-commit hook wants the tree and wants it fast; an audit
  wants the history and can wait. Making history the default would have put minutes into every
  hook, and a hook people disable protects nothing.
- **`secret history` reads added lines only**, and reports one credential once however many times
  it was added and reverted.
- **A finding has no field holding the value.** Redaction happens where the finding is built, not
  where it is rendered, so no output format, verbosity, or export can leak one. A test serialises
  a result to JSON and fails if a credential appears anywhere in it.
- **Every rule carries an entropy floor**, including the provider-prefix ones, which is what keeps
  `sk_live_XXXXXXXXXXXX` out of a report that would otherwise teach people to ignore it.
- **`git` is read-only and the test proves it**: the fake records every invocation and fails the
  build if any is a subcommand that can write.
- **Fields are joined with 0x1F, not a null byte.** An argument vector is null-terminated, so no
  argument may contain one; a tab or a pipe appears in commit subjects.

## Phase 10: Polish and the first release *(complete)*

- Shell completion for PowerShell, bash, zsh, fish. *(done)*
- `core/doctor`. *(done)*
- `--export` on every command, the markdown renderer, and `devnest export` multi-command
  reports. *(done)*
- `core/config`, which was not on this list and should have been: the documentation had told
  people to run `devnest config` since Phase 0, and the pass over the examples below is what
  found it. *(done)*
- Packaging: winget, a Homebrew tap, deb and rpm from the release page, tarballs. *(done: `.goreleaser.yaml` and the
  release workflow build the archives, the packages, the checksums, the winget manifests, and the
  Homebrew cask on a tag, then download what was published and run it on all three platforms. The
  two submission channels stay switched off until their repositories exist; `release-process.md`
  records the one field each that turns them on.)*
- Documentation pass across everything, with all examples tested. *(done: every `devnest` line in
  the documentation is checked against the real binary by a test, which is what found the missing
  `config` group, three renamed commands, and a flag that never existed.)*
- A full benchmark run against `performance.md`, with baselines committed. *(done: every target
  has a benchmark, the numbers are in `performance.md`, and `benchmarks/baseline.txt` is the
  committed baseline.)*

**Done:** 0.1.0 was released on 2026-07-25 from a tag, through the pipeline in
`.goreleaser.yaml`: six static binaries, archives, `.deb` and `.rpm`, checksums, and the generated
winget and Homebrew manifests. The workflow verifies the tagged commit on all three platforms
before publishing, then downloads what it published and runs the end-to-end suite against it.

Decisions worth carrying forward, recorded in `modules.md`, `security.md`, and `performance.md`:

- **A completion script is generated from the command tree and baked in**, rather than resolved by
  calling the binary on every keystroke. Completion stays instant and works when the binary is
  busy; the cost is that a script goes stale on upgrade, which its own header says.
- **`doctor` returns no errors.** A broken installation is the result it exists to produce, and the
  CLI turns a failed check into an exit code after the report is on screen. It is also the one
  command that starts when the configuration file will not load.
- **The markdown renderer is generated from the JSON**, not from a view written per command, which
  is what stops a report and a terminal run from describing one result two ways. The field-name
  conventions in `export-system.md` are what make that possible.
- **Configuration edits rewrite one line.** The file is hand-written and hand-commented; a value
  the schema rejects is refused before anything is written.
- **The documentation pass started with a test, not a reading.** Every documented `devnest` line
  runs against the real binary. It found nineteen broken examples, including a whole command group
  that had been documented since Phase 0 and never built.
- **Benchmarking found one defect and one non-defect.** `secret scan` allocated a buffer per file,
  579 MB over ten thousand of them; and the four seconds those commands take turned out to be the
  operating system opening files, proved by a standard-library baseline that is slower still.

**1.0 means the compatibility promise starts.** Command names, flag names, JSON field names, and
exit codes are frozen for the major version. It is not declared until the surface has been used
enough to be confident about it, because withdrawing a promise is much harder than delaying one.

## Before 1.0

**Shipped since 0.2.0**, both listed below as after-1.0 directions and both moved up for the same
reason: a flag only adds surface, so waiting for the compatibility freeze bought nothing.

- `security checksum --check`, which verifies a published `SHA256SUMS` rather than one pasted
  digest. Windows still has no `sha256sum -c`.
- `secret scan --baseline` and `--update-baseline`, so a repository with historical findings can
  adopt scanning and gate on what is new. This one moved up because it is the difference between a
  scanner an old project can use and one it cannot: a check that fails on day one is a check
  somebody turns off in a week.

**Shipped since 0.1.0.** `network scan`, a TCP connect scan of a host's ports, was added early
because the network group was the natural home for it and it shares the ping decision rather than
complicating it: a connect scan needs no raw socket and therefore no elevation, parallelism is
bounded and capped so a scan does not look like an attack, and service names come from a static
registry rather than from connecting to the service. It deliberately stops short of version
detection, banner grabbing, and every SYN-scan technique, which remain out of scope; the reasoning
is in `modules.md`.

## After 1.0

Not commitments. Directions, in rough order of how likely they are to be worth doing.

**Scaffolding.** `devnest init` with templates from `templates/`. Deferred past 1.0 because doing
it well means a template ecosystem, and doing it badly means another mediocre generator.

**Project-local configuration.** Currently excluded on purpose; see `configuration.md`. If real
usage shows a narrow set of keys that genuinely belong per-project, a `.devnest.toml` limited to
those keys (never the safety-relevant ones) becomes reasonable. **`clean` is not among them**, and
that is settled rather than pending: a file that travels with a clone must never widen what a
delete command will remove. The reasoning is in `modules.md`.

**Scan comparison.** Diff two scans to show growth over time. Useful for tracking a repository
that keeps getting bigger and nobody knows why.

**Git hotspot analysis.** Files by change frequency, as a proxy for where the risk concentrates.

**More toolchains in `env`.** A table entry each, so this is contribution-friendly and does not
need a maintainer.

**A `pkg/` surface.** If demand appears for using a module as a library. The domain layer is
already shaped for it; what is missing is the API stability commitment, not the code.

## Explicitly not planned

Restated from `prd.md` because roadmap documents attract feature requests:

- Deterministic tree digests. A directory hash is a specification, not a feature, and nothing
  consumes one; the reasoning is in `modules.md`.
- Package management, installation, or version switching.
- A daemon, a watcher, or any long-running mode.
- A TUI or interactive shell.
- A build system.
- Secret storage or a credential manager.
- Telemetry, analytics, or update checking, in any form, including opt-in.
- A plugin system in 1.x. If it is ever reconsidered, the mechanism is a subprocess exchanging
  JSON over stdio, never code loaded into this process.

## How this changes

The roadmap is revised when a phase completes or when something learned during a phase changes
what comes next. Revisions edit this document rather than appending to it, so it stays a statement
of the current plan rather than a history of previous plans: that is what version control and
`CHANGELOG.md` are for.

Feature requests belong in issues, not here. Something lands on the roadmap once a maintainer has
agreed it should exist.
