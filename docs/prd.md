# DevNest: Product Requirements

Status: current as of 1.0.0; the scope below is what shipped
Owner: project maintainer
Last revised: 2026-07-24

## Why this exists

A working day involves a lot of small, dull questions. Which process is holding port 5173? How
big has `node_modules` grown across every project on this disk? Did anyone commit an API key
into this repo last week? What is the SHA-256 of this build artifact? Is the JSON that the
staging API just returned actually valid, and what does the `errors[].code` field contain?

None of these are hard problems. All of them are solved somewhere. The trouble is *where*:
`netstat` plus a manual PID lookup on Windows, `lsof -i` on macOS, a shell alias someone wrote in
2019, a webpage that wants you to paste your token into a text box to decode it, a Python
one-liner that only works if the right interpreter is on PATH. The knowledge is scattered across
platforms, shells, and browser tabs, and it evaporates every time you move to a new machine or a
new operating system.

DevNest collects that scattered knowledge into one binary with one consistent interface.

## The problem, stated precisely

1. **Platform fragmentation.** The same task needs different commands on Windows, Linux, and
   macOS. Teams that mix operating systems cannot share scripts or documentation reliably.
2. **Tool sprawl.** Doing routine work requires a personal collection of installed utilities:
   `jq`, `httpie`, `tree`, `ncdu`, a hashing tool, a base64 tool. Each has its own flag syntax,
   its own output shape, and its own installation story.
3. **Output that machines cannot read.** Most small utilities print for humans only. Feeding
   their output into a script means parsing text with regular expressions, which breaks the
   moment the tool changes its formatting.
4. **Unsafe habits.** Because decoding a token or checking a certificate is inconvenient locally,
   people paste sensitive material into web tools. That is a real leak with a real blast radius.
5. **No install story on locked-down machines.** Corporate Windows environments often block
   package managers. A single unsigned-optional executable that needs no runtime is frequently
   the only thing that can actually be deployed.

## The solution

A single statically linked Go executable, `devnest`, that provides a grouped set of subcommands
covering the routine work above. Design commitments:

- **One binary, zero runtime.** Download, put on PATH, done. No interpreter, no shared libraries,
  no installer required.
- **Identical behaviour on every supported platform.** Where the underlying system differs, the
  difference is absorbed inside DevNest, not exposed to the user. `devnest port list` returns the
  same fields on Windows and on Linux.
- **Every command speaks JSON.** A global `--output json` flag makes any command scriptable. The
  human-readable table is a rendering of the same data, never a separate code path.
- **Local by default.** Nothing leaves the machine unless the user explicitly asks for a network
  operation (`devnest http`, and nothing else). No telemetry, ever, including opt-in.
- **Read-only unless told otherwise.** Commands that delete or modify anything require an
  explicit flag or an interactive confirmation, and support `--dry-run` first.

## Target users

**Primary: the working developer on a mixed-OS team.** Writes application code, not
infrastructure. Wants the port freed, the artifact hashed, the payload pretty-printed, and to get
back to the actual task. Values not having to remember which flag does what.

**Secondary: the automation author.** Writes CI steps, pre-commit hooks, and release scripts.
Needs stable machine-readable output and meaningful exit codes far more than pretty tables.
Cares that a command's contract does not silently change between patch releases.

**Tertiary: the newcomer to a codebase or a machine.** Uses DevNest to orient: what toolchains
are installed, how large is this project, what is generated versus authored, is anything obviously
wrong with the environment.

Explicitly *not* a target: platform and SRE teams managing production fleets. DevNest is a
workstation tool. It does not want to become a deployment or orchestration system.

## Feature set

Features are grouped into modules. Each module is independently useful and independently
testable. See `modules.md` for internal detail and `commands.md` for the command surface.

### Core (must exist for version 1.0)

| Module | Command group | What it does |
|---|---|---|
| Environment | `devnest env` | Detects installed toolchains and versions, inspects PATH, reports OS and architecture, flags common misconfigurations |
| Project scan | `devnest scan` | Walks a project tree and reports size, file counts by type, line counts, largest files, and generated-versus-authored breakdown |
| Cleanup | `devnest clean` | Finds and removes build artifacts and dependency caches, always with `--dry-run` available first |
| Ports | `devnest port` | Lists listening sockets with owning process, checks a specific port, releases a port after confirmation |
| Hashing | `devnest hash` | Computes and verifies MD5, SHA-1, SHA-256, SHA-512, CRC32 over files, directories, or stdin |
| Encoding | `devnest encode` / `devnest decode` | Base64 (standard and URL-safe), hex, URL, JWT payload inspection, all fully offline |
| Data | `devnest json`, `devnest yaml` | Validate, format, minify, query with a path expression, convert between JSON, YAML, and CSV |
| HTTP | `devnest http` | Sends a request and reports status, timing breakdown, headers, and body, with redirect and TLS detail |
| Git | `devnest git` | Repository summary, stale branch detection, contributor statistics, large-object report |
| Secrets | `devnest secret` | Scans a working tree or history for credential-shaped strings using a rule set |
| Config | `devnest config` | Reads, writes, and validates DevNest's own configuration |
| Doctor | `devnest doctor` | Self-check: config validity, write permissions, rule set freshness |

### Cross-cutting (part of the platform, not a module)

- Global flags: `--output`, `--quiet`, `--verbose`, `--no-color`, `--config`, `--dry-run`.
- Export pipeline: any command's result can be written to a file as JSON, CSV, or Markdown. See
  `export-system.md`.
- Shell completion for PowerShell, bash, zsh, and fish.
- Structured logging to stderr, never mixed into stdout results. See `logging.md`.

### Deliberately out of scope

These come up often enough that saying no in writing is worth the space.

- **Not a package manager.** DevNest reports that Go 1.25 is installed. It will not install it.
- **Not an editor or a REPL.** No interactive shells, no TUI file browsers.
- **Not a build system.** It cleans build output; it does not produce build output.
- **Not a secret store.** `devnest secret` finds leaked credentials. It does not hold, encrypt, or
  serve them.
- **Not a monitoring agent.** No daemon mode, no background process, no scheduled runs. Every
  invocation starts, does one job, and exits.
- **No plugin system in 1.x.** Third-party code loaded into the process is a security surface and
  a compatibility burden. Extension happens by contributing a module upstream, or by piping JSON
  output into your own tool. This may be revisited after 2.0; see `roadmap.md`.
- **No telemetry, no update check, no network call at startup.** The binary does not phone home.

## Constraints

- Go 1.25 or newer. The language version floor moves only on a minor release, never a patch.
- Dependencies are kept minimal and justified per addition; see `rules.md`.
- The binary must stay under 25 MB uncompressed for the release build.
- A typical command must return in under 200 ms excluding unavoidable I/O or network waits; see
  `performance.md`.
- Windows is the primary development and test platform. Linux and macOS are fully supported and
  covered in CI, but Windows-specific behaviour is never treated as a special case to be patched
  in later.

## How success gets measured

- A developer can replace at least three separate installed utilities with DevNest.
- Every command's JSON output can drive a CI step without text parsing.
- A new contributor can add a module by following `development-guide.md` without asking where
  anything goes.
- No release ever changes an existing command's JSON field names or exit codes within a major
  version.

## Open questions

Decided:

- **Query syntax for `devnest json`** (Phase 7): a small purpose-built path expression. Keys
  separated by dots, array elements by `[n]`, awkward keys in `["quoted brackets"]`, and nothing
  else: no filters, no wildcards, no functions. Adopting an established grammar would have meant
  implementing all of it, and implementing part of one is worse than having neither. Selecting a
  subtree is what a person at a terminal needs; anything past it is what `jq` is for.

- **Whether `devnest secret` scans history by default** (Phase 9): it does not. `secret scan`
  reads the working tree and `secret history` reads the history, because the two have different
  users. A pre-commit hook wants the tree and wants it in under a second; an audit wants the
  history and can wait minutes for it. Making history the default would put that wait into every
  commit, and a hook people disable protects nothing.

- **Whether `devnest clean` needs a project-local allow list file**: it does not, and it is not
  getting one. Such a file would be a file inside a tree, arriving with a clone, widening what a
  delete command is willing to remove. Nobody reads it before running `devnest clean`, and a
  repository that carries one has been handed a say in what gets deleted from the machine that
  checked it out. The three legitimate needs it would serve are already served in the open:
  `--pattern` and `--protect` at the call site, where they are visible in the command that ran, and
  `clean.patterns` and `clean.protect` in the user's own configuration, which the user wrote. Both
  stay subject to the marker rule and neither can lift the root and home directory refusal.

- **Whether a secret baseline may be a project-local file**, given the answer above: yes, and the
  difference is the direction it points. A `clean` allow list widens what a delete command will
  remove on the machine that cloned it; a baseline narrows what a report says, and the worst a
  hostile one can do is hide a finding from a scan that was optional to begin with. It cannot
  delete, cannot lower a severity, and cannot be silent about itself: accepted findings are
  counted in the output, and entries that no longer match are counted too. It is also asked for by
  path rather than discovered by name, so no file appears in a tree and starts suppressing
  findings without somebody having typed its name.

Nothing is carried. Every question opened during the phases has been answered.
