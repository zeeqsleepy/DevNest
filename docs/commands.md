# Commands

Status: every group below is implemented apart from the reserved names at the end. `init` and the
scaffolding templates are the one deferred item, and are described in `roadmap.md` under what
comes after 1.0.
Last revised: 2026-07-24

The full intended command surface. This is the plan, not a manual; anything here can change before
it ships, and anything that ships is frozen for the major version under `rules.md` R50.

Structure is always `devnest <group> <action> [target] [flags]`. Where a group has one obvious
action, that action is the default and the word may be omitted.

## Global flags

Available on every command, declared once on the root.

| Flag | Default | Meaning |
|---|---|---|
| `-o, --output` | `table` | `table`, `json`, `csv`, `markdown` |
| `--export <path>` | none | Also write the result to a file |
| `--export-format` | from extension | Override the export format |
| `-q, --quiet` | false | Suppress all non-error output |
| `-v, --verbose` | false | Debug-level logging to stderr |
| `--no-color` | auto | Disable colour; also honours `NO_COLOR` |
| `--config <path>` | OS config dir | Use a specific config file |
| `--dry-run` | varies | Show what would happen, change nothing *(planned)* |
| `-y, --yes` | false | Answer all confirmations affirmatively *(planned)* |
| `-h, --help` | none | Help for the current command |
| `--version` | none | Version, commit, build date, Go version |

---

## `devnest file`: file management *(implemented)*

| Command | Purpose |
|---|---|
| `file organize [path]` | Group files into category or extension folders |
| `file duplicate [path]` | Find files with identical content |
| `file rename [path]` | Rename many files at once |
| `file filter [path]` | Search by extension, category, name, or size |
| `file size [path]` | Show where the disk space went |
| `file hash <file...>` | Compute checksums |

Shared flags across the group: `-r, --recursive`, `--depth`, `--include-hidden`,
`--follow-symlinks`, `--exclude` (repeatable). See `cli-reference.md` for what each defaults to
and why.

### `file organize`

**Changes the disk.** Dry run by default.

Groups files into `Images/jpg/`, `Documents/pdf/`, and so on. `--by extension` produces flat
extension folders instead. Files with no extension land in `Other/no-extension/`.

Flags: `--by` (`category` or `extension`), `--on-conflict` (`skip`, `rename`, or `fail`),
`--apply`, `--force`, `-y, --yes`.

Only the files directly in the directory are touched unless `--recursive` is given, so an existing
folder structure is left as it is. Hidden files are skipped unless `--include-hidden` is given.
Running it twice does the same as running it once.

The default conflict policy is `skip`: an existing file is never replaced and no name is ever
invented. `rename` numbers the new arrival (`photo (2).jpg`) and `fail` refuses the whole
operation before anything moves.

### `file duplicate`

Read-only. Compares content, not names.

Groups by size first and hashes only the candidates, so most files are never opened. The oldest
file in each group is reported as the original.

Flags: `--algorithm` (`sha256`, `sha512`, `md5`), `--min-size`.

Nothing is deleted. The command reports what it found.

### `file rename`

**Changes the disk.** Preview by default.

Rules are applied to the name without its extension, in a fixed order: replacements, then case,
then the sequence number, then the prefix and suffix.

Flags: `--prefix`, `--suffix`, `--replace "from=to"` (repeatable), `--match <glob>`,
`--sequence`, `--sequence-start`, `--sequence-pad`, `--sequence-separator`,
`--sequence-position` (`prefix` or `suffix`), `--lowercase`, `--uppercase`, `--apply`, `--force`,
`-y, --yes`.

The whole plan is checked before anything moves. If two files would end up with the same name, or
a name is already taken by a file that is not itself being renamed away, the entire operation is
refused with nothing changed.

The result lists every old and new name. `--output json` redirected to a file is the rollback
record.

### `file filter`

Read-only.

Flags: `-e, --extension` (repeatable, dot optional), `--category`, `--match <glob>`,
`--min-size`, `--max-size`, `--sort` (`path`, `name`, `size`, `modified`), `--limit`,
`--categories`.

Every condition given must hold. Giving none lists everything. `--categories` prints the category
table and what each one claims.

### `file size`

Read-only. Always measures the whole tree; `--depth` controls how much of the result is reported,
not how much is measured, so the figures always add up.

Flags: `--depth`, `--top-directories`, `--top-files`.

### `file hash`

Read-only. SHA-256 by default.

Flags: `-a, --algorithm` (repeatable), `--all`.

Several algorithms are computed from a single pass over each file. A directory is refused rather
than walked: a tree digest has to fold in names as well as content, which is a different operation
with a different meaning.

---

## `devnest network`: networking *(implemented)*

The only commands in DevNest that open a socket.

| Command | Purpose |
|---|---|
| `network monitor <url>` | Is the site up, and how fast |
| `network http <url>` | One request, reported in full |
| `network latency <url>` | Repeated measurements, summarised |
| `network ping <host>` | Is the host reachable (TCP) |
| `network dns <domain>` | A, AAAA, CNAME, MX, TXT, NS records |
| `network ssl <host>` | Certificate issuer, expiry, trust status |

Shared flags: `--timeout`, `--insecure` (not on `ssl`), and where relevant `--attempts`,
`--interval`, `--port`. Defaults come from `[network]` in the configuration.

### `network monitor`

Flags: `--method`, `--header` (repeatable), `--expect-status`, `--max-response`, `--no-redirect`.

Any 2xx or 3xx is healthy unless `--expect-status` says otherwise. `--max-response` marks a site
that answered too slowly as slow, and slow counts as unhealthy. **Exits 1 when the site is not
healthy**, so it works as a cron entry without anyone parsing the output.

### `network http`

Flags: `-X, --method`, `-H, --header` (repeatable), `--data`, `--file`, `--no-redirect`,
`--max-redirects`, `--show-secrets`, `--body`.

Reports the status, a timing breakdown (DNS, connect, TLS, first byte, total), both sets of
headers, the redirect chain with the status that caused each hop, and the TLS session.

A non-2xx status is not a failure: inspecting a 404 is an ordinary reason to run this.
Credential-shaped headers are masked in every output format unless `--show-secrets` is given.

`--data` and `--file` cannot both be used; preferring one silently would mean sending a body the
user did not intend.

### `network latency`

Flags: `--method`, `--header`, `--attempts`, `--interval`, `--show-attempts`.

Reports minimum, average, median, maximum, and standard deviation. The median is there because a
single slow attempt drags an average badly, and the two disagreeing is the signal that something
is intermittent.

Attempts run in sequence with a pause, and connections are never reused. **Exits 1 when every
attempt failed.**

### `network ping`

Flags: `--port` (default 443), `--attempts` / `--count`, `--interval`, `--show-attempts`.

**This opens a TCP connection; it does not send ICMP.** Sending ICMP needs a raw socket and
therefore administrator rights, and DevNest never asks for elevation. Every result reports
`method: "tcp"`. A TCP probe also answers what people usually mean: plenty of hosts drop ICMP
while accepting connections on 443.

Reports probes sent, received, loss percentage, and timing. **Exits 1 when the host never
answered.**

### `network dns`

Flags: `-t, --type` (repeatable): `A`, `AAAA`, `CNAME`, `MX`, `TXT`, `NS`.

Every type is looked up unless `--type` says otherwise. A type with no answers is reported as
such rather than failing: a domain with no MX record is an ordinary domain. **Exits 3 when
nothing at all was found.**

TTL is not reported: the standard library's resolver does not expose it, and inventing a number
that looks authoritative would be worse than leaving it out.

### `network ssl`

Flags: `-p, --port` (default 443), `--warn-days` (default 30), `--chain`.

Reports issuer, subject, validity window, days remaining, trust status, TLS version, cipher, and
the names the certificate covers.

An expired or untrusted certificate is a result, not an error: this is the command you run when
something is wrong with one. **Exits 1 when the certificate is expired, not yet valid, or
untrusted.** Expiring soon is a warning, not a failure; the certificate still works.

There is no `--insecure` flag here and none is needed.

---

## `devnest security`: defensive security utilities *(implemented)*

Everything here happens locally. Nothing is sent anywhere and nothing is written to a file.

| Command | Purpose |
|---|---|
| `security password` | Generate a strong random password |
| `security password-check <password>` | Judge how strong a password is |
| `security hash <text>` | Digest of text, a file, or standard input |
| `security checksum <file> <hash>` | Verify a file against a published digest |
| `security encode <text>` | Base64-encode |
| `security decode <value>` | Base64-decode |

Commands that take a secret accept `--stdin`, which is the safer way to supply one. See
`cli-reference.md`.

### `security password`

Flags: `-l, --length`, `--count`, `--no-uppercase`, `--no-lowercase`, `--no-digits`,
`--no-symbols`, `--symbols`, `--symbol-set`, `--exclude`, `--exclude-ambiguous`, `--require-each`.

Uses the operating system's cryptographic random source. There is no seed and no way to reproduce
a result.

`--require-each` guarantees one character from every enabled class and shuffles the result, so the
required characters are not at predictable positions. `--exclude-ambiguous` drops characters people
misread when copying by hand.

The default symbol set omits quotes and backslashes, because a password that breaks when pasted
into a shell or a YAML file gets replaced by a weaker one the user picks themselves.

Defaults for length, symbols, and ambiguity come from `[security]` in the configuration.

### `security password-check`

Flags: `--stdin`, `--min-score`.

Reports a score out of 100, a rating, the entropy estimate, and each weakness found with a
suggestion.

**The password never appears in the result**: not the password, not a substring, not a quoted
example of the pattern found. **Exits non-zero below `--min-score`**, which makes it usable as a
check in a setup script.

The built-in list of known-bad passwords is short by design, and a clean result says so.

### `security hash`

Flags: `-a, --algorithm` (repeatable), `--all`, `-f, --file`, `--stdin`.

Hashes text, a file, or standard input, exactly one of the three. Several algorithms are computed
from a single pass. Files are streamed, so memory use does not depend on size.

MD5 is available because published checksums still use it, not because it is a reasonable choice
for anything new.

This shares its implementation with `devnest file hash`; that command takes several files at once,
this one adds text and standard input. Both are kept for 1.0: the overlap is one line of help
text, and removing either would leave one of the two jobs in the wrong group.

### `security checksum`

Flags: `-a, --algorithm`.

The algorithm is inferred from the digest's length (32 characters is MD5, 64 is SHA-256, 128 is
SHA-512) so there is nothing to remember. A stated `--algorithm` that disagrees with the length is
reported rather than quietly resolved.

Both digests are printed whether or not they match. **Exits non-zero on a mismatch.**

### `security encode` / `security decode`

Flags: `--url-safe`, `--no-padding`, `--stdin` on encode; `--stdin`, `--raw` on decode.

Decode accepts both alphabets, with or without padding, and ignores whitespace from a value wrapped
across lines.

When decoded bytes are not printable text, the result is shown as hex instead. Arbitrary bytes can
carry escape sequences that change how a terminal behaves, and a decode command is exactly where
untrusted bytes arrive. `--raw` overrides that.

Base64 is an encoding, not encryption.

---

## `devnest log`: log analysis *(implemented)*

Read-only, always. Every command takes one text log file and reads it once.

| Command | Purpose |
|---|---|
| `log analyze <file>` | Size, line count, detected format, read time |
| `log http <file>` | Full access log summary |
| `log errors <file>` | Failures, grouped and counted |
| `log status <file>` | Response status distribution |
| `log top <file>` | Most requested endpoints |
| `log search <file> <keyword>` | Lines containing a keyword, with line numbers |
| `log stats <file>` | Line lengths and the longest lines |

Nothing here loads a file into memory. Every command streams through a reused buffer, so a four
gigabyte log costs the same resident memory as a four kilobyte one. Every result reports how long
the read took, and the ones that parse a format report how many lines they could not read.

Results are rows, so `--output csv` works on all seven.

### `log analyze`

The command to run first. Reports size, total lines, blank lines, the detected format, and the
read time, which is also a fair estimate of what the other commands will cost on the same file.

The format is detected from the first two hundred non-blank lines and the result says how many it
sampled, so the guess can be judged rather than taken on faith. Four types: `http-access`,
`application`, `json-lines`, `text`.

A binary file is refused with a message saying so. It has no lines to report on, and a summary of
one enormous "line" would be nonsense dressed as a result.

### `log http`

Flags: `--top` (default 10).

Reports total requests, methods, status classes, the most common status codes, the busiest
endpoints, the loudest clients, and response sizes.

Understands the Common and Combined Log Formats, which is what nginx and Apache write by default.
Query strings are stripped before endpoints are counted, so `/search?q=cats` and `/search?q=dogs`
are one endpoint rather than two requests that look unrelated.

The average response size is over the responses that carried a body. Including the 304s would drag
it towards zero and make a working cache look like a server that stopped sending anything.

A line that is not an access log entry is counted as unparsed and the run continues. A file with
none at all produces a summary of zero requests and a warning saying so, not a failure.

### `log errors`

Flags: `--top` (default 10), `--warnings`.

Two kinds of line count as a finding: one announcing a severity, which is how an application log
reports a problem, and a request that came back 5xx, which is how an access log does. Both go
through the same grouping, because an incident is investigated across both kinds of file.

Messages are grouped by a normalised form of themselves, with runs of digits replaced, so `user
4821 not found` and `user 9930 not found` are one finding seen twice. Each group reports its first
and last line number, so the raw entries stay findable.

Findings are also counted by severity (`fatal`, `error`, `warning`) and by category (`timeout`,
`connection`, `permission`, `not-found`, `database`, `memory`, `crash`, `parse`, `tls`, `disk`,
`server-error`, `other`).

Warnings are counted but stay out of the findings unless `--warnings` is given. A summary that
lists every deprecation notice buries the three lines that matter.

### `log status`

Flags: `--top` (default 10).

All five status families are always reported, including the ones with no requests: a summary that
quietly omits 5xx leaves the reader unable to tell "none" from "not measured". The 4xx and 5xx
total is reported together, because that is the number people actually watch.

Reads the same collection as `log http`, so the two can never disagree about how many requests a
file holds.

### `log top`

Flags: `--limit` / `-n` (default 10), `--clients`.

The busiest endpoints with counts and shares. `--clients` ranks client addresses instead, from the
same single pass.

### `log search`

Flags: `-i, --ignore-case`, `--limit` / `-n` (default 100).

Plain substring matching, case-sensitive by default, which is what you want when the keyword is an
identifier. Not a regular expression engine, deliberately: the one time a log search needs one,
grep is already installed, and keeping this to a substring is what keeps it fast enough to be the
obvious thing to reach for.

The whole file is always read, so the match count is real even when the listing stops at
`--limit`, and a listing cut short says so. **Exits 3 when the keyword appears nowhere**, so a
script can branch without parsing anything.

### `log stats`

Flags: `--top` (default 10).

Line count, blank lines, average line length, the longest and shortest line, and the longest lines
with their line numbers and an excerpt. This is the command for "why is this file eight
gigabytes"; the answer is usually a handful of lines with a serialised payload in them, and their
line numbers are what makes them findable.

The average and the shortest line are over lines that hold something. The shortest line in almost
every log file is empty, and reporting zero answers nothing.

---

## `devnest env`: environment inspection *(implemented)*

Read-only, always. Nothing here changes a variable or writes a file, and every program that is run
is run with a timeout and without a shell.

| Command | Purpose |
|---|---|
| `env` | Full summary: OS, architecture, shell, detected toolchains, PATH health |
| `env list` | Detected toolchains with versions and resolved paths |
| `env path` | PATH entries with problems flagged |
| `env which <tool>` | Every location a tool resolves from, in PATH order |
| `env vars [pattern]` | Development-relevant environment variables, credentials hidden |

A tool that is not installed is an ordinary result, and so is one that is installed and will not
say what version it is. Both are reported, and neither stops the run.

### `env`

The summary. OS and architecture, CPU count, shell, terminal, home, the number of PATH entries and
how many have problems, and the detected toolchains grouped by kind. Run on a machine that is not
yours, or on your own after something stopped working. The PATH check here is the cheap half;
`env path --shadows` is the expensive one.

### `env list`

Flags: `--tool` (repeatable), `--missing`, `--timeout`.

Every toolchain, with its version and the location that runs. `--tool` restricts to the names you
give, including names the built-in table has never heard of, which are located but not run.
`--missing` keeps the absent tools in the listing, for a build-agent report.

### `env path`

Flags: `--shadows`.

PATH entries in order, flagging the ones listed twice, pointing at nothing, or pointing at a file.
`--shadows` adds the check that reads every directory on PATH to find executables resolvable from
more than one of them, which is the finding behind most "but I installed the new version" reports
and the only part that costs anything. A problem is a finding, not a failure.

### `env which`

Flags: `--versions`, `--timeout`.

Every place a name resolves to, in PATH order, with the winner marked. This differs from the shell
built-in by showing *all* matches rather than the first, which is the information you actually want
when the version is wrong. `--versions` runs each copy to report what it says it is; only tools the
table describes are run. **Exits 3 when the name resolves to nothing.**

### `env vars`

Flags: `--all`, `--reveal`.

Development-relevant variables, filtered by an optional name pattern. **Values whose name looks
like a credential are hidden, in the result itself rather than in one rendering of it**, because a
listing gets redirected to a file and attached to a ticket. What is shown in place is the length,
not a prefix. `--all` lists everything; `--reveal` prints the hidden values in full and warns that
it did.

---

## `devnest scan`: project analysis *(implemented)*

Read-only, always. Default target is the current directory.

| Command | Purpose |
|---|---|
| `scan [path]` | Structural summary of a project tree |
| `scan types [path]` | File counts and sizes by extension or detected language |
| `scan lines [path]` | Line counts, split into code, comment, and blank |
| `scan tree [path]` | Directory tree, with totals for each branch |

The walk skips what the project ignores: `.gitignore` rules, the vendor and build directories every
ecosystem has, and always `.git`. Without that, a small Node project reports four hundred thousand
files of which four hundred are the project. `--no-ignore` turns it off.

Where the disk space went is `devnest file size`; `scan` reports shape, not weight. Results are
rows, so `--output csv` works throughout.

Shared flags: `--depth`, `--include-hidden`, `--follow-symlinks`, `--no-ignore`, `--exclude`
(repeatable).

### `scan`

Flags: `--top`.

Files, directories, size, depth, a breakdown by category, the top languages and extensions, and the
authored share: the part of the tree somebody wrote, with vendored and generated files left out.
Every category is reported, including the empty ones, so a reader can tell "none" from "not
measured".

### `scan types`

Flags: `--limit`, `--by-language`.

The file-type breakdown, by extension or, with `--by-language`, folding `.js`, `.mjs`, and `.jsx`
into one row. Files whose language is not recognised are counted and reported as a total rather than
dropped.

### `scan lines`

Flags: `--limit`, `--max-file-size`.

Lines split into code, comment, and blank, grouped by language. Only files in a recognised language
are opened; files above `--max-file-size` are counted and skipped. The comment detection is
deliberately simple and does not parse the language, which is the right trade for numbers used to
compare parts of a tree with each other.

### `scan tree`

Flags: `--depth`, `--files`, `--max-entries`.

The directory shape, with the file count and size of everything under each branch, including the
levels not shown. Directories only by default; `--files` includes files. A listing cut at
`--max-entries` says so.

---

## `devnest clean`: artifact removal

**Destructive.** Dry run is the default; `--apply` is required to delete anything.

| Command | Purpose |
|---|---|
| `clean [path]` | Find removable artifacts, report reclaimable space, delete nothing |
| `clean apply [path]` | Same as `clean --apply` |
| `clean rules` | The directory names clean would ever consider, and what each needs |

Flags: `--apply`, `--pattern <name>` repeatable to restrict to specific artifact types,
`--protect <path>` repeatable, `--force` for a run at a filesystem root or home directory,
`--yes`. The path defaults to the current directory.

Safety behaviour is described in `modules.md` and the destructive flow in `flow.md`. Summary: only
names in the rule set are ever candidates; a generic name such as `build` counts only when a
project file sits beside it; nothing outside the scan root, on another filesystem, or behind a
symlink is touched; version control directories are never entered; and every candidate is checked
again in the moment before it is removed.

`clean rules` is worth running before the first `--apply`. It prints the whole surface of what the
command can ever remove, which is a short table.

---

## `devnest port`: port inspection

`port list` and `port check` are read-only. `port free` is **destructive**: it terminates a
process.

| Command | Purpose |
|---|---|
| `port list` | Listening sockets with owning process |
| `port check <n>` | Whether a port is in use, and by what |
| `port free <n>` | Terminate the process holding a port |

Flags: `--tcp` / `--udp`, `--all` to include ports below 1024 in a listing, `--force` for forceful
termination after the request times out, `--grace <duration>`, `--yes`.

`port check` exits 0 when the port is free and 3 when it is in use, so a script can branch without
parsing anything. Unlike the listing, it answers about ports below 1024 without `--all`.

`port list` hides ports below 1024 by default and reports how many it hid. Sockets whose owning
process the system will not name are listed with the owner shown as unknown, and the count of those
is in the result: a listing that dropped them would answer "what is listening" with something
untrue.

`port free` names the process and asks for confirmation before doing anything. It refuses pid 0 and
pid 1 unconditionally, refuses a port held by more than one process rather than guessing, and
re-verifies the pid against the port immediately before signalling. A process owned by another user
is refused by the operating system; DevNest never asks for elevation.

**On Windows, `port free` requires `--force`.** Windows offers no way for one process to ask
another to exit politely, so the command says so rather than presenting a kill as a request.

---

## `devnest hash`: checksums *(superseded)*

Hashing a file is `devnest file hash`; hashing text and verifying one checksum are `devnest
security hash` and `devnest security checksum`.

Two gaps were left open here and are now settled. **Verifying against a whole checksum file** (the
`SHA256SUMS` a release publishes) is worth having and arrives as a flag on `security checksum`, not
as a group of its own; see `roadmap.md`. It waits until after 1.0 only because adding a flag breaks
nothing later. **Deterministic tree digests are not planned**, and that is a decision rather than a
delay: see `modules.md`.

---

## `devnest encode` / `devnest decode`: encoding

Base64 is `devnest security encode` and `devnest security decode`, alongside the commands that hash
and verify. These two groups cover what Base64 does not.

| Command | Purpose |
|---|---|
| `encode hex <input>` | Hex encode, `--upper` for A-F |
| `encode url <input>` | Percent-encode, `--path` for a path segment |
| `decode hex <input>` | Hex decode |
| `decode url <input>` | Percent-decode, `--path` to keep a literal plus |
| `decode jwt <token>` | Print header, payload, and claims; report expiry |

All five accept `--stdin`.

`encode url` treats the whole input as one value and never as a URL: encoding a complete URL would
escape the colons and slashes that make it one. The default is query encoding, where a space
becomes `+`; `--path` encodes for a path segment, where it becomes `%20`. Decoding mirrors that,
and getting it wrong is how a plus sign in a filename silently becomes a space.

Decoded bytes that are not printable text are shown as Base64 rather than written to the terminal,
because arbitrary bytes can carry escape sequences that change how a terminal behaves.

`decode jwt` **never verifies signatures**, and the output says so in a field rather than only in
this document. It exists so that nobody needs to paste a token into a web page, which hands the
token to whoever runs the page. An expired token is a warning and still exit 0: the expiry is a
fact about the input, not a failure of the command. `alg=none` is warned about too.

---

## `devnest json` / `devnest yaml`: structured data

Read-only. Nothing here writes to the file it read; output goes to stdout so a redirect does the
writing where the user can see it.

| Command | Purpose |
|---|---|
| `json <path>` | Validate, and report shape, size, and top-level entries |
| `json format <path>` | Reprint with one indentation width, `--indent` to set it |
| `json minify <path>` | Strip whitespace |
| `json query <path> <expr>` | Select a subtree, `--raw` for a bare string |
| `json to-yaml <path>` | Convert to YAML |
| `json to-csv <path>` | Convert to CSV, `--flatten` for nested values |
| `yaml <path>` | Validate, and report shape, size, and document count |
| `yaml to-json <path>` | Convert to JSON, `--indent` to set the width |

All accept `--stdin`. Parse errors report the line, the column, and the offending line, because
"invalid JSON" on its own tells the user what they already knew.

There is deliberately no `yaml format`. Reprinting YAML means decoding and re-emitting it, which
drops every comment in the file; a formatter that silently deletes the comments from a
configuration file is not one anybody should run.

`json format` reprints from the document's own bytes, so key order and number precision survive
and only the whitespace changes. `json query` re-encodes the value it selects, so object keys come
back sorted; that is the trade for selecting a subtree rather than reprinting the file.

**Query expression syntax**, decided in Phase 7 and recorded in `prd.md`: keys separated by dots,
array elements by `[n]`, and a key holding a dot or a space in `["quoted brackets"]`. A leading `.`
or `$` is optional. There are no filters, wildcards, or functions. A query language is a product of
its own; anything past selecting a subtree is what `jq` is for.

`json query` exits 3 when the expression selects nothing, so a script can branch on whether a key
exists. `json to-csv` reports a nested value rather than stringifying it into a cell, because a
spreadsheet that looks converted and is not gets found weeks later, in a report.

---

## `devnest http`: HTTP requests *(superseded)*

Implemented as `devnest network http`, alongside the other networking commands. See that section.

---

## `devnest git`: repository inspection

Read-only. Never commits, pushes, or deletes anything.

| Command | Purpose |
|---|---|
| `git [path]` | Repository summary |
| `git branches [path]` | Branches with last commit and age |
| `git stale [path]` | Branches with no activity for `--days` (default 90) |
| `git contributors [path]` | Commit counts and date ranges by author |
| `git large [path]` | Largest objects in history |

`git stale` can print the deletion commands with `--print-commands`. It does not run them; the
user reviews the list and decides.

Requires the `git` executable on PATH. Its absence produces a clear message, not an obscure
failure.

---

## `devnest secret`: credential scanning

Read-only. Findings are always redacted.

| Command | Purpose |
|---|---|
| `secret scan [path]` | Scan the working tree |
| `secret history [path]` | Scan git history, slower and more thorough |
| `secret rules` | List active detection rules |
| `secret test <string>` | Check whether a string would match, for tuning |

Flags: `--entropy` threshold, `--exclude <pattern>` repeatable, `--rule <name>` to run a subset,
`--fail-on <severity>`.

`--entropy` moves the floor on the rules that match by shape, which is where a noisy report comes
from. The rules matching a provider's prefix keep their own floor whatever is passed: `AKIA`
followed by sixteen uppercase characters is an AWS key identifier however it scores. Set it once
in `secret.entropy_threshold` rather than on every run.

Exits non-zero when findings exist at or above `--fail-on`, which is what makes it usable as a
pre-commit hook or a CI gate.

Matched values are never printed in full, in any output format, at any verbosity.

---

## `devnest config`: configuration

| Command | Purpose |
|---|---|
| `config` | Show the resolved configuration and where each value came from |
| `config list` | All keys with current values |
| `config get <key>` | One value |
| `config set <key> <value>` | Set a value in the user config file |
| `config unset <key>` | Remove a key, reverting to the default |
| `config path` | Path of the config file in use |
| `config init` | Write an annotated default config file |
| `config validate` | Check the file for errors |

`config` showing the origin of each value (default, file, environment, or flag) is the fastest
way to answer "why is it behaving like that".

---

## `devnest doctor`: self-check

| Command | Purpose |
|---|---|
| `doctor` | Verify DevNest's own installation and configuration |

Checks that the config file parses and holds values DevNest accepts, that the directory it lives
in can be written to, that the rule tables are compiled in and not empty, that the external tools
optional features need are present, and what the terminal was detected as.

Output is intended to be pasted into a bug report: paths under the home directory are shortened to
`~` and the hostname is not reported.

A warning is something absent that DevNest works without, such as git on a machine that never runs
the git commands. Only a failed check exits non-zero. `doctor` is also the one command that still
runs when the configuration file will not load, because it is the command that says so.

---

## `devnest export`: report generation

| Command | Purpose |
|---|---|
| `export <command...>` | Run a command and write a formatted report |

A convenience wrapper over `--export` for multi-command reports. Detail in `export-system.md`.

Each argument is one command, so a subcommand goes in quotes: `devnest export "secret scan" scan`.
Every command runs with its defaults, since a flag here could not be told apart from the next
command's name. A failing command does not stop the ones after it, and the exit code is the worst
of the individual ones.

---

## `devnest completion`: shell completion

| Command | Purpose |
|---|---|
| `completion powershell` | PowerShell completion script |
| `completion bash` | bash completion script |
| `completion zsh` | zsh completion script |
| `completion fish` | fish completion script |

Prints to stdout for the user to redirect into their profile. Installation instructions are in
`installation.md`.

---

## `devnest version`

Prints version, commit hash, build date, Go version, and platform. `--output json` for scripts.

---

## Reserved names

Not used, and not to be used for anything else: `install`, `update`, `upgrade`, `serve`, `daemon`,
`login`, `auth`. Each implies behaviour DevNest deliberately does not have, and reserving them
prevents a future contributor from accidentally implying it does.
