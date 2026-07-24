# Design Philosophy

Status: implemented through Phase 10
Last revised: 2026-07-24

`architecture.md` says how the code is arranged. This document says what we are trying to make it
feel like, and which trade-offs get resolved in which direction when two good things conflict.

## What DevNest is trying to be

A tool you stop noticing. The best outcome is that someone types `devnest port 5173`, gets the
answer, and never thinks about DevNest again that day. Tools that demand attention (that need
configuration before first use, that print banners, that ask questions) cost more than they
save.

## Principles

### 1. Predictable beats clever

Every command follows the same shape: `devnest <group> <action> [target] [flags]`. Global flags
mean the same thing everywhere. `--dry-run` never performs the action, in any command, without
exception. Someone who has used two commands can guess the third.

Clever shortcuts that only work in one command are a net loss: they save four keystrokes and cost
a documentation lookup every time the user is unsure whether this is the command where the
shortcut applies.

### 2. Safe by default, dangerous only on request

Destructive operations are opt-in twice: the command exists, but it does nothing irreversible
until the user passes an explicit flag or answers a prompt. `devnest clean` with no flags shows
what it *would* delete. Deleting requires `--apply`.

Where a mistake cannot be undone, we accept an extra keystroke from every user forever rather
than one person losing work once.

### 3. Both audiences, one code path

Every result is a struct. The table view and the JSON view are two renderings of that same
struct. There is no scenario where the human output contains information the JSON output lacks,
because that divergence is what makes tools untrustworthy in scripts.

In code, a command hands `Env.Emit` two things: the data, and a function that writes the human
view of it. The renderer selected by `--output` decides which to use. There is no branch on output
format inside a command handler, which is what keeps the two views from drifting apart.

Auto-detection handles the common case: when the stream is not a terminal, colour is disabled
automatically. The *format* is never auto-detected: a command that silently switched to JSON when
piped would break every script written against its table output the first time someone ran it
interactively. Everything else is explicit.

### 4. Local, offline, silent

DevNest makes exactly one kind of network call: the one the user asked for with `devnest http`.
No update check, no telemetry, no error reporting, no analytics, not even opt-in, because an
opt-in switch is still code that could misfire, and its absence is a stronger promise than its
default value.

This is also why token decoding, hashing, and encoding are worth shipping at all. They are
trivial operations, but the alternative people currently reach for is a web page that sees their
data.

### 5. Fast enough to be reflexive

Startup budget is 50 ms; a typical command budget is 200 ms. Beyond correctness, this is the
feature that decides whether a tool becomes a habit. A utility that takes two seconds to answer a
trivial question stops getting used, whatever its capabilities. Concrete targets are in
`performance.md`.

### 6. Errors are part of the interface

An error message is a user-facing feature and gets the same care as help text. It states what was
attempted, what went wrong, and what the user can do next. It does not print a Go stack trace, a
raw syscall number, or a wrapped chain of internal package names.

```
Error: cannot read C:\projects\api\.env
  The file exists but the current user does not have read permission.
  Run the shell as Administrator, or pass --skip-unreadable to continue past it.
```

Diagnostic detail is not thrown away: it goes to stderr under `--verbose`, where someone
debugging can find it. See `error-handling.md`.

### 7. Deleting is design work

Every command carries permanent cost: documentation, tests, compatibility, and the cognitive load
of appearing in `--help`. A feature earns its place by being used often, not by being possible.
When a command's justification is "someone might want it", the answer is no. This is the same
argument recorded as non-goals in `prd.md`, applied continuously rather than once.

## Command-line design

### Naming

Groups are nouns: `env`, `scan`, `port`, `hash`, `git`, `secret`. Actions are verbs: `list`,
`check`, `free`, `verify`, `apply`. Full words, no invented abbreviations: `devnest config
list`, never `devnest cfg ls`. Abbreviations save characters and cost recall, and shell history
plus completion make the character count irrelevant.

Where a group has one obvious action, that action is the default: `devnest scan .` is
`devnest scan run .`, and both work.

### Flags

- Every flag has a long form. Short forms exist only for flags used constantly: `-o` for
  `--output`, `-q` for `--quiet`, `-v` for `--verbose`, `-h` for `--help`.
- Boolean flags read as positive assertions: `--recursive`, not `--no-recursive` with a default of
  true. Negative defaults produce double negatives in scripts.
- A flag means the same thing in every command it appears in.
- Global flags are declared once on the root command, not redefined per command.

### Arguments

The first positional argument is the target: a path, a port, a URL. Commands that operate on a
project default to the current directory, because that is what the user means nine times out of
ten. Commands that operate on something ambiguous require the argument explicitly.

### Output

The default human view is a table: aligned columns, a header, no box drawing, no ASCII art.
Colour is used for meaning only (a status, a severity) never for decoration, and it disappears
when stdout is redirected or `NO_COLOR` is set.

Progress indication appears only for operations expected to exceed roughly one second, only when
stdout is a terminal, and always on stderr so it cannot corrupt piped output.

No emoji in output. They render inconsistently across Windows terminals, break column alignment,
and carry no information a word does not carry better.

### Help

`devnest --help` lists groups with one line each and fits on a standard terminal screen.
`devnest <group> --help` lists actions. `devnest <group> <action> --help` gives the full flag
reference plus at least two realistic examples: examples are the part people actually read.

## Interaction principles

- **Never prompt when a flag would do.** A prompt makes a command unusable in a script. Every
  interactive confirmation has a flag that answers it in advance.
- **Never prompt when stdin is not a terminal.** In that situation an unanswered confirmation is a
  hang. Fail with a message naming the flag to pass instead.
- **Do the safe thing on ambiguity.** If a path could be interpreted two ways, say so and stop.
  Guessing wrong on a destructive command is far worse than asking.
- **Exit codes are contractual.** `0` success, `1` general failure, `2` usage error, `3` not
  found, `4` permission denied, `5` cancelled. Documented in `error-handling.md` and covered by
  tests, because CI scripts branch on them.

## Maintainability goals

The project should stay pleasant to work on after the initial enthusiasm has worn off.

- **A new module touches only new files.** If adding a feature requires editing existing modules,
  the structure has decayed and fixing it takes priority over the feature.
- **Small, single-purpose functions.** A function should fit on a screen. Anything much beyond
  that is doing more than one thing and should be split.
- **Boring code.** Explicit loops over clever generic machinery, plain structs over reflection,
  standard library over dependency. The person reading this code at 2 a.m. during an incident may
  not be the person who wrote it, and may be the person who wrote it having forgotten everything.
- **Comments explain why.** What the code does is visible in the code. Why it does it that way
  (the platform quirk, the benchmark result, the deliberately rejected alternative) is not, and
  that is what a comment is for.
- **Tests are documentation.** A module's test file should show a reader what the module does and
  what it does on the awkward inputs.
- **Dependencies are debt.** Each one is a supply-chain surface, an upgrade obligation, and
  someone else's design decisions imported permanently. The bar for adding one is high and
  written down in `rules.md`.

## Trade-offs, resolved in advance

| When these conflict | We choose | Because |
|---|---|---|
| Feature richness vs. predictability | Predictability | A guessable tool gets used; a capable confusing one gets abandoned |
| Convenience vs. safety on destructive actions | Safety | One unrecoverable deletion outweighs a lifetime of extra flags |
| Human-friendly output vs. machine-parseable output | Both, from one struct | Divergence between them is the bug that destroys trust |
| Fewer dependencies vs. faster implementation | Fewer dependencies | Implementation is one week; maintenance is years |
| A framework's features vs. owning the code | Owning the code, where the surface is small | The CLI tree and the TOML subset are each a few hundred lines; a dependency is forever |
| Cross-platform uniformity vs. platform-native behaviour | Uniformity | Teams are mixed-OS; per-platform behaviour breaks shared docs and scripts |
| Startup speed vs. eager feature loading | Startup speed | 50 ms is the difference between a habit and a chore |
