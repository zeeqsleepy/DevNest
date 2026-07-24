# Modules

Status: `core/file`, `core/network`, `core/security`, `core/log`, `core/env`, `core/scan`,
`core/encoding`, `core/data`, `core/port`, `core/clean`, `core/git`, `core/secret`, and
`core/doctor` implemented; `core/config` planned
Last revised: 2026-07-24

Every unit of real work in DevNest is a module: one package under `internal/core/`, one command
group in the CLI. This document lists the planned modules, what each owns, what it depends on,
and where it might grow.

## The module contract

Every module, without exception, follows the same shape. This uniformity is what makes the
codebase learnable: read one module and you can read all of them.

```
internal/core/<name>/
    <name>.go        Request, Result, and the entry function
    deps.go          the narrow interfaces this module needs from platform
    <name>_test.go   tests using fakes for those interfaces
    testdata/        fixture inputs, if any
```

Rules restated from `architecture.md` because they are violated most often here:

- `Request` and `Result` are plain data with JSON tags. No interfaces, no channels, no writers.
- The entry function takes `(context.Context, Request)` and returns `(Result, error)`.
- No printing. No `os.Exit`. No reading of flags, environment, or config files.
- No importing of another `core/*` package, ever.
- All outside-world access goes through interfaces declared in this module's own `deps.go`.

## Module inventory

### `core/file`: file management *(implemented)*

**Owns.** Six operations over a directory tree: organising files into folders, finding duplicates
by content, batch renaming, filtering by extension or category, analysing where the space went,
and hashing.

**Why one module and not six.** They share a vocabulary: the same `Selection` walk options, the
same `file.Info`, the same category table. Six packages would have meant six copies of that or a
seventh package to hold it. Each operation still has its own file, its own request and result
types, and its own entry function, so nothing is entangled; they simply share a package.

**Dependencies, and why there are two.** The module declares `Inspector` (resolve, contains,
protected-reason, stat, walk, digest) and `Mover` (`Inspector` plus exists, ensure-directory,
move). An operation taking an `Inspector` is incapable of changing the disk, and the signature is
the guarantee. Only `Organize` and `RenameFiles` take a `Mover`.

**The operations.**

| Function | Takes | What it does |
|---|---|---|
| `Organize` | `Mover` | Plans, and optionally performs, moves into category or extension folders |
| `Duplicates` | `Inspector` | Groups files by size, hashes only the candidates, reports groups |
| `RenameFiles` | `Mover` | Builds a rename plan, refuses on any conflict, optionally applies it |
| `Filter` | `Inspector` | Searches by extension, category, name glob, and size range |
| `Size` | `Inspector` | Totals per directory and the largest files |
| `Hash` | `Inspector` | Checksums for one or more files |

**Safety design.** This is the module that moves people's files, so the rules are worth restating:

- Nothing is ever deleted. The most destructive thing here is a rename.
- Dry run is the default for `Organize` and `RenameFiles`; `Apply` is required to change anything.
- Every path is resolved (absolute, symlinks followed) before any decision is made about it.
- Every destination is checked for containment inside the operation root, even though every
  destination is derived from a file's own name. "Should be impossible" is how paths get out.
- A move refuses to replace an existing file, whatever the platform's rename would do.
- `Organize` and `RenameFiles` refuse to run at a filesystem root, a home directory, or a system
  directory without `--force`. The guard cannot be disabled from configuration.
- Enumerate first, then act. The tree is never mutated during the walk over it.
- `RenameFiles` checks the whole plan for conflicts before touching anything, and a conflict
  anywhere aborts the batch with nothing changed.
- `Organize` continues past a file it cannot move, recording the failure. Stopping halfway leaves
  a directory that is harder to reason about than one that finished and reported.
- Cancellation is observed between files, never inside one.

**Duplicate detection.** Two passes. Files are grouped by size first, which is free and eliminates
most of the tree, and only groups holding more than one file are read and hashed. Hashing streams
through a fixed buffer, so memory use is independent of file size. If this proves slow on a large
media library (many files sharing a size but differing in content) the next step is a partial
digest of the first block as a second filter. That is deliberately not there yet: it is complexity
that needs a measurement to justify it.

**Rollback.** `RenameResult` carries every source and destination, applied or not. Running with
`--output json` and keeping the file is the rollback record; the command's own help says so.

**Categories.** A table mapping extensions to categories, in `categories.go`. Adding a format is
one entry. When `scan` and `clean` need the same classification, the table moves into its own
package below the module layer; two consumers inside one package do not justify that yet.

**Later.** Duplicate detection could grow a partial-digest prefilter. Organise could learn an
`--undo` that reads back a JSON record rather than leaving the user to script it.

---

### `core/network`: networking *(implemented)*

**Owns.** Six operations: checking whether a site is up, inspecting an HTTP exchange, measuring
latency, probing a host, looking up DNS records, and inspecting a TLS certificate.

**Dependencies, and why there are four.** `Requester`, `Resolver`, `Prober`, and `Inspector`, one
per kind of network operation. Unlike the file module, whose operations all reach for the same
filesystem, these genuinely need different things, and splitting them means a test that fakes a
DNS lookup implements one method rather than six.

**The operations.**

| Function | Takes | What it does |
|---|---|---|
| `Monitor` | `Requester` | One availability check: up, slow, or down, with the response time |
| `Fetch` | `Requester` | One request, reported in full: status, headers, timing, redirects, TLS |
| `Latency` | `Requester` | Repeated measurements reduced to min, average, median, and maximum |
| `Ping` | `Prober` | Repeated TCP connections: reachability, loss, timing |
| `Lookup` | `Resolver` | A, AAAA, CNAME, MX, TXT, and NS records |
| `Inspect` | `Inspector` | Certificate issuer, subject, expiry, days remaining, trust status |

**A failure to reach something is a result, not an error.** This is the design decision that runs
through the whole module. A site being down, a host refusing a connection, a certificate having
expired: these are the answers the commands exist to produce. Treating them as errors would make
the exit code mean "DevNest broke" rather than "the site is down", which is the difference between
a usable cron entry and a confusing one.

Only a failure to *ask* the question comes back as an error: an unusable URL, a cancelled run, a
handshake that never completed.

**Ping is TCP, not ICMP.** Sending an ICMP echo needs a raw socket and therefore elevated
privileges on every supported platform, and DevNest never asks for elevation. The alternative,
shelling out to the system `ping` and parsing its output, depends on the machine's language
settings, which is not a foundation for a cross-platform tool.

A TCP probe also answers the question people usually mean: plenty of hosts drop ICMP while
accepting connections on 443 perfectly well. Every result carries `method: "tcp"` and every
rendering says so, because a reader needs to know which question was answered.

**Certificate inspection deliberately handshakes without verifying.** A verifying handshake fails
and returns an error instead of the certificate, which is useless for the one command whose whole
job is inspecting broken certificates. The chain is retrieved and then verified separately against
the system trust store and the host name. Nothing is sent over the connection, and it closes as
soon as the certificate has been read. This is why `devnest network ssl` has no `--insecure` flag
and needs none.

**Header masking happens in the result.** Credential-shaped headers are masked in `FetchResult`,
not in one output path, because a report gets attached to a ticket and a ticket gets shared. The
same masking therefore applies to the table, the JSON, and any future export.

**Latency is not a load test.** Attempts run in sequence with a pause between them. Firing them
concurrently would measure how well the server handles concurrency (a different question) and
would look like a load generator in someone's logs. The attempt count is capped for the same
reason.

**Connections are never reused.** A reused connection reports almost no setup cost for every
attempt after the first, which flatters the numbers and hides the part most likely to be slow.

**Later.** A repeat mode for `monitor` would need a scheduler, and DevNest has no daemon mode by
design; a cron entry plus the exit code already does the job. Reporting DNS TTL needs a resolver
the standard library does not provide.

---

### `core/security`: defensive security utilities *(implemented)*

**Owns.** Generating passwords, judging their strength, hashing text or a file, verifying a file
against a published checksum, and Base64 encoding and decoding.

**Dependencies.** One: `Hasher`, which is `platform/fs`'s digest surface. The module cannot walk a
tree, move a file, or open a socket, and the interface is what enforces that.

**No logger.** Every other module takes one; this one does not. The simplest way to guarantee a
password never reaches a log is for the code to have nowhere to send it.

**The operations.**

| Function | What it does |
|---|---|
| `GeneratePassword` | Cryptographically random passwords with configurable classes and rules |
| `CheckStrength` | A score, a rating, and the specific weaknesses found |
| `Hash` | SHA-256, SHA-512, or MD5 over text or a file |
| `VerifyChecksum` | Compares a file against a published digest |
| `Encode` / `Decode` | Base64, both alphabets, padding optional |

#### Password generation

Randomness arrives as an `io.Reader` parameter: `crypto/rand.Reader` in production, a
deterministic stream in tests. There is no seed, no package-level source, and no path to
`math/rand`. A generator whose output can be reproduced is not a generator, and making the source a
parameter is what makes that testable rather than merely asserted.

Selection uses `crypto/rand.Int`, which does the rejection sampling that keeps it uniform. Taking a
random byte modulo the alphabet size would quietly favour the first characters of the pool (the
classic way this gets written wrong) so there is a test that checks the distribution of four
thousand generated characters.

`--require-each` draws one character from each enabled class and then shuffles the whole password
with the same random source. Placing the required characters at fixed positions would leak their
classes to anyone who has read this code, which is everyone.

The default symbol set omits quotes and backslashes. A password that breaks when pasted into a
shell, a YAML file, or a connection string gets replaced by a weaker one the user picks by hand,
which is a worse outcome than a slightly smaller alphabet.

#### Password strength

The score starts from an entropy estimate and loses points for the patterns that make such an
estimate a lie: short length, few character classes, repeated runs, a repeated block, sequences,
keyboard walks, digit-only values, and matches against a list of the passwords that top every
breach corpus.

A dictionary match applies a **ceiling** as well as a penalty. `Password123!` has four classes,
twelve characters, and about 79 bits of nominal entropy; subtracting points from that still leaves a
respectable-looking number, and a cracker finds it in milliseconds. The ceiling says the thing the
arithmetic cannot.

The embedded list is short on purpose. A real cracking wordlist has hundreds of millions of entries
and belongs in a dedicated tool; embedding one would add tens of megabytes to a binary whose appeal
is being one small file. A clean result therefore says in as many words that it is not a guarantee.

**The password never appears in the result.** Not the password, not a substring, not a quoted
example of the pattern that was found. Findings are fixed strings selected by code ("four
characters in consecutive order", never "1234") because a result is rendered, exported, and pasted
into tickets. This is tested two ways: that a given finding code always produces byte-identical
text whatever produced it, and that the serialised result never contains the input.

#### Hashing and checksums

Both go through `platform/fs`, which gained `DigestReader` for this so that text and files share
one implementation. A string and a file of the same content therefore produce the same digest by
construction rather than by coincidence, and there is a test asserting exactly that.

Checksum verification infers the algorithm from the digest's length (every supported algorithm has
a distinct one) so someone pasting from a release page does not have to know which kind it is. A
stated `--algorithm` that disagrees with the length is reported rather than quietly overridden.
Comparison is constant-time, which the threat model does not really demand for a local file but
costs nothing.

A mismatch is a result, not an error. The caller turns it into a non-zero exit code.

#### Base64

Both alphabets are accepted on decode, with or without padding, and whitespace from a value wrapped
across lines is ignored. Asking the user which shape they have is asking them to know something the
program can work out.

When decoded bytes are not printable text, the result carries hex instead and says so. Valid UTF-8
is not the test: an escape character is perfectly valid UTF-8 and can repaint a terminal, and a
decode command is precisely where untrusted bytes arrive.

**Overlap with `devnest file hash`, stated plainly.** Both commands hash a file, and both call the
same code. `file hash` takes several files at once and belongs with the file tools; `security hash`
takes one input and adds text and standard input. The duplication is in the command surface, not
in the implementation. Whether both should exist is a judgement worth revisiting before 1.0.

**Later.** An option to check a password against a breach corpus without sending it anywhere (the
k-anonymity range API does this) would be genuinely useful, but it makes a network call from a
module whose selling point is that it does not.

---

### `core/log`: log analysis *(implemented)*

**Owns.** Seven operations over one text log file: an overview, an HTTP access summary, an error
summary, a status code breakdown, a most-requested listing, a keyword search, and line statistics.

**Dependencies.** One: `Reader`, with three read-only methods (resolve, stat, open). The module
cannot walk a tree, move a file, or open a socket. A test satisfies the whole interface with a
string.

**The operations.**

| Function | What it does |
|---|---|
| `Analyze` | Size, line count, blank lines, detected format, read time |
| `SummarizeHTTP` | Requests, methods, statuses, top endpoints, top clients, response sizes |
| `SummarizeErrors` | Findings grouped by message, counted by severity and category |
| `SummarizeStatus` | The five status families and the most common codes |
| `TopRequests` | The busiest endpoints, or the busiest clients |
| `Search` | Lines containing a keyword, with line numbers |
| `Stats` | Line lengths, the extremes, and the longest lines |

#### One pass, fixed memory

This is the property the whole module is designed around, because the logs worth analysing are the
ones too large to open in an editor.

Every operation reads the file once through a buffer it reuses. The line handed to a visitor is a
slice into that buffer, valid only until the next line, and every place that keeps something
copies it deliberately. Nothing is loaded whole, and the only thing that grows with the input is
the number of distinct keys being counted.

Two consequences worth recording:

- **A line longer than the buffer is assembled, not abandoned.** `bufio.Scanner` gives up on a
  line longer than its buffer, and giving up on a file because one line is odd is not acceptable
  behaviour for a log tool. Past a one megabyte cap the content is cut, but the line is still
  counted and its true length still reported, so memory cannot follow the input by way of one
  pathological line.
- **Cancellation is checked every few thousand lines**, not every line. Checking per line is
  measurable on a file with ten million of them, and a few thousand still returns within
  milliseconds of a Ctrl+C.

#### Counting without allocating

The counters hold pointers rather than values. Go optimises a map lookup written as `m[string(b)]`
into one that does not copy the bytes, but assigning back into the map does copy them, so
incrementing in place through a pointer is the difference between allocating once per distinct
value and once per line. On a two hundred thousand line access log that measured as 800,000
allocations before the change and 1,273 after.

Cardinality is capped at 100,000 distinct values per counter. Past the cap, new values are counted
in a single bucket and the result says the ranking is of what was tracked. A ranking of a subset
that reads as a ranking of the whole is worse than no ranking.

#### Malformed input is normal

Log files are truncated mid-write, carry lines from three different programs, and hold entries no
format documents. None of that is an error here. A line that does not parse is counted as unparsed
and the run continues, because a summary of the ninety-eight percent that did parse is what the
user came for, and every result reports the count so the rest can be judged.

Only a failure to read the file at all is an error: a missing file, a directory, or a file that is
not text. The binary check is a NUL byte in the first eight kilobytes, which are already in the
buffer the scan is about to read from.

#### Parsing lives in one place

`http`, `status`, and `top` are three projections of one collection pass. Collecting a status code
costs the same whether or not this run reports it, and one collection is why "requests" means the
same number in all three. A second parser for status codes is how two commands end up disagreeing
about how many requests a file holds, and there is a test asserting they never do.

The access log parser is hand-written rather than a regular expression. A regexp over ten million
lines is the difference between a command that answers while you wait and one you go and make
coffee for, and the Common Log Format is a handful of space-separated fields. The protocol is taken
from the end of the request rather than the second space, because a path can contain a space and
dropping those entries would understate the request count.

#### Grouping error messages

The value of a "most common messages" listing is entirely in the collapse: the raw lines are
already in the file. Messages are grouped by a signature with runs of digits replaced, so the
timestamps, identifiers, and durations that make every occurrence unique are exactly what is
removed. The first line that produced a group is kept verbatim as the sample, along with the first
and last line numbers, so the raw entries stay findable.

Categories are a table of keywords checked in order, and the table is the whole of the
classification logic. Adding a category is an entry in it and nothing else.

**Later.** A `--since` filter would need timestamp parsing across the half-dozen formats logs use,
which is a real piece of work and not obviously worth it while `search` exists. Following a file as
it grows is a different shape of command (it never terminates) and would be the first thing in
DevNest that does.

---

### `core/env`: environment inspection *(implemented)*

**Owns.** Five operations: a machine summary, a toolchain listing, a PATH inspection, a
resolve-everywhere lookup, and an environment-variable listing.

**Dependencies, and why there are three.** `Runner` runs a program, `Locator` answers questions
about PATH, and `Describer` answers questions about the machine. They are separate because most
commands need only one, and an operation that only reads PATH should not be handed something that
can start a process. The summary needs all three and takes the composite `Inspector`.

**The operations.**

| Function | Takes | What it does |
|---|---|---|
| `Summarize` | `Inspector` | OS, architecture, shell, terminal, detected tools, PATH health |
| `List` | `Runner` + `Locator` | Every toolchain, with its version and location |
| `InspectPath` | `Locator` | PATH entries, their problems, and shadowed executables |
| `Which` | `Runner` + `Locator` | Every place a name resolves to, in order |
| `Vars` | `Describer` | Development-relevant variables, credentials masked |

**Detection is a table.** Executable name, version flag, and kind. Adding a toolchain is a line in
`toolchains.go` and nothing else. A tool is looked up on PATH first and only run if it is there, so
a machine with three of the thirty starts three processes, which is what keeps the whole command
inside its budget.

**A missing tool is an answer, and so is a mute one.** A machine without Rust is ordinary, and
reporting "not found" for it is correct rather than a failure. So is a tool that is installed and
will not say what version it is: it is reported as present with an unknown version, because knowing
a binary is there and would not answer beats knowing nothing. Only a request that cannot be carried
out at all is an error.

**Version extraction is one function.** Every tool announces itself differently (`go version
go1.25 windows/amd64`, `v22.1.0`, `git version 2.44.0.windows.1`), and the shared logic looks for a
token holding a digit, a dot, and another digit. A tool whose version is a bare integer therefore
reports nothing rather than guessing, which is the right way round: `x86_64-pc-linux-gnu` never
becomes version 86.

**Shadowing is the headline finding.** The same executable name resolvable from more than one PATH
entry is the cause of most "but I installed the new version" reports, and `Which` and `List` both
surface it. Only the winning copy is probed for its version by default, because running every copy
of every tool turns a summary into a minute of process creation.

**Credentials are masked in the result, not in a rendering.** A variable whose *name* looks like a
credential is masked in `VarsResult` itself, because a listing gets redirected to a file and
attached to a ticket. Masking is by name rather than by value shape, because guessing whether a
value is a secret is wrong in the dangerous direction. What replaces the value is its length, not a
prefix: a prefix identifies which key it is and, for some formats, starts guessing the rest.

**Depends on.** `platform/proc` for running and locating, `platform/sys` for describing the
machine. The operating-system differences (execute bits versus PATHEXT, file name versus stem)
live entirely in `proc`'s build-tag files, so this module has no OS conditionals.

**Later.** Per-project expectations: a file declaring required toolchain versions, with `devnest
env check` reporting drift and exiting non-zero, useful as a CI gate.

---

### `core/scan`: project analysis *(implemented)*

**Owns.** Four operations over a directory tree: a structural summary, a file-type breakdown, a
line count, and a tree listing.

**Dependencies.** One: a read-only `Inspector` (resolve, stat, walk, open). The scanner cannot move
a file or open a socket, and the interface is what enforces that.

**The operations.**

| Function | What it does |
|---|---|
| `Summarize` | Files, directories, size, depth, by category, top languages and extensions, authored share |
| `Types` | Counts and sizes by extension, or by folded language |
| `Lines` | Code, comment, and blank lines, grouped by language |
| `Tree` | The directory shape, with totals for every branch |

**Ignoring what the project ignores.** A scan that counts `node_modules` answers a question nobody
asked: four hundred thousand files, of which four hundred are the project. So the walk skips the
rules in `.gitignore`, plus the vendor and build directories every ecosystem has whether written
down or not, and always `.git`. The skipping happens before a directory is read, not after, which
is the difference between a scan that takes a second and one that takes a minute. `--no-ignore`
turns it off.

**The .gitignore support is honest about its limits.** The root file is read and the parts that
decide what a scan sees are applied: comments, negation, directory-only rules, anchoring, `**`, and
glob matching on name or path. Nested ignore files, `.git/info/exclude`, and knowledge of what is
already tracked are not, because those need a repository rather than a directory and this module
works on any directory. The gap is stated in the code and here rather than hidden, because "why is
this file in the count" has to have an answer, and `--exclude` covers the difference.

**Line counting is deliberately approximate.** Only files in a recognised language are opened; a
PNG has no lines. A line whose first characters open a comment is a comment, and a block comment
runs to its terminator, without parsing the language, so a comment marker inside a string literal
counts as a comment. Parsing forty languages properly means forty parsers to maintain, and these
numbers are used to compare parts of a tree with each other, where a small consistent error cancels
out. Each file streams through a reused buffer, so memory does not follow the size of the tree or
of any file in it.

**Classification is somebody else's table.** Whether a path is source, test, generated, or vendored
is decided in `internal/classify`, below the module layer, because `clean` and `secret` will need
the same answers and modules may not import each other.

**Size reporting lives elsewhere.** "Where did the disk space go" is `devnest file size`, and this
module does not duplicate it. It reports shape, not weight.

**Depends on.** `platform/fs` and `internal/classify`. Deliberately no git dependency: `scan` works
on any directory, including one that is not a repository.

**Later.** Comparison of two scans to show growth over time.

---

### `core/clean`: artifact removal *(implemented)*

**Owns.** Locating build output and dependency caches (`node_modules`, `target`, `dist`, `build`,
`__pycache__`, `.next`, `bin`, `obj`, and others), reporting reclaimable space, and (only when
explicitly told) removing them.

**Safety design.** This is the highest-risk module and its design reflects that:

- Dry run is the default. `--apply` is required to delete anything.
- Only names from the built-in rule set, plus user-configured names, are ever candidates. It never
  removes anything by a heuristic guess: size, age, and emptiness are not evidence.
- **A generic name needs a marker beside it.** `build`, `dist`, `out`, `coverage`, `target`, `bin`,
  and `obj` count only when the directory containing them also holds a project file such as
  `package.json`, `Cargo.toml`, or a `.csproj`. `node_modules` and `__pycache__` need nothing,
  because nobody has a personal directory by those names. This is the rule that separates build
  output from somebody's work, and a configured pattern is held to it too.
- Never crosses a filesystem boundary, never follows a symlink, never removes one, and never
  operates on a path outside the scan root after resolving.
- Never enters a version control directory.
- Refuses to run at a filesystem root or in a user's home directory without an explicit `--force`,
  which no configuration file can supply.
- Every candidate is re-checked against every guard in the moment before it is removed, because a
  tree can change between being listed and being deleted.
- A removal that fails is recorded and the rest continue; a run that stops halfway leaves a tree
  nobody can reason about.

**Two interfaces, split by whether they destroy.** `Scan` takes an `Inspector`, which has no method
that deletes; `Apply` takes a `Remover`. Calling the wrong function cannot delete anything, because
the wrong function has nothing to delete with.

**Deliberately absent.** `vendor` is not a rule: it is checked in on purpose in plenty of
repositories and deleting it breaks an offline build. Nor is anything outside the tree the user
named, such as `~/.npm` or `~/.cargo`. A tool that reaches into a home directory to free space is
a different and more dangerous tool.

**Depends on.** `platform/fs`.

**Later.** Age filters (`--older-than 30d`). Recursive multi-project mode for cleaning an entire
workspace directory in one pass: powerful and correspondingly dangerous, so it lands behind its
own flag with its own confirmation.

---

### `core/port`: network port inspection *(implemented)*

**Owns.** Enumerating listening sockets with their owning process, checking whether a specific
port is in use, and terminating a process holding a port when the user asks for it.

**Platform reality.** This is where cross-platform uniformity costs the most, and it cost slightly
more than planned. Windows calls the IP Helper API (`GetExtendedTcpTable` and its UDP twin) through
`syscall`, with no dependency added. Linux reads `/proc/net/{tcp,tcp6,udp,udp6}` and resolves
socket inodes to pids through `/proc/<pid>/fd`. macOS was planned as `libproc` and ships as `lsof`
instead: `libproc` needs cgo, DevNest builds with `CGO_ENABLED=0` so releases stay static and
cross-compilable, and giving that up for one command on one platform is the wrong trade. `lsof` has
shipped with macOS for two decades and its `-F` field output is a stable contract.

Three implementations, one exported surface, all under build tags in `platform/net` and
`platform/proc`. The module itself contains no OS conditionals.

**Privilege.** Process ownership for other users' processes may be unavailable without elevation.
Every such socket is listed with its owner marked unknown, and the result counts them, so a listing
is never quietly incomplete.

**Windows has no polite signal.** There is no way for one process to ask another to exit: a console
program can be sent a break event only from the same console, and from an unrelated process the
only universal mechanism is `TerminateProcess`, which is a kill. So on Windows `port free` requires
`--force` and says why, rather than presenting a kill as a request.

**Termination.** `devnest port free <n>` names the process, asks for confirmation, then asks the
process to exit and waits. Killing needs `--force`. Pid 0 and pid 1 are refused unconditionally in
the platform layer. Ownership is not checked here at all: the kernel is the authority on who may
signal what, it refuses, and the refusal is reported. A port held by more than one process is
refused rather than guessed at, and the pid is re-verified against the port immediately before
anything is signalled.

**Depends on.** `platform/net`, `platform/proc`.

**Later.** Watch mode for a single port. Reporting established connections, not just listeners.

---

### `core/hash`: checksums *(superseded)*

Hashing files lives in `core/file` as `devnest file hash`; hashing text and verifying a single
checksum live in `core/security` as `devnest security hash` and `devnest security checksum`. What
remains unimplemented from the original plan is verification against a whole checksum *file* (the
`SHA256SUMS` a release publishes) and deterministic directory tree digests.

**Owns.** Computing MD5, SHA-1, SHA-256, SHA-512, and CRC32 over a file, a directory tree, or
stdin, and verifying content against an expected digest or a checksum file.

**Design.** Streams through a fixed-size buffer; file size does not affect memory use. Multiple
algorithms in one pass write to a `MultiWriter` so a large file is read once. Directory hashing
produces a deterministic tree digest by sorting entries by path and folding in relative paths as
well as content, so a rename changes the digest.

**Verification** compares in constant time and exits non-zero on mismatch, so it drops directly
into a CI step.

**Depends on.** `platform/fs`.

**Later.** BLAKE3, if a well-maintained dependency exists and someone asks. Signature verification
is explicitly out of scope: that is a key-management problem and belongs to a real signing tool.

---

### `core/encoding`: encode and decode *(implemented)*

Base64 is implemented in `core/security` as `devnest security encode` and `devnest security
decode`, because it arrived with the commands that hash and verify. This module owns the rest.

**Owns.** Hex encoding and decoding, URL percent-encoding in both the query and path forms, and
JWT structural decoding.

**Decoded bytes are not assumed to be text.** A decode result says whether what came out is
printable; when it is not, the bytes are carried as Base64 and never written to a terminal. An
escape character is valid UTF-8 and can repaint the screen of whoever ran the command, and a decode
command is exactly where untrusted bytes arrive.

**Percent-encoding has two modes and says which it used.** A space is `+` in a query value and
`%20` in a path segment. Both are exposed rather than one being guessed at, because decoding a path
value in query mode silently turns a plus sign in a filename into a space.

**JWT handling.** Decodes the header and the payload, keeps both as raw JSON so key order survives,
and reports the registered claims with expiry judged against a time the caller passes in. It does
**not** verify signatures, and `signatureVerified` is a field in the result rather than a note in
this document. Verification needs the signing key, a policy for acceptable algorithms, and a
decision about issuer and audience; a tool that checks the shape of a signature without any of that
teaches people to trust a result that means nothing. `alg=none` is reported as unsecured. A JWE is
recognised and refused with an explanation rather than a Base64 error.

**Depends on.** Nothing outside the standard library. The caller supplies the input; this module
opens nothing.

**Later.** HTML entities. Query-string parsing into structured output. Unicode escape handling.

---

### `core/data`: JSON, YAML, CSV *(implemented)*

**Owns.** Validating, formatting, minifying, querying, and converting structured data between
JSON, YAML, and CSV.

**Everything is held in memory**, which is the opposite of how `core/log` works and is deliberate.
A document is a tree: pretty-printing the end depends on the start, a query can select the last
key, and a converter has to know the shape before it writes a row. The consequence is a size limit
of 64 MiB, enforced and reported rather than discovered as an out-of-memory kill, with an error
that names the streaming alternative.

**Errors.** A parse failure reports line and column with the offending line quoted. "Invalid JSON"
alone is not an acceptable message; finding the comma is the entire job. The fragment is quoted
without escaping, because a JSON line is already full of quotation marks and escaping them turns
the one thing the user is meant to read into a puzzle.

**Reprinting versus re-encoding.** `format` and `minify` work on the document's own bytes, so key
order and number precision survive and only the whitespace changes: a formatter that reorders keys
produces a diff nobody can review. `query` re-encodes the value it selects, so its object keys come
back sorted. Both facts are documented in the command help, because the difference is visible.

**Querying.** A path expression selects a subtree: keys separated by dots, array elements by `[n]`,
a key holding a dot or a space in `["quoted brackets"]`, and an optional leading `.` or `$`. No
filters, wildcards, or functions. The syntax was the open question in `prd.md` and this is the
answer: a query language is a product of its own, and a half-built one is a worse `jq` that nobody
has documented. Selecting nothing is `NOT_FOUND`, which is exit 3, so a script can branch on
whether a key exists.

**Conversion limits.** JSON to CSV requires an array of objects, a single object, or an array of
plain values. A nested value is rejected with a message naming the record and the key, or spread
across dotted columns with `--flatten`, never silently stringified. YAML anchors and aliases are
resolved on load, multi-document YAML becomes a JSON array, and comments are lost in that
direction because JSON cannot hold them. There is no `yaml format` for the same reason: re-emitting
YAML would delete every comment in the file.

**Depends on.** `platform/fs` for reading, the standard library for JSON and CSV, and
`github.com/goccy/go-yaml` for YAML. That is DevNest's only dependency; the reasoning is in
`architecture.md` under the decision record.

**Later.** TOML. Schema validation. Order-preserving query output, if the demand appears.

---

### `core/httpprobe`: HTTP requests *(superseded)*

Implemented as part of `core/network` in Phase 3, as `devnest network http`. The description below
is the original plan; everything in it now lives in that module.

**Owns.** Sending a request and reporting status, a timing breakdown (DNS, connect, TLS
handshake, time to first byte, total), response headers, body, redirect chain, and TLS
certificate details including expiry.

**Explicitly not** a full API client. No collections, no environments, no saved requests, no
scripting. Those are a different product; DevNest sends one request and reports precisely what
happened.

**Safety.** Redirects are capped and every hop is reported. Authorization headers are dropped on
a cross-origin redirect. Header values matching credential patterns are masked in output unless
`--show-secrets` is passed. Timeouts are on by default; an unbounded default hang is not
acceptable.

**Depends on.** `platform/net` for transport construction, so tests inject a fake round-tripper
and never touch the network.

**Later.** Simple repeat mode for a rough latency distribution, deliberately not a load testing
tool.

---

### `core/git`: repository inspection *(implemented)*

**Owns.** Repository summary (branch, remotes, working-tree status, commit count, age), stale
branch detection, contributor statistics, and a report of the largest objects in history.

**Parsing git.** Every invocation asks for a machine format rather than the human one:
`for-each-ref` and `log` with an explicit `--format`, `status --porcelain`, `cat-file
--batch-check`. Fields are joined with the unit separator, 0x1F. A null byte would be the obvious
choice and cannot be used, because an argument vector is null-terminated and the operating system
rejects an argument containing one; a tab or a pipe would be wrong because both appear in commit
subjects. Every call also passes `-c color.ui=false --no-pager`, so a user's own configuration
cannot colour or paginate output that is about to be parsed.

**Contributors are identified by address**, folded to lower case, because names are spelled several
ways in every repository of any age. Nothing here discovers anything private: the address is in
every commit object already.

**`large` walks every object**, which makes it the slowest command in DevNest and the one with the
longest timeout. Objects unreachable from any ref are left out: they are usually waiting to be
garbage collected, and a row nobody can act on is noise.

**Implementation choice.** Shells out to the `git` executable rather than embedding a git library.
The reasoning: any machine that has a repository to inspect has git installed, the CLI is a
stable and well-documented interface, and a git implementation is an enormous surface to carry
for read-only reporting. If git is absent, the command reports that clearly instead of failing
obscurely. Recorded here because it is the kind of decision that gets re-litigated.

**Read-only.** This module never commits, pushes, rebases, or deletes a branch. It reports; the
user decides. `devnest git stale` prints branches and, on request, the deletion commands to run.
It does not run them.

**Depends on.** `platform/proc` for invoking git, `platform/fs` for repository discovery.

**Later.** Hotspot analysis: files with the highest change frequency, a useful proxy for where
the risk lives.

---

### `core/secret`: credential scanning *(implemented)*

**Owns.** Scanning a working tree, and optionally git history, for credential-shaped strings,
reporting matches with file, line, rule name, and a redacted excerpt.

**Rules.** Pattern rules with an entropy threshold, compiled into the binary so the tool works
offline and behaves identically everywhere. Sixteen of them: provider prefixes that mean one thing
(`AKIA`, `ghp_`, `sk_live_`, `AIza`), private key headers, and two generic rules for an assignment
to something named like a secret. The set is deliberately not exhaustive, because a scanner that
recognises two hundred providers and fires on every second file gets switched off, and a scanner
that is switched off finds nothing.

**Every rule has an entropy floor**, including the ones matching a provider prefix. That is what
separates a live Stripe key from `sk_live_` followed by twenty X characters: both have the
right shape, and only one carries any information. A `Keyword` substring in front of each pattern
keeps the expensive part cheap, since most lines match no rule at all.

**History scanning reads added lines only.** A removal is not a second leak, and one credential
added, reverted, and re-added is reported once. The default depth is 500 commits; the whole
history is `--all` and is much slower, which is why it is a separate command rather than a flag on
the working-tree scan.

**False positives.** The dominant failure mode of every scanner of this kind. Mitigations: an
entropy floor before a generic high-entropy rule fires, path-based exclusions for test fixtures
and lock files, and an inline suppression comment. The scanner reports *candidates*, and the
output says so: the human decides.

**Output redaction.** Matched values are never printed in full: four characters and a length, which
is enough to recognise which of twelve keys in a file this is and no use to anybody. The redaction
happens where the finding is built rather than where it is rendered, and the `Finding` type has no
field holding the value at all, so there is nothing for a renderer, an export, or a verbose flag to
leak. A test serialises a whole result to JSON and fails if a credential appears anywhere in it.

**Depends on.** `platform/fs` for the working-tree scan, `platform/proc` for history scanning. Only
`History` takes a `Runner`, so a tree scan cannot start a process.

**Later.** Baseline files so an existing repository can adopt scanning without drowning in
historical findings. User-supplied rules from configuration: the `secret.custom_rules` key is
loaded and validated but not yet read by this module, because a user-supplied regular expression
needs a complexity bound before it is compiled.

---

### `core/config`: configuration management

**Owns.** Reading, writing, listing, and validating DevNest's own configuration. Thin by design:
the loading and merging logic lives in `internal/config`, and this module is the command-facing
view of it.

**Depends on.** `internal/config`, `platform/fs`.

---

### `core/doctor`: self-check

**Owns.** Verifying that DevNest itself is in working order: config file parses, config directory
is writable, embedded rule sets load, required external tools for optional features (git) are
present, terminal capabilities are detected correctly.

**Purpose.** The first thing to ask someone filing a bug report to run. Its output is designed to
be pasted into an issue: paths under the home directory are shortened to `~` and the hostname is
not reported at all.

**Depends on.** `internal/config`, `platform/sys`, `platform/fs`, `platform/proc`.

**Decided while building it.**

- **Nothing here returns an error.** A configuration file that will not parse and a missing git are
  the answers this module exists to produce; returning them as failures of the command would mean
  the report never arrives. The CLI turns a failed check into a non-zero exit, after printing.
- **A warning is not a failure.** A machine without git is a healthy machine: most commands never
  call it. Only a failure clears `healthy` and changes the exit code.
- **The rule tables are counted by the caller.** A module may not import another module, and the
  tables belong to `secret` and `clean`. The CLI passes the counts in, which costs one struct and
  keeps the layering intact.
- **`doctor` starts even when the configuration will not load**, with the compiled defaults in its
  place. Every other command refuses; the command whose job is to diagnose that file is the one
  that must not.
- **Writability is tested by writing.** Permission bits describe almost nothing on Windows, and an
  access control list, a read-only mount, or a full disk can refuse a write the mode allows. The
  probe in `platform/fs` writes a temporary file and removes it.

## Shared, below the modules

These are not modules. They hold no policy and make no decisions.

- **`platform/fs` walker** *(implemented)*: one tree walker with depth limits, exclusion globs,
  hidden-file rules, symlink following with cycle detection, cancellation, and a problem handler
  that lets a walk continue past an unreadable entry. Every operation in `core/file` uses it;
  `scan`, `clean`, and `secret` will use the same one.
- **Digest helpers** *(implemented)*: streaming hash construction in `platform/fs`. Every
  requested algorithm is fed from a single pass over the file, so three checksums of a large file
  cost one read.
- **Classification rule set** *(implemented)*: `internal/classify`, added in Phase 6, mapping a
  path to a category (source, test, generated, vendored, build, asset, docs, config) and a file to
  a language with its comment syntax. A leaf package holding rules and nothing else. `core/scan`
  uses it; `clean` and `secret` will. It is a different table from `core/file`'s `categories.go`:
  that one answers "photo or document", this one "authored or generated".
- **`platform/fs` digests** *(implemented)*: one streaming implementation serving both `Digest`
  (a file) and `DigestReader` (any stream), plus `DigestLength` and `AlgorithmForLength` so a
  checksum can be recognised from its shape. `core/file` and `core/security` both use it; neither
  has hashing code of its own.
- **`platform/fs` streaming reads** *(implemented)*: `Open` returns a reader over a file so a
  module can process one it could not hold. Added in Phase 5 for `core/log`; any module that needs
  to stream a file uses the same one rather than reaching for `os.Open` and losing the error
  classification that names the path.
- **`platform/proc` runner** *(implemented)*: running an external program under a timeout, and
  locating one on PATH with every copy in order. Added in Phase 6 for `env`. What makes a file
  runnable and what a shell would call it differ by operating system and live in its build-tag
  files, so `env` counts shadowed executables without knowing about PATHEXT.
- **`platform/sys` describer** *(implemented)*: OS, architecture, CPU count, hostname, home, shell,
  terminal, and the environment. Added in Phase 6. Every answer is cheap and none of them runs a
  program, which is what keeps the environment summary fast.
- **`platform/net` client** *(implemented)*: one HTTP client with a timing breakdown, a recorded
  redirect chain, credential dropping across origins, and a body cap; one DNS resolver; one TLS
  inspector; one TCP prober. Every network operation in the tool goes through it, which is what
  makes "every request is bounded by a timeout" a property of the code rather than a habit.

## Adding a module

1. Create `internal/core/<name>/` with `<name>.go`, `deps.go`, and tests.
2. Define `Request` and `Result` first, and write the JSON output you want to see before writing
   any logic. The output shape is the contract; deciding it last produces awkward types.
3. Declare dependency interfaces as narrowly as the module actually needs: an interface with one
   method is a good sign.
4. Implement, with tests against fakes.
5. Add the command file under `internal/cli/` and register it.
6. Update `commands.md`, `cli-reference.md`, and this file.

If step 4 requires importing another module, stop: something needs to move down a layer first.
