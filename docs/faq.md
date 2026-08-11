# FAQ

Status: current as of 1.0.0
Last revised: 2026-07-25

## About the project

**What is DevNest?**

A single-binary command line toolkit for the routine work that surrounds writing code: checking
what is holding a port, finding what is eating disk space, hashing a file, decoding a token,
scanning for leaked credentials, inspecting a repository. One tool, one interface, identical
behaviour on Windows, Linux, and macOS.

**Why does this need to exist? All of it is already possible.**

Possible, yes: with a different command on each platform, a personal collection of installed
utilities, and a browser tab for the things that are inconvenient locally. DevNest's contribution
is not capability, it is consolidation: one interface, one install, the same output shape
everywhere, and machine-readable output on every command.

**Is it a replacement for `jq` / `curl` / `git`?**

No. Those tools are better at their jobs than DevNest will ever be, and DevNest is not trying to
displace them. It covers the common cases well enough that you do not need to install five things
on a new machine, and it makes those cases consistent across platforms. When you need the full
power of a specialist tool, use the specialist tool.

**Why Go?**

Single static binary with no runtime dependency, genuine cross-compilation to six targets from one
machine, a standard library that covers most of what this tool needs, and fast startup. For a CLI
distributed to machines where you cannot assume a package manager exists, it is close to ideal.

**Why is Windows the primary platform?**

Because "Windows support" added later is usually broken. Developer tools written on Linux and
ported afterwards accumulate assumptions (path separators, case sensitivity, the 260-character
limit, process APIs) that are painful to unwind. Building Windows-first means those assumptions
never get made. Linux and macOS are fully supported and covered in CI equally.

**Is it open source?**

Yes. MIT, which is about as permissive as a licence gets: keep the copyright notice and do what you
like, including selling it.

It was not always. Releases up to and including 0.1.0 went out under the Apache License 2.0 with
the Commons Clause, which withheld the right to sell and therefore was not open source under the
OSI definition. That restriction bought nothing: it kept DevNest out of Homebrew core and the Linux
distribution repositories, and out of reach of anyone who wanted to contribute to an open source
project, in exchange for protecting a commercial plan that does not exist.

A copy taken under the old terms keeps them. Everything from here is MIT. See `LICENSE`.

## Using it

**Do I need to configure anything?**

No. Every command works immediately after install. Configuration exists to remove repetition for
daily users, not to make the tool usable.

**Does it send anything anywhere?**

Only when you run a `devnest network` command, which is the group whose entire purpose is sending
a request.
No telemetry, no analytics, no error reporting, no update check, not even opt-in. The absence of
the code is a stronger guarantee than a default setting.

**Can it delete my files?**

`devnest clean` can, and only with `--apply`. Without it, the command reports what it would remove
and stops. It only ever considers known artifact patterns, never crosses a filesystem boundary,
never touches anything outside the directory you ran it in, and refuses to run at a filesystem
root or home directory without `--force` and a confirmation. Every removal is logged with its full
path before it happens.

**Can I use it in CI?**

That is a primary use case. Every command has `--output json` with stable field names, meaningful
exit codes, and clean stdout/stderr separation. `devnest secret scan` and `devnest security checksum`
exit non-zero on a negative result specifically so they work as gates without output parsing.

A repository that already has findings adopts the scanning gate with `secret scan --baseline`:
accept what is there once, and only credentials added afterwards can fail the build.

**Does it need admin rights?**

No, and it never asks for them. Some information (process ownership for other users' processes)
is only visible when elevated. DevNest reports what it can see, marks the rest unknown, and
continues.

**Can I use it as a library?**

Not yet. Everything is under `internal/`, which the Go compiler prevents external modules from
importing. That is deliberate: a public API is a promise, and promises are expensive to keep. The
domain layer is already shaped for it, so if real demand appears, promoting a stable subset to
`pkg/` is a decision rather than a rewrite.

## Design questions

**Why is JSON output not automatic when piping?**

Because it would break every script written against the table output the first time someone ran
the command interactively while developing it. Colour is auto-detected; format never is. If you
want JSON, ask for it: with a flag, or with `DEVNEST_GENERAL_OUTPUT=json` in a CI environment.

**Why no plugin system?**

Third-party code loaded into this process is a security surface and a permanent compatibility
obligation. Extension happens two ways: contribute a module upstream, or consume the JSON output
from your own tool. If it is ever reconsidered, the mechanism will be a subprocess exchanging JSON
over stdio, not code loaded into DevNest.

**What can go in a project-local config file?**

A project can carry a `.devnest.toml` at its root, discovered by walking up from the working
directory. Its scope is deliberately narrow: presentation and inspection keys only. **Safety-relevant
keys are never allowed** (nothing in `[clean]` or `[secret]`, and not `general.confirm`) because a
file that travels with a clone must never widen what a delete command will remove or turn off a
confirmation. See `configuration.md` for the full list and the precedence order.

**Why does `devnest git` shell out instead of using a git library?**

Any machine with a repository to inspect has git installed. The CLI is a stable, documented,
extremely well-tested interface. A git implementation is an enormous surface to carry for
read-only reporting. If git is absent, the command says so clearly.

**Why does JWT decoding not verify signatures?**

Verification requires key material and key management, and doing it halfway gives a false sense of
assurance. The command exists so nobody has to paste a token into a web page to see what is in it,
and its output states plainly that the token is unverified.

**Why no emoji in the output?**

They render inconsistently across Windows terminals, break column alignment, and carry no
information a word does not carry better.

**Why are abbreviations banned in command names?**

`devnest cfg ls` saves four keystrokes and costs a documentation lookup every time someone is
unsure which abbreviation this particular tool chose. Shell history and tab completion make the
character count irrelevant.

## Contributing

**How do I add a new toolchain to `env` detection?**

A table entry: executable name, version flag, and a pattern to extract the version. No new logic.
This is the most contribution-friendly part of the project.

**Can I add a dependency?**

With a written justification covering what it does, why the standard library is insufficient, its
maintenance status, its own dependency count, its license, and what happens if it is abandoned.
See `rules.md` R13. The bar is high because each dependency is a supply-chain surface and a
permanent upgrade obligation.

**Why did my pull request get closed on scope grounds?**

Probably something in the non-goals list in `prd.md`. That list exists so the answer is
predictable rather than arbitrary: the project stays useful by staying narrow. Opening an issue
before writing code avoids this outcome.

**Can I add a command that only works on Windows?**

Only if it degrades cleanly elsewhere: reporting that it is unavailable on this platform rather
than failing obscurely. Cross-platform uniformity is a core commitment; see `design.md`.

## Practical

**Where is my configuration stored?**

| Platform | Path |
|---|---|
| Windows | `%APPDATA%\devnest\config.toml` |
| Linux | `~/.config/devnest/config.toml` |
| macOS | `~/Library/Application Support/devnest/config.toml` |

`devnest config path` prints the file in use.

**What else does it write to disk?**

Nothing, unless you ask. No cache directory, no registry keys, no service registration, no log
files. Reports go where `--export` points.

**How do I update it?**

Whatever installed it. See `installation.md`. DevNest never updates itself and never checks for
updates.

**How big is the binary?**

Under 25 MB uncompressed, and that is enforced as a release target.

**How do I report a security issue?**

Not in a public issue. Use the private channel in `SECURITY.md`.
