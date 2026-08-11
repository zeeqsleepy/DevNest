# Changelog

Notable changes to DevNest, newest first.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versioning follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html), with the public surface defined
precisely in [docs/release-process.md](docs/release-process.md).

Entries describe what changed for a user, not which files were edited.

## [Unreleased]

## [0.5.0] - 2026-08-11

A minor rather than a patch: a command, two flags, and a whole JSON surface were added, and
`docs/release-process.md` counts command names, flag names, and JSON fields as public surface.
Everything in 0.4.0 behaves exactly as it did.

### Added: network port scanning

- `devnest network scan <host>`: find which TCP ports a host is listening on, in
  parallel, with `--ports` for a list or range and `--concurrency` to bound how
  many probes run at once.

This is a connect scan, not a SYN scan: every port gets a normal TCP connection
attempt, a success means the service accepted the connection, and nothing is
sent that needs a raw socket or administrator rights. DevNest never asks for
elevation, the same decision that makes `network ping` TCP rather than ICMP.

Three outcomes are reported. A port that accepts the connection is **open**, a
port that refuses it is **closed**, and a port that stays silent until the probe
timeout is **filtered** — which is how a host that drops packets differs from
one that rejects them. The three counts always add up to the total, because
every port is probed exactly once.

The service name next to an open port comes from a static registry of
well-known ports, never from connecting to the service to find out, so it is a
hint rather than a detection: an FTP server moved to port 7000 is still reported
as having port `7000` open, with no made-up service.

Probes run in parallel under a worker pool that is bounded by default and capped
(100, at most 512), because a sweep that opens thousands of sockets at once
looks like an attack to the machine it is pointed at. A host that cannot be
resolved exits 3; otherwise a scan that completes is a successful run whatever
it found, because finding nothing open is the answer, not a failure.

## [0.4.0] - 2026-07-28

A minor rather than a patch: two flags and two JSON counters were added, and
`docs/release-process.md` counts flag names and JSON fields as public surface. A scan without
`--baseline` behaves exactly as it did in 0.3.1.

### Added: secret scanning baselines

- `devnest secret scan --baseline <path>`: accept the findings recorded in a file, and report only
  what is new. Accepted findings leave the table, the counts, and the exit code, so `--fail-on`
  gates on what arrived since.
- `devnest secret scan --baseline <path> --update-baseline`: write everything this scan found to
  that file. This run never fails the gate, and it still prints what it accepted.

This is what lets an old repository start scanning. Four hundred historical candidates fail every
run, and a check that always fails is a check somebody turns off in a week; a baseline draws the
line at today.

An entry is a path, a rule, and the redacted excerpt. **Never a line number**: a finding that moved
down a file when somebody added an import is the same finding, and a baseline that forgets what it
accepted on every edit is one nobody keeps. **Never the value**: the excerpt is the same four
characters and length the report shows, because the scanner does not hold the credential in the
first place, which is what makes the file safe to commit.

Two counters say what the file did. `baselined` is how many findings it accepted; `baselineStale`
is how many of its entries matched nothing this run — a credential that was dealt with, or a file
that moved. A baseline nobody prunes eventually accepts things that are not there.

The path is asked for rather than assumed: this file gets committed, and a magic filename appearing
in somebody's repository is not a decision this tool should make. `secret history` has no baseline,
because that scan is an audit of what was committed and the answer to a credential in the history
is rotating it.

## [0.3.1] - 2026-07-28

A patch: help text only. No command, flag, field, or exit code changed.

### Fixed: the usage line for `security checksum` names both forms

`devnest security checksum --help` described `--check` in its text and its examples but its usage
line still read `<file> <hash>`, so the second form was invisible to anybody skimming for the shape
of the command. It now reads `<file> <hash> | --check <checksum-file> [name...]`.

## [0.3.0] - 2026-07-28

A minor rather than a patch: a flag was added, and `docs/release-process.md` counts flag names as
public surface. Nothing was removed or narrowed, so an 0.2.0 command line still means what it did.

This is also the first release to reach both package channels without anyone copying a file across
by hand — or the first to prove it cannot, which is the other reason to cut it.

### Added: verifying a whole checksum file

- `devnest security checksum --check <file>`: verify against a published `SHA256SUMS` rather than
  one pasted digest, in the format every `*sum` tool writes. Names in it are read relative to the
  checksum file, so a directory of downloads is one command.
- Name files after the flag to check only those. A name the checksum file does not cover is an
  error rather than an empty result, because answering the question you asked with silence reads as
  a pass.

Each line carries its own digest, so a file mixing algorithms needs nothing said about it, and
`--algorithm` becomes an assertion applied to every line instead of a thing to remember.

A listed file that is not on disk is reported as missing rather than failed. A release publishes a
digest for every platform it built and nobody downloads six of them, so counting the five absent
ones as failures would make the flag useless for the one case it exists for. Nothing is given up:
a file that is present and wrong is still a mismatch, and a run that verified nothing at all exits
non-zero rather than reporting a clean sweep of nothing.

An entry naming an absolute path, or one climbing out of the checksum file's own directory, is
refused. A checksum file arrives from the same page as the download it vouches for, so it is
exactly as trustworthy as the thing it is there to check.

This existed on the roadmap as an after-1.0 direction. It moved up because a flag only adds
surface, so the compatibility freeze was never what it was waiting for, and because Windows has no
`sha256sum -c`: CertUtil hashes one file and leaves the comparing to the reader.

## [0.2.0] - 2026-07-25

A minor version rather than a patch: a configuration key was removed and a flag's meaning was
narrowed, and `docs/release-process.md` counts both as public surface. Pre-1.0 still carries no
compatibility promise, but the version number should say what happened rather than hide it.

The Homebrew tap and the winget fork are now updated by the release itself, so this is the first
version that reaches a package manager without anyone copying a file across by hand. The pull
request to `microsoft/winget-pkgs` is still opened by a person.

### Fixed: the configured entropy floor is applied, and applies where it should

`secret.entropy_threshold` was read from the configuration file and then used by nothing. The code
meant to apply it assigned zero to a value that was already zero, so the only way to move a floor
was `--entropy` on every run.

It now works, and it moves only the rules that match by shape rather than by a provider's prefix.
That is the difference between a tuning knob and an off switch: raising the floor is how somebody
quietens a report full of guesses, and under the old reading a threshold of 4.5 in a configuration
file would have stopped `AKIA...` and `ghp_...` from being reported at all, silently, on every run
after it was set. A value with the shape of an AWS key identifier is one whatever it scores.

`--entropy` has the same meaning and overrides the configured value for one run.

The compiled default is now `0`, meaning every rule keeps the floor it was written with. It was
`4.5`, which was never applied to anything; applying it as written would have been stricter than
the rules themselves and would have dropped candidates on machines whose owners never configured
anything. Scanning this repository at `4.5` reports nothing and at each rule's own floor reports
one candidate, which is the whole argument in one line.

### Removed: the `secret.custom_rules` configuration key

The key was loaded and read by nothing. Somebody adding their organisation's token format to it got
a scan that reported nothing and a reasonable belief that the tree was clean, which is the one
failure mode a credential scanner must not have.

It was in the wrong place to begin with. A rule is a name, a severity, an entropy floor, and a
pattern with a capture group naming the part that is the credential; the configuration file holds
flat sections of scalars and string lists and rejects arrays of tables on purpose. A bare list of
patterns would have produced findings with no name to select or exclude them by, one fixed
severity, and no floor of their own.

An existing configuration file that sets the key keeps working: an unknown key is a warning, never
an error. `devnest secret rules` remains the whole surface of what a scan can find.

### Changed: the licence is now MIT

DevNest is MIT licensed, replacing the Apache License 2.0 with the Commons Clause that 0.1.0 went
out under. Use it, change it, redistribute it, sell it, put it in a paid product: keep the copyright
notice and there are no other conditions.

The Commons Clause withheld the right to sell, which made DevNest source-available rather than open
source under the OSI definition. That restriction bought nothing. It kept the project out of
Homebrew core and the Linux distribution repositories, and out of reach of anyone who will only
contribute to something open source, in exchange for protecting a commercial plan that does not
exist.

A copy taken under the old terms keeps them. Everything from here is MIT.

### Changed: credential scanning skips generated build output

`.next`, `.nuxt`, `.svelte-kit`, `.output`, `.angular`, `.parcel-cache`, `.turbo`, `.cache`,
`coverage`, `.gradle`, and `.dart_tool` join the directories a scan does not descend into.

Scanning one ordinary Next.js project produced 291 candidates: one real, two false positives from a
CI workflow, and **288 from inside `.next`** — every one a placeholder shipped inside a bundled
dependency. A report that long is one nobody reads to the end, which is how the finding at the top
of it gets missed.

## [0.1.0] - 2026-07-25

The first release. Every command group described in `docs/commands.md` is implemented and tested on
Windows, Linux, and macOS: files, networking, security, logs, environment, project scanning,
encoding, JSON and YAML, ports, cleanup, git, credential scanning, configuration, self-check,
completion, and export.

**Pre-1.0 means no compatibility promise.** Command names, flag names, JSON field names, and exit
codes can still change. The promise starts at 1.0, which is not declared until the surface has been
used enough to be confident about it.

### Added: managing the configuration

- `devnest config`: every value with the layer it came from — default, file, or environment. That
  column is the fastest answer to "why is it behaving like that".
- `devnest config list`, `config get <key>`, `config path`, `config validate`: read what is set.
- `devnest config set <key> <value>`, `config unset <key>`, `config init`: change it without
  opening the file.

Editing preserves the file. Setting one key rewrites one line and leaves the comments and layout
exactly as they were, because a configuration file is hand-written and a tool that reformats it to
change one value is a tool people stop using. A value the schema rejects is refused before anything
is written, so the command that fixes a broken configuration cannot create one. `config init` never
overwrites an existing file.

### Fixed: documentation examples

Every `devnest` line in the documentation is now checked against the real binary by a test, which
found `devnest hash` and `devnest http` (renamed to `devnest file hash` and `devnest network http`
in earlier phases), a `--skip-unreadable` flag that never existed, and the whole `config` group,
which the documentation had described since the first commit.

Help text now lists a command's own flags. It had only ever shown the global ones, so `--method`,
`--fail-on`, and every other flag existed in the documentation and nowhere the user could see them.

### Added: exporting results to a file

- `--export <path>` on every command: write the result to a file as well as to the terminal. The
  format follows the extension (`.json`, `.csv`, `.md`, `.txt`), or `--export-format` says it.
- `--output markdown`, and with it a report meant for pasting into a ticket: headings, a summary
  table, and the detail below, with the numbers formatted for reading.
- `devnest export <command...>`: run several commands and write one combined document. A command
  with a space in it is one argument, so `devnest export "secret scan" scan` runs two. A failure
  does not stop the rest, and the exit code is the worst of the individual ones.

Exporting never replaces terminal output. Somebody who exports a scan usually also wants to watch
it, and a command that went quiet because a file was requested is a command people run twice.

The file is written by rendering beside the target and renaming over it, so an interrupted run
leaves the previous report or the new one, never a truncated file that still looks valid.

The markdown renderer is driven by the same JSON the machine format emits rather than by a view
written per command, which is what stops a report and a terminal run from describing the same
result differently. The JSON field-name conventions are what make that possible: a field ending in
`Bytes` is a size, one ending in `Ms` is a duration, and a list of flat objects is a table.

### Added: self-check

- `devnest doctor`: check this installation. Whether the configuration file parses and holds values
  DevNest accepts, whether the directory it lives in can be written to, whether the rule tables are
  compiled in, whether git is present, and what the terminal was detected as.

The report is written to be pasted into an issue: paths under the home directory are shortened to
`~` and the hostname is not reported at all. Neither helps anyone reading a stack of bug reports,
and both identify whoever filed one.

A warning is something absent that DevNest works without — git on a machine that never runs the
git commands is the ordinary case — and does not affect the exit code. A failed check exits 1,
after the report is printed.

`doctor` is the one command that still starts when the configuration file will not load, with the
compiled defaults in its place. Everything else refuses, which is why the command that diagnoses
that file must not.

### Added: shell completion

- `devnest completion powershell|bash|zsh|fish`: print a completion script to stdout, for you to
  redirect wherever your shell keeps them. Nothing is installed and no file is written; the
  instructions for each shell are in [docs/installation.md](docs/installation.md).

Completion covers the command tree and the flags, and falls back to file names where a command
takes a path. The tree is baked into the script rather than fetched by calling the binary back on
every keystroke, so completion stays instant and keeps working when the binary is busy or under a
different name. The trade is that a script is only as current as the version that printed it:
regenerate it after an upgrade.

`--output json` returns the same script as a field, which is what a packaging step wants.

### Added: credential scanning

- `devnest secret scan [path]`: search a working tree for credential-shaped strings. Sixteen rules:
  provider prefixes that mean one thing (`AKIA`, `ghp_`, `sk_live_`, `AIza`, `xox`), private key
  headers, passwords inside connection strings, and two generic rules for a value assigned to
  something named like a secret.
- `devnest secret history [path]`: the same over a repository's history, where a credential deleted
  two years ago is still committed and still leaked. Added lines only; one credential added,
  reverted, and re-added is reported once. The last 500 commits by default, `--all` for the lot.
- `devnest secret rules`: every detector with its severity and its entropy floor. This is the whole
  surface of what a scan can find, which is worth reading before trusting a clean result.
- `devnest secret test <string>`: which rules a value matches and what it scored, for tuning. The
  value is never echoed back, because somebody testing a scanner is testing real credentials as
  often as not.

**A matched value is never printed in full**, and the guarantee is structural rather than careful:
a finding has no field holding it. Four characters and a length is all that exists downstream, so
no renderer, export, or verbosity setting can leak one. A test serialises a whole result to JSON
and fails if a credential appears anywhere in it.

Every rule carries an entropy floor, including the provider-prefix ones, which is what keeps
`sk_live_XXXXXXXXXXXX` out of a report. Fixture directories, lock files, and dependency
directories are skipped by default, and a `devnest:allow-secret` comment silences one line. What
comes back is described as candidates, in those words, because a scanner people have learned to
ignore finds nothing at all.

`--fail-on <severity>` turns a scan into a gate: with it, findings at or above that level exit
non-zero, which is what a pre-commit hook or a CI step needs. Without it, finding something is
still a successful run.

### Added: repository inspection

- `devnest git [path]`: what a repository is. Branch, HEAD, remotes, commit and branch and tag
  counts, the state of the working tree, and how old and how idle the history is. A detached HEAD
  is named as one, and a repository with no commits is described rather than reported as an error.
- `devnest git branches [path]`: local branches, most recent activity first, with who touched each
  one last, how long ago, and whether it has ever been pushed. Anything past the staleness window
  is flagged and the count is in the result, so one listing answers both questions.
- `devnest git stale [path]`: the branches nobody has touched for `--days`, default 90. The branch
  you are standing on is never listed. `--print-commands` adds the git commands that would delete
  them, printed for you to review and run yourself; they use `branch -d` rather than `-D`, so a
  command copied without reading it still refuses to throw away unmerged work.
- `devnest git contributors [path]`: commit counts, shares, and first and last activity by author,
  with `--since` to narrow the window. People are identified by email address folded to lower case,
  because names are spelled several ways in every repository of any age.
- `devnest git large [path]`: the biggest objects anywhere in the history, which is the answer to
  "why does cloning this take ten minutes". A file deleted two years ago still costs every clone
  what it weighed and appears in no listing of the working tree.

Every command in the group is read-only, and a test asserts it: the fake git records each
invocation and fails the build if any of them is a subcommand that can write. The commands ask git
for machine formats rather than parsing the human ones, and pass `-c color.ui=false --no-pager` so
that a user's own configuration cannot change what gets parsed.

### Added: ports and cleanup

- `devnest port list`: every listening socket with the process that owns it, and how reachable each
  one is: on all interfaces, or only from this machine. Sockets whose owner the system will not
  name are listed as unknown rather than dropped, and the count of them is reported, because a
  listing that quietly omits what it could not attribute is not an answer. Ports below 1024 are
  hidden unless `--all` is passed, and the number hidden is part of the result.
- `devnest port check <port>`: whether a port is taken, and by what. Exit 0 when it is free and 3
  when it is in use, so a script can branch without parsing anything.
- `devnest port free <port>`: end the process holding a port. It names the process before asking,
  asks the process to exit rather than killing it, and kills only with `--force`. A port held by
  more than one process is refused instead of guessed at; pid 0 and pid 1 are refused
  unconditionally; and the pid is re-verified against the port in the moment before anything is
  signalled, because pids are reused. On Windows `--force` is required and the command says why:
  Windows offers no way for one process to ask another to exit politely, and presenting a kill as a
  request would be a lie.
- `devnest clean [path]`: find the directories a build regenerates and report what they cost.
  **Nothing is deleted without `--apply`.** A directory is a candidate only when its name is in the
  rule set, and generic names need evidence beside them: `build` counts next to a `package.json` or
  a `Cargo.toml`, and not in a directory of photographs that happens to have one. Size, age, and
  emptiness are never evidence.
- `devnest clean apply [path]`: the same, spelled as a verb. The plan is shown and confirmed first,
  every candidate is re-checked against every guard in the moment before it is removed, and a
  directory that cannot be removed is reported while the rest continue.
- `devnest clean rules`: the whole surface of what clean can ever remove, with what each rule needs
  beside it and what regenerating it costs. Worth reading before the first `--apply`.

Guards on `clean`, each with a test asserting the refusal rather than the success: symbolic links
are never followed or removed, version control directories are never entered, nothing outside the
scan root or on another filesystem is touched, and a run at a filesystem root or in a home
directory is refused unless `--force` is passed on the command line, which no configuration file
can do. `vendor` is deliberately not a rule: it is checked in on purpose in plenty of repositories.

Three platform packages grew to support this. Socket enumeration is three implementations behind
one surface: the IP Helper API through `syscall` on Windows, `/proc/net` with inode-to-pid
resolution on Linux, and `lsof` on macOS, where the alternative was cgo and DevNest builds without
it so releases stay static. Process naming and termination are three more, and `platform/fs` gained
`RemoveAll` and `DeviceID`.

### Added: data and encoding tools

- `devnest encode hex` and `devnest decode hex`: hex in both directions. Decoding accepts either
  case, a leading `0x`, and the separators a value picks up in a packet dump or a wrapped email.
  Bytes that are not printable text come back as Base64 rather than being written to a terminal,
  because arbitrary bytes can carry escape sequences that change how the terminal behaves.
- `devnest encode url` and `devnest decode url`: percent-encoding, with `--path` for a path segment
  and the query form by default. The two differ over the plus sign, and each result says which was
  used, because decoding a path value the wrong way silently turns a plus in a filename into a
  space.
- `devnest decode jwt`: the header, the payload, and the registered claims of a JSON Web Token,
  with expiry judged and reported. The signature is **never** verified and the result carries a
  field saying so. An expired token warns and still exits 0: the expiry is a fact about the input,
  not a failure of the command. `alg=none` warns too. Nothing is transmitted anywhere, which is
  the whole point: a token pasted into a web page has been handed to whoever runs the page.
- `devnest json <file>`: validate, and report the shape, size, and top-level entry count. A
  document that does not parse fails with the line, the column, and the offending line quoted,
  because "invalid JSON" tells the user what they already knew.
- `devnest json format`: reprint at one indentation width, `--indent` to choose it. The document is
  reprinted from its own bytes, so key order and number precision survive and only the whitespace
  changes.
- `devnest json minify`: strip the whitespace. Standard output carries the document and nothing
  else; how much it saved is in `--output json`, so a redirect gets a clean file.
- `devnest json query <file> <expression>`: select a subtree. Keys separated by dots, array
  elements by `[n]`, awkward keys in `["quoted brackets"]`, and a leading `.` or `$` optional.
  `--raw` prints a selected string without its quotes. Selecting nothing exits 3, so a script can
  branch on whether a key exists.
- `devnest json to-yaml`: convert to YAML with the key order intact.
- `devnest json to-csv`: convert an array of objects to CSV, with the columns being the union of
  the keys across every record so a missing key is an empty cell rather than a shifted row. A
  nested value is reported, naming the record and the key, rather than stringified into a cell;
  `--flatten` spreads it across columns named with a dot.
- `devnest yaml <file>`: validate, including multi-document files, and report the document count.
- `devnest yaml to-json`: convert to JSON, `--indent` to choose the width. Several documents become
  a JSON array and anchors are resolved on the way through. There is deliberately no `yaml format`:
  re-emitting YAML deletes every comment in the file.

Every command in these four groups reads standard input with `--stdin`, and none of them writes to
the file it read.

### Changed

- **The licence is now Apache 2.0 with the Commons Clause**, replacing MIT. Apache 2.0 applies in
  full, patent grant included, with one right removed: selling the software, meaning providing
  DevNest to others for a fee as a product or service whose value comes substantially from what
  DevNest does. Repackaging and charging for it, hosting it as a paid service, and charging for
  support of it are all out. Using it at work, inside a company that makes money, or in the build
  pipeline of a paid product is not: it is the tool being sold that is restricted, never the work
  done with it. This makes DevNest source-available rather than open source in the OSI sense, and
  the documentation says so plainly rather than blurring the term.
- **DevNest now has one dependency**: `github.com/goccy/go-yaml` (MIT, no transitive dependencies),
  for YAML parsing. Hand-writing a YAML parser is not a reasonable use of anyone's time. CI now
  pins the entire module graph to an allow list, so a second dependency has to be a decision rather
  than something that arrives with an upgrade.
- The `json` and `yaml` commands hold a whole document in memory, unlike the streaming `log`
  commands, because formatting, querying, and converting all need the shape of the tree before they
  can act. Documents past 64 MiB are refused with a message naming the limit rather than being
  discovered as an out-of-memory kill.

### Added: environment and project tools

- `devnest env`: a machine summary. Operating system, architecture, CPU count, shell, terminal,
  detected toolchains grouped by kind, and the state of PATH. The command to run on a machine that
  is not yours, or on your own after something stopped working.
- `devnest env list`: every installed toolchain with its version and the location that runs.
  Detection is a table, so adding a toolchain is one line. A tool is looked up on PATH first and
  only run if it is there; probes run in a bounded pool with a timeout on each, without a shell.
  A tool that is absent, or present but mute about its version, is reported as such rather than
  failing the run.
- `devnest env path`: PATH entries in order, flagging duplicates, dead directories, and entries
  that point at a file. `--shadows` finds executables resolvable from more than one entry, which
  is the cause of most "but I installed the new version" reports.
- `devnest env which <tool>`: every place a name resolves to, not just the winner, with
  `--versions` to run each copy. Exits 3 when the name resolves to nothing.
- `devnest env vars [pattern]`: development-relevant environment variables, filtered by an optional
  name pattern. Values whose name looks like a credential are hidden, in the result itself rather
  than in one rendering of it, and shown as a length rather than a prefix. `--reveal` prints them
  in full and warns that it did.
- `devnest scan`: what a project tree is made of. Files, directories, size, depth, a breakdown by
  category, the top languages and extensions, and the authored share with vendored and generated
  files left out. The walk skips `.gitignore` rules, the vendor and build directories every
  ecosystem has, and always `.git`, so a small Node project is not reported as four hundred
  thousand files. `--no-ignore` turns that off.
- `devnest scan types`: the file-type breakdown, by extension or, with `--by-language`, folding
  `.js`, `.mjs`, and `.jsx` into one row.
- `devnest scan lines`: code, comment, and blank lines by language. Only files in a recognised
  language are opened, and each streams through a reused buffer.
- `devnest scan tree`: the directory shape, with the file count and size of everything under each
  branch including the levels not shown.

Two platform packages arrived with these: `platform/proc` runs a program under a timeout and
locates one on PATH, with the operating-system differences (execute bit versus PATHEXT, name versus
stem) confined to its build-tag files; `platform/sys` describes the machine and the environment
without running anything. A new leaf package, `internal/classify`, holds the file category and
language rules, shared below the module layer because `clean` and `secret` will need them too.

### Added: log tools

- `devnest log analyze`: size, total lines, blank lines, the detected format, and how long the
  read took. The format is detected from the first two hundred non-blank lines and the result says
  how many it sampled, so the guess can be judged rather than taken on faith.
- `devnest log http`: a summary of an access log in the Common or Combined Log Format. Requests,
  methods, status classes and codes, the busiest endpoints, the loudest clients, and response
  sizes. Query strings are stripped before endpoints are counted, so one busy search page is one
  entry rather than thousands.
- `devnest log errors`: failures grouped by a normalised form of their message, so "user 4821 not
  found" and "user 9930 not found" are one finding seen twice, reported with its first and last
  line number. Counted by severity and by category. A 5xx in an access log is a finding too.
  Warnings are counted but listed only with `--warnings`.
- `devnest log status`: the five status families, always all five including the empty ones, plus
  the most common individual codes and the 4xx and 5xx total.
- `devnest log top`: the most requested endpoints, or the busiest clients with `--clients`.
- `devnest log search`: every line containing a keyword, with its line number. Case-sensitive by
  default, `-i` to fold. Exits 3 when the keyword appears nowhere, so a script can branch on it.
  The whole file is read even when the listing is capped, so the match count is the real one.
- `devnest log stats`: line counts, average length, the longest and shortest line, and the longest
  lines with their line numbers. This is the command for "why is this file eight gigabytes".
- `--output csv`, on any command whose result is genuinely rows. All seven log commands are. A
  command whose result is a handful of named values says it has no CSV form rather than inventing
  one, and a CSV file carries the header and the rows and nothing else.

Everything in this group streams. A log file is read once through a buffer that is reused for
every line, so a four gigabyte log costs the same memory as a four kilobyte one, and nothing the
commands return grows with the size of the file. Lines that do not fit the expected format are
counted as unparsed and the run continues; every result reports how many, so the rest can be
judged. A binary file is refused with a message saying why.

Measured on a generated 200,000 line access log of about 20 MB: `analyze` 7.8 ms, `stats` 9.6 ms,
`search` 17.8 ms, `errors` 44.2 ms, `http` 57.1 ms. Full figures in `docs/performance.md`.

### Added: security tools

- `devnest security password`: generate passwords from the operating system's cryptographic
  random source. Configurable length, character classes, custom or excluded characters,
  `--exclude-ambiguous` for characters that are easy to misread, and `--require-each` to guarantee
  one from every class. There is no seed and no way to reproduce a result.
- `devnest security password-check`: score a password and report exactly what is wrong with it:
  length, character variety, repeated runs, repeated blocks, sequences, keyboard walks, and matches
  against the passwords at the top of every breach corpus. Exits non-zero below `--min-score`.
- `devnest security hash`: SHA-256, SHA-512, or MD5 over text, a file, or standard input. Several
  algorithms come from one pass over the input.
- `devnest security checksum`: verify a file against a published digest. The algorithm is inferred
  from the digest's length. Exits non-zero on a mismatch.
- `devnest security encode` / `decode`: Base64, both alphabets, padding optional. Decode accepts
  any shape and ignores whitespace from a value wrapped across lines.
- A `[security]` configuration section for password defaults. It holds the shape of a password,
  never a password.

### Privacy and secret handling

- **A password given to the strength checker never appears in the result**, not the password, not
  a substring, not a quoted example of the weak pattern found. Findings are fixed strings, because
  a result is serialised, exported, and pasted into tickets.
- **The security module has no logger**, which is the surest way to guarantee a password never
  reaches one.
- **Nothing is written to a file.** A generated password goes to standard output and nowhere else.
- **Commands taking a secret accept `--stdin`** and warn when the argument form is used: an
  argument is written to shell history and is readable from the process table.
- **Randomness is a parameter**, satisfied by `crypto/rand`. There is no path to `math/rand`.
- **Decoded bytes that are not printable text are shown as hex**, because arbitrary bytes can carry
  escape sequences that change how a terminal behaves and a decode command is where untrusted bytes
  arrive. `--raw` overrides it.
- A dictionary match **caps** the strength score rather than only subtracting from it: a known base
  is cracked in milliseconds however much nominal entropy the character arithmetic reports.

### Added: network tools

- `devnest network monitor`: check whether a site is up, how quickly it answers, and what status
  it returns. `--expect-status` and `--max-response` set what counts as healthy. Exits 1 when the
  site is not, so it works as a cron entry without anyone parsing the output.
- `devnest network http`: send one request and report the status, a timing breakdown (DNS,
  connect, TLS, first byte, total), both sets of headers, the redirect chain with the status that
  caused each hop, and the TLS session.
- `devnest network latency`: repeated measurements reduced to minimum, average, median, maximum,
  and standard deviation. Connections are never reused, so the setup cost is measured every time
  rather than only on the first attempt.
- `devnest network ping`: reachability and timing. **This opens a TCP connection rather than
  sending ICMP**, because ICMP needs a raw socket and therefore administrator rights, and DevNest
  never asks for elevation. Every result reports `method: "tcp"`.
- `devnest network dns`: A, AAAA, CNAME, MX, TXT, and NS records. A type with no answers is
  reported as such rather than failing: a domain with no MX record is an ordinary domain.
- `devnest network ssl`: certificate issuer, subject, validity window, days remaining, trust
  status, TLS version, and cipher. An expired or untrusted certificate is a result, not an error.
- Duration flags accept a unit: `--timeout 5s`, `--max-response 500ms`, `--interval 200ms`.

### Changed

- The `[http]` configuration section is now `[network]`, and gains `attempts` and `interval_ms`.
  Its timeout bounds a DNS lookup and a TLS handshake as well as a request, so naming it for HTTP
  was a small lie in a file people hand-edit. Nothing has been released, so this breaks nothing.
- `--insecure` is a single flag with a warning printed on every use, rather than the two flags an
  earlier draft of `docs/security.md` called for. Two flags for one decision is friction people
  alias past, and habituation is the actual risk. The command that genuinely needs to inspect a
  broken certificate (`devnest network ssl`) needs no flag at all.

### Network safety

- Only the `network` group opens a socket. Nothing else in DevNest does.
- Every operation is bounded by a timeout. There is no unbounded default anywhere.
- `Authorization`, `Cookie`, and `Proxy-Authorization` are dropped on a cross-origin redirect.
- Credential-shaped headers are masked in the result, so the mask applies to every output format
  rather than to one of them.
- Only `http` and `https` are accepted; `file://` and friends are rejected before anything is sent.
- Response bodies are size-capped, and the remainder is drained so the exchange still completes.
- The proxy configured in the environment is honoured.

### Added: file tools

- `devnest file organize`: group files into category folders (`Images/jpg`, `Documents/pdf`) or
  flat extension folders. Dry run by default; `--apply` performs the moves after showing the plan
  and asking. An existing file is never replaced: `--on-conflict` chooses between skipping,
  numbering the new arrival, or refusing the whole operation.
- `devnest file duplicate`: find files with identical content. Groups by size first and hashes
  only the candidates, so most files are never opened. Reports the oldest copy as the original and
  how much space the duplicates cost. Deletes nothing.
- `devnest file rename`: batch rename with prefixes, suffixes, literal replacements, running
  numbers, and case changes. Preview by default. The whole plan is checked first, and any naming
  conflict refuses the batch with nothing changed. The result is a rollback record: run with
  `--output json` and keep the file.
- `devnest file filter`: search by extension, category, name glob, or size range, with sorting
  and a limit. `--categories` lists the category table.
- `devnest file size`: where the disk space went: largest directories with their share of the
  total, and the largest files. Always measures the whole tree; `--depth` controls the report.
- `devnest file hash`: SHA-256, SHA-512, and MD5, computed from a single pass over each file.
- `devnest <group> help` now works as a synonym for `devnest <group> --help`.
- Table output, byte and count formatting, and size flags that accept units (`10MB`, `1.5GB`).
- A confirmation prompt for commands that change the disk, answerable in advance with `--yes`.
  Where there is nothing to answer on, the command fails and names the flag rather than hanging.

### Safety

- No command deletes a file. The most destructive operation available is a rename, and a rename
  that would replace an existing file is refused.
- `organize` and `rename` refuse to run at a filesystem root, a home directory, or a system
  directory without `--force`. The guard cannot be disabled from configuration.
- Paths are fully resolved before any decision is made about them, and every destination is
  checked for containment inside the operation root.
- Operations enumerate first and act second; the tree is never modified during the walk over it.
- Cancellation is observed between files, never inside one.

### Added: foundation

- `devnest version`: version, commit, build date, Go version, and platform. Build metadata is
  injected at link time by the release build.
- `devnest help`: help for DevNest or for a specific command. Identical to
  `devnest <command> --help`.
- Global flags: `--output`, `--config`, `--quiet`, `--verbose`, `--no-color`, `--log-format`,
  `--log-timestamps`, `--help`, `--version`. Flag position does not matter, a flag may come before
  or after the command it applies to.
- Output formats `table` and `json`. Every result is rendered from one value, so the two views can
  never disagree.
- Configuration from four sources, in increasing precedence: compiled defaults, a TOML file,
  `DEVNEST_*` environment variables, and command-line flags. DevNest runs with no configuration
  file at all; an annotated example ships in `configs/`.
- A classified error model with stable codes and a documented exit-code contract:
  0 success, 1 failure, 2 usage, 3 not found, 4 permission denied, 5 cancelled.
- Structured logging to stderr at four levels, in a human-readable format or as JSON. stdout
  carries results and nothing else, at every verbosity.
- Interrupt handling: the first Ctrl+C cancels and unwinds, the second exits immediately.
- CI across Windows, Linux, and macOS, with the race detector, an 80% coverage floor, an
  import-boundary check, dependency verification, and a vulnerability scan.

### Notes

- No third-party dependencies. `go.sum` is empty and intended to stay small.
- Project foundation from Phase 0: architecture, layering rules, module boundaries, directory
  structure, and the documentation set in `docs/`.
- `.ts` is classified as TypeScript rather than as an MPEG transport stream. An extension can
  belong to one category only, and on a developer's machine the source file is the overwhelmingly
  more likely meaning.

Output formats are still `table` and `json`; CSV and Markdown land with the first user who needs
them. See `docs/roadmap.md` for what comes next.

---

[Unreleased]: https://github.com/zeeqsleepy/DevNest/compare/v0.5.0...main
[0.5.0]: https://github.com/zeeqsleepy/DevNest/releases/tag/v0.5.0
[0.4.0]: https://github.com/zeeqsleepy/DevNest/releases/tag/v0.4.0
[0.3.1]: https://github.com/zeeqsleepy/DevNest/releases/tag/v0.3.1
[0.3.0]: https://github.com/zeeqsleepy/DevNest/releases/tag/v0.3.0
[0.2.0]: https://github.com/zeeqsleepy/DevNest/releases/tag/v0.2.0
[0.1.0]: https://github.com/zeeqsleepy/DevNest/releases/tag/v0.1.0
