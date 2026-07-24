# CLI Reference

Status: partially implemented. The grammar, global flags, exit codes, and the `file`, `network`,
`security`, `log`, `env`, `scan`, `encode`, `decode`, `json`, `yaml`, `port`, and `clean` command
groups below are live as of Phase 8
Last revised: 2026-07-24

`commands.md` lists what the commands are. This document covers how the interface is put
together: grammar, naming discipline, flag conventions, and the rules that keep the surface
consistent as it grows.

## Grammar

```
devnest <group> [action] [target...] [flags]
```

- **group**: a noun naming an area: `env`, `scan`, `clean`, `port`, `hash`, `git`, `secret`.
- **action**: a verb: `list`, `check`, `verify`, `apply`, `free`, `scan`. Omitted when the group
  has one obvious default.
- **target**: what to operate on: a path, a port number, a URL. Path-taking commands default to
  the current directory.
- **flags**: long form always, short form only for the handful used constantly.

Both of these are valid and identical:

```
devnest scan
devnest scan run .
```

Two levels of nesting is the ceiling. `devnest a b c d` means the grouping is wrong and the
command belongs somewhere else.

**`devnest <group> help` is the same as `devnest <group> --help`.** The rule is general rather
than a `help` subcommand added to each group, so it works the moment a group exists.

## Flags shared across a group

Where several commands need the same option, it is registered once and means the same thing in
each of them. The `file` group shares these:

| Flag | Default | Meaning |
|---|---|---|
| `-r, --recursive` | varies | Descend into subdirectories |
| `--depth <n>` | 0 | Limit how deep to descend; 0 means unlimited |
| `--include-hidden` | off | Include dotfiles and, on Windows, hidden entries |
| `--follow-symlinks` | off | Descend into symlinked directories, with cycle detection |
| `--exclude <glob>` | none | Skip matching entries; repeatable, and added to the configured list |

The default for `--recursive` differs by command, and deliberately so. Read-only commands
(`duplicate`, `filter`, `size`) default to recursive, because that is what you mean when you ask
what is in a tree. Commands that move files (`organize`, `rename`) default to the single directory
you named, so an existing folder structure is not rearranged by accident.

`devnest file size` is the one exception to the shared list: it always measures the whole tree, so
a walk-depth flag would be meaningless there, and `--depth` instead controls how many directory
levels appear in the report.

The `network` group shares these:

| Flag | Default | Meaning |
|---|---|---|
| `--timeout <duration>` | `network.timeout_ms` | Give up after this long, for example `5s` |
| `--insecure` | off | Skip certificate verification; warns every time it is used |
| `--attempts <n>` | `network.attempts` | How many measurements to take (`latency`, `ping`) |
| `--interval <duration>` | `network.interval_ms` | Pause between attempts (`latency`, `ping`) |
| `--port <n>` | 443 | TCP port (`ping`, `ssl`) |

`--insecure` is not offered on `devnest network ssl`, and none is needed: inspecting a broken
certificate is what that command does, and it does so without disabling anything the user did not
ask about.

The `log` group shares less than the others, because a log command takes one file and reports on
it. What it does share:

| Flag | Default | Meaning |
|---|---|---|
| `--top <n>` | 10 | How many entries each ranked listing reports (`http`, `status`, `errors`, `stats`) |
| `--limit <n>`, `-n` | varies | How many entries to report (`top` uses 10, `search` uses 100) |

Two names for one idea is a wart, and it is deliberate. `--top` reads correctly on a command that
reports several rankings at once; `--limit` reads correctly on one that reports a single list and
where the count is a cap on output rather than a depth of ranking. Both are documented in the help
text of every command that has one.

Every `log` command takes exactly one file. There is no default of "the current directory", because
a directory is not a log, and guessing which file in it was meant is worse than saying so.

The `scan` group shares the walk options `--depth`, `--include-hidden`, `--follow-symlinks`,
`--no-ignore`, and `--exclude` (repeatable). `scan tree` is the exception the `file` group also has:
it walks the whole tree and uses `--depth` for how much of the result to draw, so it owns that flag
rather than sharing the walk-depth one.

The `env` group shares almost nothing, because its commands answer different questions about the
same machine. `--timeout` appears on the commands that run a program (`env`, `env list`, `env
which`) and bounds each probe.

The `encode` and `decode` groups share `--stdin`, which every command in them accepts. `--path`
appears on both `url` commands and means the same thing in each: a path segment rather than a query
value, so a space is `%20` and a plus sign is a plus sign.

The `json` and `yaml` groups share these:

| Flag | Default | Meaning |
|---|---|---|
| `--stdin` | off | Read the document from a pipe instead of a file |
| `--indent <n>` | 2 | Spaces per level (`json format`, `yaml to-json`) |

Every command in both groups takes one document, from a path or from `--stdin`, and never both.
Neither group has a command that writes to the file it read: the result goes to stdout, so a
redirect does the writing where the user can see it.

The `port` group shares `--tcp` and `--udp`, which mean the same thing everywhere and where
neither means both. `--all` appears only on `list`, because it exists to keep a listing readable
and a direct question about one port deserves a direct answer.

The `clean` group shares these:

| Flag | Default | Meaning |
|---|---|---|
| `--pattern <name>` | every rule | Restrict to a named rule; repeatable |
| `--protect <path>` | none | A path that is never removed; repeatable, added to the configured list |
| `--force` | off | Allow a run in a home directory or at a filesystem root |
| `--yes` | off | Answer the confirmation in advance |

`--apply` is on the group command only; `clean apply` is the same thing spelled as a verb. Neither
`--pattern` nor `--protect` can widen what the rule set allows, and `--force` lifts only the
protected-root refusal, never the marker requirement or the containment check.

## Supplying a secret

Commands in the `security` group that take a secret (`password-check`, and `hash` when the input
is sensitive) accept `--stdin`:

```
echo 'the password' | devnest security password-check --stdin
```

**Prefer it.** A value passed as an argument is written to your shell history and, while the
command runs, is readable from the process table by anything running as the same user. Passing a
secret that way should be treated as having disclosed it.

The argument form still works, because refusing it would be paternalistic and because it is what
people reach for first. It prints a warning to stderr naming `--stdin`, so the warning never
contaminates piped output.

Only the trailing newline is stripped from standard input. A password may legitimately contain
spaces, and `echo` adds a newline nobody means to include.

## Commands that change the disk

`devnest file organize` and `devnest file rename` are the only commands so far that can modify
anything. Both follow the same pattern:

- **Dry run is the default.** Without `--apply` they print what they would do and stop.
- **`--apply` performs the change**, after showing the plan and asking for confirmation.
- **`--yes` answers the confirmation in advance**, for unattended use.
- **`--force` permits running in a protected directory**: a filesystem root, a home directory, or
  a system directory. Nothing else lifts that guard, and configuration cannot.

When there is nothing to answer on (a pipeline, a CI step) an unanswered prompt would be a hang.
The command fails instead, with a message naming `--yes`.

## Naming discipline

**Groups are nouns, actions are verbs.** `devnest port free 3000` reads as a sentence. `devnest
free-port 3000` does not group with anything and produces a flat namespace that gets unusable at
thirty commands.

**Full words, no invented abbreviations.** `devnest config list`, never `devnest cfg ls`. The
saved characters are worth nothing next to shell history and tab completion; the cost is a
lookup every time someone is unsure which abbreviation this tool chose.

**Established verbs, used consistently.** Across the whole surface:

| Verb | Meaning, everywhere it appears |
|---|---|
| `list` | Enumerate, read-only |
| `get` | Retrieve one item |
| `set` | Change one value |
| `check` | Test a condition, exit code carries the answer |
| `verify` | Compare against an expectation, exit code carries the result |
| `scan` | Walk and analyse, read-only |
| `apply` | Perform the destructive action that was previewed |
| `free` | Release a held resource |
| `init` | Create something that did not exist |

A verb never means two different things in two different groups. If a new command needs a verb
whose meaning would conflict, it gets a different verb.

**Reserved words.** `install`, `update`, `upgrade`, `serve`, `daemon`, `login`, `auth` are
permanently unused. Each implies capability DevNest deliberately does not have, and an unrelated
command claiming one of those names would imply it does.

## Flag conventions

**Long form always.** Global short forms exist only for `-o` (`--output`), `-q` (`--quiet`), `-v`
(`--verbose`), `-h` (`--help`), `-y` (`--yes`). No new short forms without a strong argument;
short flags are a fixed namespace that fills up and then produces arbitrary assignments.

A command may add a short form of its own where the long name is one people type constantly and
the letter is unambiguous in that context: `-r` (`--recursive`) and `-e` (`--extension`) in the
`file` group, `-a` (`--algorithm`) in `security`, `-t` (`--type`) and `-p` (`--port`) in `network`,
`-n` (`--limit`) and `-i` (`--ignore-case`) in `log`. `-i` meaning "ignore case" is what every
search tool on the machine means by it, and a log search that spelled it differently would be
wrong more often than it was right. A test fails the build if any of these collides with a global
flag.

`env` and `scan` add no short forms. Their flags (`--shadows`, `--versions`, `--reveal`,
`--by-language`, `--no-ignore`) are typed rarely enough that a full word costs nothing.

**Positive booleans.** `--recursive`, `--follow-symlinks`, `--no-ignore`. Never a `--no-x` flag
whose default is true, because that produces double negatives in scripts and nobody reads them
correctly under pressure.

**One meaning per flag name.** `--force` means "override a safety check" in every command it
appears in. It never means "overwrite the output file" somewhere, because a user who learned it
in one place will be wrong in the other.

**Repeatable flags accumulate.** `--exclude a --exclude b` excludes both. Repeatable flags are
documented as such in help text.

**Values with units carry them.** `--timeout 30s`, `--max-size 10MB`, `--older-than 7d`. A bare
number whose unit lives in the documentation is a bug waiting to be filed. Sizes accept `512`,
`10KB`, `1.5GB`, or `2g`, and the units are binary, matching what filesystems report and what every
other disk tool on the machine shows.

**Global flags are declared once**, on the root command, and work identically everywhere. A
command that redefines a global flag is a review blocker.

## Global flags

Declared once on the root command and registered on every command, so a flag means the same thing
wherever it appears.

| Flag | Default | Meaning |
|---|---|---|
| `-o, --output <format>` | `table` | Output format |
| `--config <path>` | platform default | Configuration file to use |
| `-q, --quiet` | off | Suppress all non-error output |
| `-v, --verbose` | off | Debug-level logging to stderr |
| `--no-color` | auto | Disable colour; `NO_COLOR` is honoured too |
| `--log-format <format>` | follows `--output` | `text` or `json` |
| `--log-timestamps` | off | Include timestamps in text logs |
| `-h, --help` | none | Help for the current command |
| `--version` | none | Version and build information |

Four flags described elsewhere in this documentation are not implemented yet: `--export` and
`--export-format` (see `export-system.md`), `--dry-run` and `--yes` (see `design.md`), and
`--compact`. Each arrives with the first command that has something to export, delete, or confirm.
A global flag that silently does nothing is worse than one that does not exist.

Both `-flag value` and `-flag=value` work, and either one or two leading dashes. Combined short
flags (`-qv`) are not supported.

## Output selection

`--output` takes `table`, `json`, or `csv`, and defaults to `table`. `markdown` is described in
`export-system.md` and lands with the export flag; asking for it now is a usage error that names
the supported formats.

`csv` arrived in Phase 5 with the `log` group, the first whose results are genuinely rows. It is
available to any command that supplies a row view of its result, and refused, with a message
saying so, by any command that does not. That refusal is the point: a command whose result is a
handful of named values has no honest CSV form, and inventing one produces a file somebody writes
a script against.

A CSV file carries the header and the rows and nothing else. No envelope, no metadata, no warning
lines. A CSV with a preamble is not a CSV any tool will read, so the metadata stays in `--output
json` where it belongs, and warnings go to stderr as usual.

Auto-detection covers colour only: when stdout is not a terminal, colour is disabled. The *format*
is never auto-detected. A command that silently switched to JSON when piped would break every
script written against its table output the first time someone ran it interactively during
development.

Commands whose data cannot be represented in a requested format say so and exit 2. A deeply
nested result asked for `--output csv` produces an error naming the problem, never a lossy
approximation.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | General failure |
| 2 | Usage error |
| 3 | Not found |
| 4 | Permission denied |
| 5 | Cancelled |

Check-style commands use the exit code as their answer. Live as of Phase 7:

| Command | Exits non-zero when |
|---|---|
| `network monitor` | The site is down, or slower than `--max-response` |
| `network ping` | The host never answered |
| `network ssl` | The certificate is expired, not yet valid, or untrusted |
| `network latency` | Every attempt failed |
| `network dns` | No records were found at all |
| `security checksum` | The file does not match the expected digest |
| `security password-check` | The score is below `--min-score`, when that is set |
| `log search` | The keyword appears nowhere in the file (exit 3, not found) |
| `env which` | The tool name resolves to nothing on PATH (exit 3, not found) |
| `json query` | The expression selects nothing (exit 3, not found) |
| `json`, `yaml` | The document does not parse (exit 1, with the line and column) |
| `port check` | The port is in use (exit 3, not found) |

Still planned: `hash verify` exits 1 on mismatch. Each such command documents its exit codes in its
own help text, since that is where someone writing a script will look.

The distinction that makes this work: a site being down is a *result*, so the command succeeds and
the exit code carries the answer. Only a failure to ask the question, such as an unusable URL or a
cancelled run, is an error in the usual sense.

The mapping is covered by tests at two levels: in-process against the error classifier, and
end-to-end against the built binary in `tests/e2e_test.go`.

## Help text

Three levels, each with a job.

**`devnest --help`**: one line per group, fits on one screen, plus the global flags and a
pointer at `devnest <group> --help`. It is an index, not a manual.

**`devnest <group> --help`**: what the group covers, its actions with one line each, and flags
shared across the group.

**`devnest <group> <action> --help`**: full detail: description, usage line, every flag with its
default, exit codes if the command uses them meaningfully, and **at least two realistic
examples**. Examples are the part people actually read, and "realistic" means a plausible path
and plausible flags, not `<input>` placeholders.

`TestEveryRunnableCommandIsDocumented` in `internal/cli` walks the whole tree and fails the build
if a runnable command is missing its description, usage line, or two examples, or if an example
is not a full `devnest ...` invocation.

Two more tests guard the flag surface, both added after the mistakes they catch reached a running
binary: `TestEveryCommandBuildsItsFlagSet` builds every command's flag set, because registering a
name twice panics only when that command runs, and `TestCommandFlagsDoNotShadowGlobalOnes` fails
if a command claims a name the global flags already own.

## Interaction

**Never prompt when a flag would do.** Every confirmation has a flag that answers it in advance,
so nothing is unscriptable.

**Never prompt when stdin is not a terminal.** In that situation a prompt is a hang. The command
fails immediately with a message naming the flag to pass.

**`--yes` answers everything.** One flag for unattended use, rather than a different
acknowledgement flag per command.

**Confirmations state the consequence.** "Delete 1,247 files (2.3 GB) under C:\projects\api?"
gives counts, size, and the resolved root path, so the answer is informed.

## Piping and streams

- **stdout carries results only.** Never a log line, never a progress indicator, at any verbosity.
- **stderr carries everything else**: logs, progress, prompts, diagnostics.
- **stdin is read** by commands that accept it, when no positional argument was given and stdin is
  not a terminal. When stdin is a terminal and no argument was supplied, the command prints usage
  instead of blocking on a read that will never complete.

These three together mean `devnest scan --output json | jq '.data.totalBytes'` behaves identically
with and without `--verbose`.

## Compatibility

Within a major version, none of the following change: command names, action names, flag names,
flag semantics, JSON field names, exit code meanings.

Additive changes are fine: new commands, new flags with defaults preserving existing behaviour,
new JSON fields.

A breaking change requires a major version and a migration note in the changelog. Deprecation runs
one minor release ahead of removal: the old form keeps working and prints a warning to stderr
naming its replacement.
