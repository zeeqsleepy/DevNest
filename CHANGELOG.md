# Changelog

Notable changes to DevNest, newest first.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versioning follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html), with the public surface defined
precisely in [docs/release-process.md](docs/release-process.md).

Entries describe what changed for a user, not which files were edited.

## [Unreleased]

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

[Unreleased]: https://github.com/<owner>/devnest/commits/main
