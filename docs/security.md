# Security

Status: every section is implemented as of 0.1.0
Last revised: 2026-07-25

DevNest deletes files, terminates processes, reads credentials, and makes network requests. Each
of those is a way to cause real damage. This document states how those capabilities are
constrained.

For reporting a vulnerability, see `SECURITY.md` at the repository root.

## Threat model

**What DevNest protects against:**

- Accidental destruction, deleting the wrong tree, killing the wrong process.
- Path traversal, an input path escaping the intended operation root.
- Credential exposure, leaking a secret through output, logs, exported reports, or an error
  message.
- Untrusted input causing unbounded resource consumption or a crash.
- Supply-chain risk in the dependency graph.

**What it does not protect against:**

- A malicious user with a shell on the machine. Someone who can run `devnest` can already run
  `rm`. DevNest is not a sandbox and does not pretend to be one.
- Compromise of the operating system or the Go toolchain.
- A user who deliberately passes `--force` past a guard that told them what would happen.

**Attacker positions considered:** a hostile repository being scanned (crafted filenames, symlink
loops, enormous files, deeply nested trees), a hostile HTTP response (huge body, redirect loop,
malicious headers), a hostile configuration file, and a hostile data file being parsed.

## Principles

**1. Read-only unless explicitly told otherwise.** Most commands cannot modify anything. Those
that can require an explicit flag; there is no command whose default behaviour destroys data.

**2. No network unless the user asked for a network operation.** `devnest http` connects. Nothing
else does: no update check, no telemetry, no error reporting, not even an opt-in switch. The
absence of the code is a stronger guarantee than a default setting.

**3. Least privilege.** DevNest never requests elevation. When an operation needs privileges it
does not have, it reports what it could not do and continues with the rest, rather than failing
the whole run or asking to be re-launched elevated.

This principle has shaped a feature rather than merely constrained one. `devnest network ping`
opens a TCP connection instead of sending an ICMP echo, because ICMP needs a raw socket, a raw
socket needs administrator rights, and a tool that works only when elevated is a tool most people
cannot use. Every result carries `method: "tcp"` and every rendering says so, so nobody has to
guess which question was answered.

**4. Secrets are never printed in full.** Not in output, not in logs, not in exported reports, not
under `--verbose`. This applies to secret-scanner findings, HTTP headers, environment variables,
and JWT contents.

The one deliberate exception is a password DevNest has just generated at the user's request, which
goes to standard output because producing it is the entire point of the command. Even then it is
never logged and never written to a file. A password *supplied* to DevNest is never printed back
at all; see the security module section below.

**5. Everything is local.** No data leaves the machine. Reports are written where the user asks.
No caching of file contents outside the process.

## File operations

**Path resolution.** Every path is made absolute and fully resolved (symlinks followed, `..`
collapsed) *before* any security decision is made. Deciding on the unresolved path and acting on
the resolved one is the classic time-of-check/time-of-use hole.

**Containment.** After resolution, every target must be under the operation root. A path that
escapes is rejected with `PERMISSION_DENIED`, not silently skipped, because silence hides an
attempted traversal.

**Files DevNest writes.** Three commands write outside a directory the user named: `--export`
writes a report, `config set` and `config init` write the configuration file. All three go through
one atomic write in `platform/fs`, which renders beside the target and renames over it, so an
interrupted write leaves the previous file or the new one and never a truncated file that still
looks valid. The temporary file is created with the owner-only permissions `os.CreateTemp` gives it
and the rename carries them over, which matters most for the configuration file.

Overwriting an existing export is allowed and warned about in the result, so the warning survives
every output format rather than only the terminal. `config init` refuses to overwrite at all: it is
the one file on the machine holding decisions somebody made by hand.

**Reports meant to be shared.** `devnest doctor` exists to be pasted into a public issue, so it
shortens paths under the home directory to `~` and does not report the hostname at all. Neither
helps anyone reading a stack of bug reports, and both identify whoever filed one.

**Symlinks.** Not followed by default. When `--follow-symlinks` is passed, the walk records each
resolved directory path and refuses to enter one twice, so a link pointing at its own ancestor
terminates instead of looping. A depth cap bounds the walk independently. On Windows this covers
directory junctions and reparse points as well, which behave differently enough from POSIX
symlinks that they get their own tests.

**Destructive guards.** For any removal:

- Dry run is the default; `--apply` is required.
- Only patterns from the built-in or configured rule set are ever candidates. Nothing is removed
  by heuristic guess.
- Never crosses a filesystem boundary.
- Refuses to operate at a filesystem root, at a user home directory, or at a system directory
  without `--force` plus an interactive confirmation.
- The `protect` list in configuration is absolute and cannot be overridden by a pattern match.
- Enumerate first, then act. The tree is never mutated during the walk over it.
- Every removal is logged with its full resolved path *before* it happens, so an interrupted run
  leaves a record of exactly how far it got.

**Guards on moving, as implemented.** `devnest file organize` and `devnest file rename` are the
first commands that change anything, and they are held to the same rules:

- **Nothing is deleted, by any command in the file group.** The most destructive operation
  available is a rename, and a rename that would replace an existing file is refused.
- Dry run is the default. `--apply` is required, and it prompts before proceeding unless `--yes`
  was passed or confirmation was turned off in configuration.
- The protected-path guard is a build-tag table in `platform/fs`: a drive or filesystem root, the
  user's home directory, and the system directories for that platform. It is lifted only by
  `--force` on the command line, never from a configuration file.
- Destinations are resolved and checked for containment inside the operation root before they are
  created, even though every destination is derived from a file's own name.
- Organise runs read-only first to build the plan, then performs it. A failure on one file is
  recorded and the rest continue; a rename conflict anywhere aborts the whole batch before
  anything moves.
- Cancellation is observed between files, never inside one.
- Hidden files are skipped unless asked for, and non-recursive is the default for both commands,
  so a mistyped path does not rearrange a directory tree.

**A known window.** `fs.System.Move` checks that the destination is free and then renames. Those
are two operations, so a file appearing between them would still be replaced on a platform whose
rename overwrites. Closing it needs a platform-specific atomic primitive; for a workstation tool
acting inside a directory the user named, the check is the right trade. It is recorded here and in
the code so nobody assumes otherwise.

**No copy-then-delete fallback.** A rename across filesystems fails rather than falling back to
copying and deleting the source. That fallback removes data, and an interruption partway through
loses it. Failing with a clear message is the better outcome.

**Writes are atomic.** Exports and config writes go to a temporary file in the destination
directory, are flushed, then renamed over the target. An interrupted write leaves the old file or
the new file, never a truncated one that parses as valid.

**Permissions on created files.** Config files are created with owner-only permissions on POSIX
(`0o600`) and the equivalent restricted ACL on Windows. Report files use `0o644` since they are
meant to be shared.

## Process operations

**Enumeration** is read-only and reports what the current user is permitted to see. Processes it
cannot inspect are listed with unknown fields rather than omitted, because a silently short list
is misleading.

**Termination** is the sharpest edge in the tool:

- The target process is identified by name and PID before anything happens, and shown to the user
  as part of the question. The command line is deliberately *not* read: it can carry a credential
  passed as an argument, and this output is printed, exported, and pasted into tickets.
- Interactive confirmation is required unless `--yes` was passed.
- PID 0 and PID 1 are refused unconditionally, in the platform layer, where the refusal is closest
  to the syscall. On Unix, signalling PID 0 addresses an entire process group.
- A port held by more than one process is refused rather than acted on. Choosing between two
  listeners is guessing with somebody's process.
- Graceful termination first, forceful only after a timeout and only with `--force`.
- The PID is re-verified against the port immediately before signalling. PIDs are reused, and the
  window between enumeration and action is exactly where the wrong process gets killed.

**Ownership is the operating system's decision.** DevNest computes no ownership check of its own
and never attempts to acquire elevation. The kernel refuses to signal another user's process, and
that refusal is classified and reported as a permission error naming what to do instead. A check
computed in DevNest would either duplicate the kernel's or, worse, disagree with it.

**Windows cannot ask a process to exit.** There is no cross-process equivalent of SIGTERM: a
console program can be sent a break event only from a process attached to the same console, and
from anywhere else the only universal mechanism is `TerminateProcess`, which is a kill. `port free`
on Windows therefore requires `--force` and explains why. Presenting a kill as a polite request
would be a lie that costs somebody unsaved state.

## Removal

`devnest clean` is the only command that deletes data, and its guards are tested against the
refusal rather than the success:

- Dry run is the default; `--apply` is required, and it prompts unless `--yes` was passed or
  confirmation was turned off in configuration.
- Only names in the built-in rule set or the user's configuration are candidates. Nothing is
  selected by size, age, or emptiness.
- A generic name needs corroboration: `build`, `dist`, `out`, `target`, `bin`, `obj`, and
  `coverage` count only when a project file such as `package.json` or `Cargo.toml` sits beside
  them. A configured pattern is held to the same requirement.
- Symbolic links are never followed and never removed. Version control directories are never
  entered.
- A candidate outside the scan root after resolving, or on a different filesystem, is skipped and
  the reason is reported.
- The protected-path table (filesystem roots, home directories, system directories) refuses the
  run; only `--force` on the command line lifts it, never a configuration file.
- Every candidate is re-checked against every guard immediately before it is removed. Between the
  scan and the delete, a directory can be replaced by a symlink or moved.
- Every skipped candidate is reported with its reason, because a guard that fires silently is
  indistinguishable from a bug.

**Subprocess execution** (currently only `git`):

- Resolved through PATH lookup, then executed by absolute path.
- Arguments passed as an argument vector, never through a shell. No string interpolation into a
  command line, ever.
- A timeout on every invocation.
- Output size is capped so a pathological repository cannot exhaust memory.
- The environment passed to the subprocess is minimal and explicit.

## Network operations

Only the `devnest network` group opens a socket. Nothing else in DevNest does.

- **TLS certificate verification is on** for every request. `--insecure` disables it and prints a
  warning every single time it is used.
- **Redirects are capped** at 10 by default and every hop is reported, including the status code
  that caused it.
- **`Authorization`, `Cookie`, and `Proxy-Authorization` are dropped on a cross-origin redirect.**
  Forwarding credentials to a host the user did not choose is a real leak, and it is the default
  behaviour of several popular tools. Go's client already strips two of these; DevNest repeats it
  explicitly and adds the third, because relying on a default that might change for something this
  consequential is not a trade worth making.
- **Every operation has a timeout.** There is no unbounded default anywhere: not for a request,
  not for a DNS lookup, not for a TLS handshake, not for the pause between attempts.
- **Response bodies are size-capped**, and the cap is reported when it is hit. The remainder is
  drained rather than abandoned, so the exchange completes and the recorded timing means
  something.
- **Credential-shaped headers are masked** in the result, not in one output path, so the table,
  the JSON, and any export all carry the mask. Unmasking needs `--show-secrets`, which warns.
  The rule catches the named headers plus anything containing `token`, `secret`, `password`,
  `api-key`, or `auth`.
- **Only `http` and `https` are accepted.** A scheme like `file://` inside a network command would
  read local files while the user believes a request is being made: a confused deputy in one
  line of argv.
- **No connection reuse, no cookie jar, no credential storage.** Keep-alives are disabled, which
  is also what makes a latency measurement honest.
- **The proxy configured in the environment is honoured**, so DevNest behaves like everything else
  on a corporate machine rather than mysteriously bypassing it.

### Why `--insecure` is one flag and not two

An earlier draft of this document called for `--insecure` plus a separate acknowledgement flag.
That was changed during implementation, deliberately, and it is worth recording why.

Two flags for one decision is friction people alias past, and habituation (reaching for
`--insecure` reflexively and no longer noticing certificate problems) is the actual risk. A
warning printed on every single use is the better control.

The change is also safer than it looks because the legitimate reason to look at a broken
certificate does not involve this flag at all. `devnest network ssl` inspects expired, self-signed,
and untrusted certificates as its ordinary job, with no flag required. That leaves `--insecure`
for the genuinely rare case of "fetch this anyway", which is exactly the case that should feel
unusual.

### Certificate inspection

`devnest network ssl` completes a TLS handshake with verification switched off, and then verifies
the chain that came back against the system trust store and the requested host name.

This is not a weakening. A verifying handshake fails and returns an error *instead of* the
certificate, which is useless for the one command whose purpose is explaining why a certificate is
bad. The verification still happens; it happens where its result can be reported rather than
thrown.

Nothing is sent over the connection. The handshake completes, the chain is copied out, and the
connection closes.

## Input validation

Validation happens at the CLI boundary. By the time a module receives a `Request`, its contents
are known good.

- **Paths**, resolved, checked for containment, checked for existence where required.
- **Ports**, 1 to 65535. Ports below 1024 require `--all` for enumeration, as a mild guard
  against a typo producing an alarming list.
- **URLs**, parsed and required to be `http` or `https`. `file://` and other schemes are
  rejected; a URL scheme that reads local files inside an HTTP command is a confused-deputy
  problem waiting to happen. A bare host is accepted and given `https`, because that is what
  people type.
- **Domains**, checked for the shapes that are certainly wrong: empty labels, spaces, labels over
  63 characters, names over 253. The check stops there on purpose. A full grammar would reject
  valid internal names, and `example.corp` resolving on a company network is not DevNest's
  business to second-guess.
- **HTTP methods**, checked against a fixed list rather than passed through, so a typo becomes a
  usage error instead of a request nobody meant to send.
- **Sizes and durations**, parsed with units and range-checked.
- **Regular expressions** from user configuration are compiled with a size limit and matched with
  a timeout, so a catastrophically backtracking pattern cannot hang the process.
- **Parsed data**, nesting depth and total size are capped. A deeply nested JSON document is a
  cheap way to exhaust a stack.

## Secret handling

DevNest holds no credentials of its own and stores none.

**Scanner output** is redacted always: file, line, rule name, and four characters with a length,
which is enough to recognise which of twelve keys in a file this is and no use to anybody. This
holds in every output format, at every verbosity, and in exported reports, because reports get
attached to tickets and tickets get shared.

The guarantee is structural rather than careful. `secret.Finding` has no field holding the matched
value, so there is nothing downstream for a renderer, an exporter, or a verbose flag to print. The
redaction happens where the finding is built. A test serialises a whole result to JSON and fails if
a credential appears anywhere in it, and `devnest secret test` never echoes the value it was handed
either, because somebody tuning a rule set is testing real credentials as often as not.

**False positives are the failure mode**, and the mitigations are in the rules rather than in
advice. Every rule carries an entropy floor, including those matching a provider prefix, so a
placeholder with the right shape and no information does not fire. Fixture directories, lock files,
dependency directories, and generated build output (`.next`, `.nuxt`, `.svelte-kit`, `.turbo`,
`coverage`, and the rest) are skipped by default. That last group was added after a scan of one
ordinary Next.js project produced 288 findings inside `.next` and one outside it: a report that
long is one nobody reads to the end, which is how the finding that mattered gets missed. A `devnest:allow-secret` comment silences one
line. The result is described as candidates, in those words, because a scanner people have learned
to ignore protects nothing.

**Environment variable display** (`env vars`) masks values whose names match credential patterns
(`*_TOKEN`, `*_SECRET`, `*_KEY`, `*_PASSWORD`, and similar). The full value is never shown, and
there is no flag to show it: if someone needs the value, their shell already prints it.

**JWT decoding** happens entirely offline and marks the output as unverified. No signature
verification is performed and none is implied.

**Logs never contain secret values.** The logger has no access to raw scanner matches; redaction
happens before a value can reach a log field.

**Test fixtures** containing credential-shaped strings use documented, obviously fake, never-issued
patterns, and are assembled from a prefix and a body rather than written out as one literal. That
is not superstition: a file holding a literal token is flagged by every scanner in the world,
including this one and including the push protection on the repository it lives in, and a fixture
that cannot be committed is not a fixture. Where the shape survives anyway, the line carries a
`devnest:allow-secret` comment.

`devnest secret scan .` over DevNest's own repository reports nothing, which is the state to keep
it in: a contributor who runs the tool on the tool should not be handed a page of findings to
learn to ignore.

## The security module

`devnest security` handles passwords, which makes it the part of DevNest most able to cause harm
by leaking. The rules below are implemented, not aspirational, and each has a test.

### Nothing is stored, logged, or written

`core/security` has **no logger**. Every other module takes one; this one does not, because the
surest way to guarantee a password never reaches a log is for the code to have nowhere to send it.

Nothing is written to a file. A generated password goes to standard output and nowhere else; if the
user wants it in a file they redirect it themselves, which is a decision they have made rather than
one DevNest made for them.

### The strength checker never echoes its input

The result of `password-check` contains no part of the password. Not the password, not a substring,
not a quoted example of the weak pattern that was found. Findings are fixed strings selected by
code: "four characters in consecutive order", never "1234".

This matters because a result is not just printed. It is rendered, serialised to JSON, exported,
and pasted into tickets read by people who were never meant to see the input. Describing the shape
of a weakness is enough to act on.

Two tests enforce it: one asserts that a given finding code always produces byte-identical text
whatever password produced it, which is the property that makes leakage structurally impossible;
the other marshals the result and checks the input does not appear in it.

The length is reported, and that does disclose the length. It is inherent to the feature.

### Randomness

Password generation takes its randomness as a parameter, an `io.Reader`, satisfied by
`crypto/rand.Reader` in production and a deterministic stream in tests. There is no seed, no
package-level source, and no path anywhere in the module to `math/rand`.

Selection uses `crypto/rand.Int`, whose rejection sampling keeps it uniform. Taking a random byte
modulo the alphabet size (the usual mistake) would favour the start of the pool. A test checks
the distribution over four thousand generated characters.

### Supplying a secret

A secret passed as a command-line argument is written to shell history and is readable from the
process table by anything running as the same user. It should be treated as disclosed.

Commands that take one therefore accept `--stdin`, and warn on stderr when the argument form is
used, naming the flag rather than only stating the problem. The argument form is not refused:
refusing it would be paternalistic, and it is what people reach for first.

### Clearing memory, honestly

Generated passwords are built in rune buffers which are zeroed as soon as the value has been copied
out. That reduces the window in which password material sits in a reusable buffer.

It does not eliminate the value from the process. Go strings are immutable and cannot be zeroed,
and the garbage collector may have copied the data during its lifetime. Claiming otherwise would be
worse than not trying, so the package comment says exactly this.

### Decoded bytes are not written blindly

`security decode` receives untrusted input by definition. When the decoded bytes are not printable
text they are reported as hex rather than written to the terminal.

Valid UTF-8 is not the test. An escape character is perfectly valid UTF-8 and can move a cursor,
repaint a screen, or change how a terminal behaves afterwards. Tab, newline, and carriage return
are allowed because text legitimately contains them; everything below 0x20, and 0x7f, is not.

`--raw` overrides this for someone who wants the bytes.

### What the strength checker does not do

The embedded list of known-bad passwords is roughly a hundred entries. A real cracking wordlist has
hundreds of millions and belongs in a dedicated tool; embedding one would add tens of megabytes to
a binary whose appeal is being a single small file.

A clean result therefore states in as many words that it is not a guarantee. A checker that implies
otherwise is worse than none, because it tells the user they have solved a problem they still have.

For the same reason, a password built on a known base applies a **ceiling** to the score rather than
only a penalty: `Password123!` has around 79 bits of nominal entropy and is found in milliseconds,
and subtracting points from a large number still leaves a respectable-looking one.

### MD5

MD5 is offered by `security hash` and accepted by `security checksum` because published checksums
still use it, not because it is a reasonable choice for anything new. It is broken for any purpose
that depends on an attacker being unable to construct a collision. The help text says so.

## Supply chain

- Dependencies are minimal and each addition is justified in writing; see `rules.md` R12–R16.
- Versions are pinned and `go.sum` is committed. Builds are reproducible.
- Dependency updates are their own pull request, never bundled with a feature, so the diff is
  reviewable.
- Vulnerability scanning runs in CI on every push and on a schedule, because a dependency can
  become vulnerable without anyone touching this repository.
- Only permissive licenses (MIT, BSD, Apache-2.0, ISC) enforced in CI.
- Release binaries are built by CI from a tagged commit, and checksums are published with every
  release.
- No dependency that executes code at import time or requires a build step beyond `go build`.

## Reporting a vulnerability

Do not open a public issue. Use the private reporting channel described in `SECURITY.md` at the
repository root. Reports get an acknowledgement within 48 hours and an assessment within 7 days.
